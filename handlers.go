package main

import (
	"errors"
	"net/http"
	"strconv"

	"gorm.io/gorm"
)

func (a *app) handleHealthz(w http.ResponseWriter, r *http.Request) {
	sqlDB, err := a.db.DB()
	if err == nil {
		err = sqlDB.PingContext(r.Context())
	}
	if err != nil {
		a.logger.Error("healthz: db ping failed", "err", err)
		http.Error(w, "db unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok\n"))
}

type messagePage struct {
	Title string
	Text  string
}

func (a *app) message(w http.ResponseWriter, r *http.Request, status int, title, text string) {
	a.render(w, r, status, "message.html", messagePage{Title: title, Text: text})
}

// notFound is also the answer for things that exist but the viewer can't see.
func (a *app) notFound(w http.ResponseWriter, r *http.Request) {
	a.message(w, r, http.StatusNotFound, "Not found", "There's nothing here, or you don't have access to it.")
}

func (a *app) ownerOnly(w http.ResponseWriter, r *http.Request) {
	a.message(w, r, http.StatusForbidden, "Owners only", "Only an owner of this household can do that.")
}

// lookupError turns a missing row into a 404 and anything else into a 500.
func (a *app) lookupError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		a.notFound(w, r)
		return
	}
	a.serverError(w, r, err)
}

func pathID(r *http.Request, name string) uint {
	return parseID(r.PathValue(name))
}

func parseID(s string) uint {
	id, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0 // no row has ID 0, so lookups become 404s
	}
	return uint(id)
}

// redirectBack sends a form post back to the page it came from. HTMX follows the
// redirect and swaps the page content in place.
func redirectBack(w http.ResponseWriter, r *http.Request, fallback string) {
	to := fallback
	if back := r.PostFormValue("back"); back != "" {
		to = safeNext(back)
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func (a *app) handleHome(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u == nil {
		a.render(w, r, http.StatusOK, "home.html", nil)
		return
	}
	groups, err := a.dashboard(u)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, "dashboard.html", groups)
}

type householdGroup struct {
	Household Household
	Role      string
	CreatedBy string // the creator's email, when that isn't the viewer
	Cards     []trackerCard
}

func (g householdGroup) IsOwner() bool { return g.Role == roleOwner }

func (a *app) dashboard(u *User) ([]householdGroup, error) {
	var members []HouseholdMember
	if err := a.db.Where("user_id = ?", u.ID).Order("created_at, household_id").Find(&members).Error; err != nil {
		return nil, err
	}
	loc := location(u.TimeZone)
	now := a.now()
	groups := make([]householdGroup, 0, len(members))
	for _, m := range members {
		g := householdGroup{Role: m.Role}
		if err := a.db.Take(&g.Household, m.HouseholdID).Error; err != nil {
			return nil, err
		}
		var err error
		if g.CreatedBy, err = a.createdByOther(m.HouseholdID, u.ID); err != nil {
			return nil, err
		}
		var trackers []Tracker
		if err := a.db.Where("household_id = ? AND archived_at IS NULL", m.HouseholdID).Order("position, id").Find(&trackers).Error; err != nil {
			return nil, err
		}
		for _, t := range trackers {
			c, err := a.card(t, loc, now, &u.ID)
			if err != nil {
				return nil, err
			}
			c.Back = "/"
			g.Cards = append(g.Cards, c)
		}
		groups = append(groups, g)
	}
	return groups, nil
}

// createdByOther returns the household creator's email, or "" when the viewer created it.
func (a *app) createdByOther(householdID, viewerID uint) (string, error) {
	creator, err := householdCreator(a.db, householdID)
	if err != nil || creator.ID == viewerID {
		return "", err
	}
	return creator.Email, nil
}
