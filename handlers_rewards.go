package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"
)

const rewardRequestBytes = 16

var (
	errNoReward       = errors.New("no reward available")
	errRewardReplay   = errors.New("redeem request already applied")
	errRewardSpent    = errors.New("redeem request already used")
	errRewardKey      = errors.New("redeem request does not belong to this tracker")
	errRewardConflict = errors.New("redeem request conflict")
)

func newRequestKey() (string, error) {
	buf := make([]byte, rewardRequestBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func validRequestKey(key string) bool {
	if len(key) != rewardRequestBytes*2 {
		return false
	}
	for _, c := range key {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

func (a *app) redemptionCount(trackerID uint) (int64, error) {
	var n int64
	err := a.db.Model(&Redemption{}).Where("tracker_id = ?", trackerID).Count(&n).Error
	return n, err
}

func (a *app) listRedemptions(trackerID uint) ([]Redemption, error) {
	var rows []Redemption
	err := a.db.Where("tracker_id = ?", trackerID).Order("redeemed_at DESC, id DESC").Find(&rows).Error
	return rows, err
}

type redemptionView struct {
	ID   uint
	When string
	Note string
	By   string
}

func (a *app) redemptionViews(rows []Redemption, loc *time.Location, now time.Time) ([]redemptionView, error) {
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		if row.UserID != nil {
			ids = append(ids, *row.UserID)
		}
	}
	emails := map[uint]string{}
	if len(ids) > 0 {
		var users []User
		if err := a.db.Select("id, email").Where("id IN ?", ids).Find(&users).Error; err != nil {
			return nil, err
		}
		for _, u := range users {
			emails[u.ID] = u.Label()
		}
	}
	views := make([]redemptionView, 0, len(rows))
	for _, row := range rows {
		v := redemptionView{
			ID:   row.ID,
			When: redemptionWhen(row.RedeemedAt, loc, now),
			Note: row.Note,
		}
		if row.UserID != nil {
			v.By = emails[*row.UserID]
		}
		views = append(views, v)
	}
	return views, nil
}

func redemptionWhen(at time.Time, loc *time.Location, now time.Time) string {
	local := at.In(loc)
	if local.Year() == now.In(loc).Year() {
		return local.Format("Jan 2, 3:04 PM")
	}
	return local.Format("Jan 2, 2006, 3:04 PM")
}

// withImmediate runs fn inside a SQLite immediate transaction, so the
// availability check and the insert cannot interleave with another writer.
func (a *app) withImmediate(fn func(tx *gorm.DB) error) error {
	sess := a.db.Session(&gorm.Session{SkipDefaultTransaction: true})
	return sess.Connection(func(raw *gorm.DB) error {
		// Session clones each statement. The connection callback itself does not,
		// and a not-found lookup would stick to it and skip the rollback.
		tx := raw.Session(&gorm.Session{SkipDefaultTransaction: true})
		if err := tx.Exec("BEGIN IMMEDIATE").Error; err != nil {
			return err
		}
		err := fn(tx)
		if err != nil {
			_ = tx.Exec("ROLLBACK").Error
			return err
		}
		if err := tx.Exec("COMMIT").Error; err != nil {
			_ = tx.Exec("ROLLBACK").Error
			return err
		}
		return nil
	})
}

func (a *app) handleRedeemReward(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	t, role, err := trackerForUser(a.db, u.ID, pathID(r, "id"))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	note, noteMsg := readNote(r)
	if noteMsg != "" {
		a.failRedeem(w, r, t, role, note, noteMsg)
		return
	}
	if !t.IsCumulative() || t.Goal == nil || *t.Goal <= 0 {
		a.failRedeem(w, r, t, role, note, "Rewards are only for a cumulative tracker with a goal.")
		return
	}
	key := r.PostFormValue("request")
	if !validRequestKey(key) {
		a.failRedeem(w, r, t, role, note, "Reload the page and try again.")
		return
	}
	now := a.now()
	result, err := a.redeemReward(t, u.ID, note, key, now)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	switch result {
	case redeemApplied, redeemReplay:
		a.finishReward(w, r, t.ID, "redeemed")
	case redeemSpent:
		a.finishReward(w, r, t.ID, "")
	case redeemNone:
		a.failRedeem(w, r, t, role, note, "No reward is available.")
	case redeemWrongTracker:
		a.failRedeem(w, r, t, role, note, "Reload the page and try again.")
	default:
		a.serverError(w, r, errors.New("unknown redeem result"))
	}
}

type redeemResult int

const (
	redeemApplied redeemResult = iota
	redeemReplay
	redeemSpent
	redeemNone
	redeemWrongTracker
)

func (a *app) redeemReward(t Tracker, userID uint, note, key string, now time.Time) (redeemResult, error) {
	var result redeemResult
	err := a.withImmediate(func(tx *gorm.DB) error {
		var seen []RewardRequest
		if err := tx.Limit(1).Where("request_key = ?", key).Find(&seen).Error; err != nil {
			return err
		}
		if len(seen) > 0 {
			if seen[0].TrackerID != t.ID {
				result = redeemWrongTracker
				return nil
			}
			var n int64
			if err := tx.Model(&Redemption{}).Where("request_key = ?", key).Count(&n).Error; err != nil {
				return err
			}
			if n > 0 {
				result = redeemReplay
				return nil
			}
			result = redeemSpent
			return nil
		}
		amounts, err := loadEntryAmounts(tx, t.ID)
		if err != nil {
			return err
		}
		var redeemed int64
		if err := tx.Model(&Redemption{}).Where("tracker_id = ?", t.ID).Count(&redeemed).Error; err != nil {
			return err
		}
		if rewardsAvailable(earnedGoals(amounts, t.Goal), redeemed).Sign() <= 0 {
			result = redeemNone
			return nil
		}
		row := Redemption{
			TrackerID:  t.ID,
			UserID:     &userID,
			Note:       note,
			RequestKey: key,
			RedeemedAt: now.UTC(),
			CreatedAt:  now.UTC(),
		}
		if err := tx.Create(&row).Error; err != nil {
			if isUniqueConstraint(err) {
				return errRewardConflict
			}
			return err
		}
		req := RewardRequest{RequestKey: key, TrackerID: t.ID, CreatedAt: now.UTC()}
		if err := tx.Create(&req).Error; err != nil {
			if isUniqueConstraint(err) {
				return errRewardConflict
			}
			return err
		}
		result = redeemApplied
		return nil
	})
	if errors.Is(err, errRewardConflict) {
		return a.replayReward(t.ID, key)
	}
	return result, err
}

func (a *app) replayReward(trackerID uint, key string) (redeemResult, error) {
	var seen []RewardRequest
	if err := a.db.Limit(1).Where("request_key = ?", key).Find(&seen).Error; err != nil {
		return 0, err
	}
	if len(seen) == 0 {
		return redeemNone, nil
	}
	if seen[0].TrackerID != trackerID {
		return redeemWrongTracker, nil
	}
	var n int64
	if err := a.db.Model(&Redemption{}).Where("request_key = ?", key).Count(&n).Error; err != nil {
		return 0, err
	}
	if n > 0 {
		return redeemReplay, nil
	}
	return redeemSpent, nil
}

func (a *app) handleUndoRedemption(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	t, _, err := trackerForUser(a.db, u.ID, pathID(r, "id"))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	rid := pathID(r, "rid")
	var removed int64
	err = a.withImmediate(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND tracker_id = ?", rid, t.ID).Delete(&Redemption{})
		if res.Error != nil {
			return res.Error
		}
		removed = res.RowsAffected
		return nil
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	notice := ""
	if removed > 0 {
		notice = "undone"
	}
	a.finishReward(w, r, t.ID, notice)
}

func (a *app) finishReward(w http.ResponseWriter, r *http.Request, trackerID uint, result string) {
	fallback := trackerURL(Tracker{ID: trackerID})
	if r.Header.Get("HX-Request") != "true" {
		to := redirectTarget(r, fallback)
		if result != "" {
			to = withRewardNotice(to, trackerID, result)
		}
		http.Redirect(w, r, to, http.StatusSeeOther)
		return
	}
	notice := ""
	switch result {
	case "redeemed":
		notice = "Reward redeemed."
	case "undone":
		notice = "Redemption undone."
	}
	if err := a.writeInPlace(w, r, trackerID, notice, fallback); err != nil {
		a.serverError(w, r, err)
	}
}

func (a *app) failRedeem(w http.ResponseWriter, r *http.Request, t Tracker, role, note, msg string) {
	if r.Header.Get("HX-Request") == "true" {
		path := pagePath(r)
		if path == "/" || trackerShowPath(path) {
			if err := a.writeRewardError(w, r, t, note, msg); err != nil {
				a.serverError(w, r, err)
			}
			return
		}
	}
	u := currentUser(r)
	overlay, err := a.resolveOverlay(u.ID, t, requestedOverlayID(r))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	p, err := a.trackerPage(t, role, u, overlay)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p.Card.RewardError = msg
	p.Card.RewardNote = note
	a.render(w, r, http.StatusUnprocessableEntity, "tracker_show.html", p)
}

func (a *app) writeRewardError(w http.ResponseWriter, r *http.Request, t Tracker, note, msg string) error {
	u := currentUser(r)
	if u == nil {
		redirectTo(w, r, "/login")
		return nil
	}
	switch path := pagePath(r); path {
	case "/":
		cards, err := a.dashboard(u)
		if err != nil {
			return err
		}
		page := dashboardPage{}
		for i := range cards {
			if cards[i].Tracker.ID != t.ID {
				continue
			}
			cards[i].RewardError = msg
			cards[i].RewardNote = note
			cards[i].OOB = "outerHTML"
			page.Cards = []trackerCard{cards[i]}
			w.Header().Set("HX-Reswap", "none")
			a.renderBlock(w, r, http.StatusUnprocessableEntity, "dashboard.html", "homeRefresh", page)
			return nil
		}
		return a.writeInPlace(w, r, t.ID, "", trackerURL(t))
	default:
		role := roleMember
		if _, got, err := trackerForUser(a.db, u.ID, t.ID); err == nil {
			role = got
		}
		overlay, err := a.resolveOverlay(u.ID, t, requestedOverlayID(r))
		if err != nil {
			a.lookupError(w, r, err)
			return nil
		}
		p, err := a.trackerPage(t, role, u, overlay)
		if err != nil {
			return err
		}
		p.Card.RewardError = msg
		p.Card.RewardNote = note
		p.OOBLog = true
		p.OOBTrend = true
		p.OOBHistory = true
		w.Header().Set("HX-Reswap", "none")
		a.renderBlock(w, r, http.StatusUnprocessableEntity, "tracker_show.html", "trackerRefresh", p)
		return nil
	}
}
