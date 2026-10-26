package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

const (
	startCookie            = "start"
	transferCookie         = "transfer"
	startAttemptLifetime   = 10 * time.Minute
	maxGuestTrackers       = 20
	maxGuestHouseholds     = 5
	guestTrackLimitMessage = "A guest account can have 20 trackers. Add an email to create more."
	guestHouseLimitMessage = "A guest account can have 5 households. Add an email to create more."
)

var (
	errStartClosed   = errors.New("start attempt closed")
	errGuestLimited  = errors.New("guest creation limited")
	errRecorderGone  = errors.New("recorder is not a member")
	errTransferDone  = errors.New("transfer already completed")
	errTransferDeny  = errors.New("transfer denied")
	errTransferBlock = errors.New("transfer blocked")
)

// startSuggestions is the only copy of the homepage placeholder list.
var startSuggestions = []string{
	"Times my dog ate",
	"Weight",
	"Breast feedings",
	"Bench press weight",
	"Dirty diapers",
	"Workouts completed",
	"Glasses of water",
	"Books read",
	"Minutes practiced",
	"Hours slept",
	"Miles walked",
	"Cups of coffee",
	"Medications taken",
	"Plants watered",
	"Money saved",
	"Reward points earned",
}

type homeStart struct {
	Name        string
	Error       string
	Placeholder string
	Suggestions string
}

func homeStartData(name, msg string) homeStart {
	raw, _ := json.Marshal(startSuggestions)
	page := homeStart{Name: name, Error: msg, Suggestions: string(raw)}
	if len(startSuggestions) > 0 {
		page.Placeholder = startSuggestions[0]
	}
	return page
}

func trackerNameProblem(name string) string {
	switch {
	case name == "":
		return "Give the tracker a name."
	case utf8.RuneCountInString(name) > maxTrackerName:
		return fmt.Sprintf("Keep the name to %d characters.", maxTrackerName)
	}
	return ""
}

func setupURL(name string) string {
	return "/trackers/new?name=" + url.QueryEscape(name)
}

func redirectDocument(w http.ResponseWriter, r *http.Request, to string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func (a *app) setFlowCookie(w http.ResponseWriter, name, value string, lifetime time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/",
		MaxAge:   int(lifetime.Seconds()),
		HttpOnly: true, Secure: a.cfg.Env != "dev", SameSite: http.SameSiteLaxMode,
	})
}

func (a *app) clearFlowCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: a.cfg.Env != "dev", SameSite: http.SameSiteLaxMode,
	})
}

func (a *app) ensureStartCookie(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(startCookie); err == nil && c.Value != "" {
		return
	}
	raw, err := newToken()
	if err != nil {
		return
	}
	a.setFlowCookie(w, startCookie, raw, startAttemptLifetime)
}

func (a *app) handleStart(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PostFormValue("name"))
	if msg := trackerNameProblem(name); msg != "" {
		a.render(w, r, http.StatusUnprocessableEntity, "home.html", homeStartData(name, msg))
		return
	}
	if currentUser(r) != nil {
		a.clearFlowCookie(w, startCookie)
		redirectTo(w, r, setupURL(name))
		return
	}
	tz := r.PostFormValue("timezone")
	if !validTimeZone(tz) {
		tz = "UTC"
	}
	raw := ""
	if c, err := r.Cookie(startCookie); err == nil {
		raw = c.Value
	}
	sessionRaw, expires, err := a.guestForStart(raw, tz, a.clientIP(r))
	switch {
	case errors.Is(err, errGuestLimited):
		a.render(w, r, http.StatusTooManyRequests, "home.html", homeStartData(name, "Too many new trackers from this network. Try again later."))
		return
	case errors.Is(err, errStartClosed):
		a.clearFlowCookie(w, startCookie)
		a.render(w, r, http.StatusConflict, "home.html", homeStartData(name, "That start expired. Enter the name again."))
		return
	case err != nil:
		a.serverError(w, r, err)
		return
	}
	a.setSessionCookie(w, sessionRaw, expires)
	a.clearFlowCookie(w, startCookie)
	redirectDocument(w, r, setupURL(name))
}

// guestForStart creates a guest, or reuses one from an unexpired start attempt.
// The session row is inserted in the same transaction as the guest check, so a
// user who is no longer a guest does not receive a cookie.
func (a *app) guestForStart(raw, tz, ip string) (string, time.Time, error) {
	var sessionRaw string
	var expires time.Time
	err := a.withImmediate(func(tx *gorm.DB) error {
		now := a.now()
		userID, err := a.guestUserForStart(tx, raw, tz, ip, now)
		if err != nil {
			return err
		}
		sessionRaw, err = newToken()
		if err != nil {
			return err
		}
		expires = now.Add(sessionLifetime)
		return tx.Create(&Session{ID: hashToken(sessionRaw), UserID: userID, ExpiresAt: expires}).Error
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return sessionRaw, expires, nil
}

func (a *app) guestUserForStart(tx *gorm.DB, raw, tz, ip string, now time.Time) (uint, error) {
	if raw == "" {
		if !a.guestLimiter.allow(ip) {
			return 0, errGuestLimited
		}
		return createGuest(tx, tz)
	}
	hash := hashToken(raw)
	var attempt StartAttempt
	err := tx.Take(&attempt, "token_hash = ?", hash).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if !a.guestLimiter.allow(ip) {
			return 0, errGuestLimited
		}
		attempt = StartAttempt{TokenHash: hash, ExpiresAt: now.Add(startAttemptLifetime)}
		if err := tx.Create(&attempt).Error; err != nil {
			if !isUniqueConstraint(err) {
				return 0, err
			}
			if err := tx.Take(&attempt, "token_hash = ?", hash).Error; err != nil {
				return 0, err
			}
			return a.reuseStartGuest(tx, attempt, now)
		}
		id, err := createGuest(tx, tz)
		if err != nil {
			return 0, err
		}
		res := tx.Model(&StartAttempt{}).Where("token_hash = ? AND user_id IS NULL", hash).Update("user_id", id)
		if res.Error != nil {
			return 0, res.Error
		}
		if res.RowsAffected != 1 {
			return 0, errStartClosed
		}
		return id, nil
	}
	if err != nil {
		return 0, err
	}
	return a.reuseStartGuest(tx, attempt, now)
}

func (a *app) reuseStartGuest(tx *gorm.DB, attempt StartAttempt, now time.Time) (uint, error) {
	if attempt.ConsumedAt != nil || !now.Before(attempt.ExpiresAt) || attempt.UserID == nil {
		return 0, errStartClosed
	}
	var u User
	if err := tx.Take(&u, *attempt.UserID).Error; err != nil {
		return 0, err
	}
	if !u.IsGuest() {
		if err := tx.Model(&StartAttempt{}).Where("token_hash = ? AND consumed_at IS NULL", attempt.TokenHash).Update("consumed_at", now).Error; err != nil {
			return 0, err
		}
		return 0, errStartClosed
	}
	return u.ID, nil
}

func createGuest(tx *gorm.DB, tz string) (uint, error) {
	u := User{TimeZone: tz}
	if err := tx.Create(&u).Error; err != nil {
		return 0, err
	}
	if err := createPersonalHousehold(tx, u.ID); err != nil {
		return 0, err
	}
	return u.ID, nil
}

func (a *app) consumeStartAttempts(tx *gorm.DB, userID uint, now time.Time) error {
	return tx.Model(&StartAttempt{}).Where("user_id = ? AND consumed_at IS NULL", userID).Update("consumed_at", now).Error
}

// supersedeGuestClaims cancels this guest's open claim attempts and the
// credentials that belong to them. Login tokens with no transfer intent stay.
func (a *app) supersedeGuestClaims(tx *gorm.DB, guestID uint, now time.Time) error {
	var claims []GuestClaim
	if err := tx.Where("guest_user_id = ? AND cancelled_at IS NULL AND consumed_at IS NULL", guestID).Find(&claims).Error; err != nil {
		return err
	}
	if len(claims) == 0 {
		return nil
	}
	ids := make([]uint, len(claims))
	for i, c := range claims {
		ids[i] = c.ID
	}
	if err := tx.Model(&GuestClaim{}).Where("id IN ?", ids).Update("cancelled_at", now).Error; err != nil {
		return err
	}
	if err := tx.Model(&VerificationToken{}).Where("claim_id IN ? AND used_at IS NULL", ids).Update("used_at", now).Error; err != nil {
		return err
	}
	var intents []TransferIntent
	if err := tx.Where("claim_id IN ?", ids).Find(&intents).Error; err != nil {
		return err
	}
	if err := tx.Model(&TransferIntent{}).Where("claim_id IN ? AND cancelled_at IS NULL AND consumed_at IS NULL", ids).Update("cancelled_at", now).Error; err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}
	intentIDs := make([]uint, len(intents))
	for i, intent := range intents {
		intentIDs[i] = intent.ID
	}
	return tx.Model(&LoginToken{}).Where("transfer_intent_id IN ? AND used_at IS NULL", intentIDs).Update("used_at", now).Error
}

func (a *app) showKeep(u *User) bool {
	if u == nil || !u.IsGuest() || u.EmailPromptDismissedAt != nil {
		return false
	}
	var n int64
	if err := a.db.Model(&Entry{}).Where("recorded_by_id = ?", u.ID).Count(&n).Error; err != nil || n > 0 {
		return n > 0
	}
	if err := a.db.Model(&RecordedZero{}).Where("recorded_by_id = ?", u.ID).Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}

type keepPrompt struct {
	Show bool
	OOB  bool
}

func (a *app) handleDismissEmailPrompt(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u.IsGuest() && u.EmailPromptDismissedAt == nil {
		now := a.now()
		if err := a.db.Model(&User{}).Where("id = ? AND email_prompt_dismissed_at IS NULL", u.ID).Update("email_prompt_dismissed_at", now).Error; err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	if r.Header.Get("HX-Request") == "true" {
		a.renderBlock(w, r, http.StatusOK, "dashboard.html", "keepTrackers", dashboardPage{})
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) handleLogoutConfirm(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if !u.IsGuest() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	a.render(w, r, http.StatusOK, "logout_confirm.html", nil)
}
