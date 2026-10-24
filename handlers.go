package main

import (
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"gorm.io/gorm"
)

func (a *app) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	sqlDB, err := a.db.DB()
	if err == nil {
		var one int
		err = sqlDB.QueryRowContext(r.Context(), "SELECT 1").Scan(&one)
	}
	if err != nil {
		a.logger.Error("healthz: database check failed", "err", err)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok\n"))
}

type messagePage struct {
	Title  string
	Text   string
	Action string // empty keeps the default "Back to Track Anything" link
	Href   string
}

func (a *app) message(w http.ResponseWriter, r *http.Request, status int, title, text string) {
	a.render(w, r, status, "message.html", messagePage{Title: title, Text: text})
}

func (a *app) messageTo(w http.ResponseWriter, r *http.Request, status int, title, text, action, href string) {
	a.render(w, r, status, "message.html", messagePage{Title: title, Text: text, Action: action, Href: href})
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

// redirectTo sends a form to another page. A normal submit is a 303. An HTMX submit
// gets a full navigation, so the address bar matches the page it lands on.
func redirectTo(w http.ResponseWriter, r *http.Request, to string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// redirectBack sends a form post back to the page it came from. HTMX follows the
// redirect and swaps the page content in place.
func redirectBack(w http.ResponseWriter, r *http.Request, fallback string) {
	to := fallback
	if back := r.PostFormValue("back"); back != "" {
		to = safeNext(back)
	} else if kept := samePageOverlay(r, fallback); kept != "" {
		to = kept
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// samePageOverlay keeps ?overlay= when a post returns to the tracker page it came from.
// Only a numeric overlay id is copied. The choice is not stored on the tracker.
func samePageOverlay(r *http.Request, fallback string) string {
	raw := r.Header.Get("HX-Current-URL")
	if raw == "" {
		raw = r.Referer()
	}
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Path != fallback {
		return ""
	}
	id := parseID(u.Query().Get("overlay"))
	if id == 0 {
		return ""
	}
	return fallback + "?overlay=" + strconv.FormatUint(uint64(id), 10)
}

func (a *app) handleHome(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u == nil {
		a.render(w, r, http.StatusOK, "home.html", nil)
		return
	}
	cards, err := a.dashboard(u)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, "dashboard.html", dashboardPage{Cards: cards})
}

type householdGroup struct {
	Household Household
	Role      string
	CreatedBy string // creator's email, and only when another visible household has the same name
	Cards     []trackerCard
}

type dashboardPage struct {
	Cards []trackerCard
	Name  string // the name field, kept when creating a household fails
	Error string
}

func (p dashboardPage) HasTrackers() bool { return len(p.Cards) > 0 }

// dashboard is the viewer's home list: every active tracker they can see, in
// their own order. Trackers they never placed follow, in household order.
func (a *app) dashboard(u *User) ([]trackerCard, error) {
	groups, err := a.dashboardGroups(u)
	if err != nil {
		return nil, err
	}
	var rows []TrackerOrder
	if err := a.db.Where("user_id = ?", u.ID).Find(&rows).Error; err != nil {
		return nil, err
	}
	placed := make(map[uint]int, len(rows))
	for _, row := range rows {
		placed[row.TrackerID] = row.Position
	}
	var cards []trackerCard
	for _, g := range groups {
		cards = append(cards, g.Cards...)
	}
	sort.SliceStable(cards, func(i, j int) bool {
		pi, iok := placed[cards[i].Tracker.ID]
		pj, jok := placed[cards[j].Tracker.ID]
		if iok != jok {
			return iok
		}
		return iok && pi < pj
	})
	return cards, nil
}

func (a *app) dashboardGroups(u *User) ([]householdGroup, error) {
	var members []HouseholdMember
	if err := a.db.Where("user_id = ?", u.ID).Order("position, household_id").Find(&members).Error; err != nil {
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
			c.Reorder = true
			c.NumberField = t.IsNumber()
			g.Cards = append(g.Cards, c)
		}
		groups = append(groups, g)
	}
	// The creator's email appears only when two visible households share a name,
	// and never on a household the viewer created.
	dup := duplicateHouseholdNames(groups)
	for i := range groups {
		if !dup[groups[i].Household.ID] {
			continue
		}
		email, err := a.createdByOther(groups[i].Household.ID, u.ID)
		if err != nil {
			return nil, err
		}
		groups[i].CreatedBy = email
	}
	personalID, err := a.personalHouseholdID(u.ID, groups)
	if err != nil {
		return nil, err
	}
	for i := range groups {
		if groups[i].Household.ID == personalID {
			continue
		}
		label := groups[i].Household.Name
		if groups[i].CreatedBy != "" {
			label += " · " + groups[i].CreatedBy
		}
		link := "/households/" + strconv.FormatUint(uint64(groups[i].Household.ID), 10)
		for j := range groups[i].Cards {
			groups[i].Cards[j].HouseholdLabel = label
			groups[i].Cards[j].HouseholdURL = link
		}
	}
	return groups, nil
}

// personalHouseholdID is the household created for this user at signup: the
// earliest household on the dashboard whose creator is the viewer. Later
// households they create, and households someone else created, are shared.
func (a *app) personalHouseholdID(viewerID uint, groups []householdGroup) (uint, error) {
	var best uint
	var bestAt time.Time
	for _, g := range groups {
		creator, err := householdCreator(a.db, g.Household.ID)
		if err != nil {
			return 0, err
		}
		if creator.ID != viewerID {
			continue
		}
		if best == 0 || g.Household.CreatedAt.Before(bestAt) || (g.Household.CreatedAt.Equal(bestAt) && g.Household.ID < best) {
			best = g.Household.ID
			bestAt = g.Household.CreatedAt
		}
	}
	return best, nil
}

// duplicateHouseholdNames marks households whose names match another on the page,
// after trimming surrounding whitespace and ignoring case.
func duplicateHouseholdNames(groups []householdGroup) map[uint]bool {
	dup := map[uint]bool{}
	for i := range groups {
		for j := i + 1; j < len(groups); j++ {
			if sameHouseholdName(groups[i].Household.Name, groups[j].Household.Name) {
				dup[groups[i].Household.ID] = true
				dup[groups[j].Household.ID] = true
			}
		}
	}
	return dup
}

// createdByOther returns the household creator's email, or "" when the viewer created it.
func (a *app) createdByOther(householdID, viewerID uint) (string, error) {
	creator, err := householdCreator(a.db, householdID)
	if err != nil || creator.ID == viewerID {
		return "", err
	}
	return creator.Email, nil
}
