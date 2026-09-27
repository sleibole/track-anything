package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultLogLabel = "+ Log"
	maxLogLabel     = 24
	maxTrackerName  = 60
	maxNote         = 500

	summaryTimes = "times"
	summaryDone  = "done"
	summaryLast  = "last"

	timeInputLayout = "2006-01-02T15:04" // <input type="datetime-local">
)

// summaryDisplays is the create/edit choice. Empty is not a value; a missing field stores times.
var summaryDisplays = []choice{
	{summaryTimes, "Times today — show how many times it happened today"},
	{summaryDone, "Done today — show whether it happened today"},
	{summaryLast, "Last occurrence — show when it last happened"},
}

// Today's state of one tracker, in the zone of whoever is looking.

type todayState struct {
	Day     string
	Count   int
	Zero    *RecordedZero
	Entries []Entry // newest first
}

func (a *app) loadToday(t Tracker, loc *time.Location, now time.Time) (todayState, error) {
	s := todayState{Day: localDay(now, loc)}
	start, end, err := dayBounds(s.Day, loc)
	if err != nil {
		return s, err
	}
	err = a.db.Where("tracker_id = ? AND occurred_at >= ? AND occurred_at < ?", t.ID, start, end).
		Order("occurred_at DESC, id DESC").Find(&s.Entries).Error
	if err != nil {
		return s, err
	}
	s.Count = len(s.Entries)
	var zeros []RecordedZero
	if err := a.db.Where("tracker_id = ? AND day = ?", t.ID, s.Day).Limit(1).Find(&zeros).Error; err != nil {
		return s, err
	}
	if len(zeros) > 0 {
		s.Zero = &zeros[0]
	}
	return s, nil
}

// trackerCard is the home card, also used at the top of the tracker and share pages.
type trackerCard struct {
	Tracker  Tracker
	Count    int
	Summary  string
	Zone     string // IANA name used for this summary, for the optimistic last-occurrence clock
	Link     string // tracker page; empty on the share page
	QuickURL string
	UndoURL  string // set while the viewer's latest entry can still be undone
	Back     string // where the log and undo buttons return; empty means the tracker page
}

// card builds a tracker's card. viewerID is nil for a share-link visitor.
func (a *app) card(t Tracker, loc *time.Location, now time.Time, viewerID *uint) (trackerCard, error) {
	s, err := a.loadToday(t, loc, now)
	if err != nil {
		return trackerCard{}, err
	}
	return a.cardView(t, s, loc, now, viewerID)
}

// cardView builds a card from today's state. Last occurrence also loads the latest entry.
func (a *app) cardView(t Tracker, s todayState, loc *time.Location, now time.Time, viewerID *uint) (trackerCard, error) {
	var latest *Entry
	if t.SummaryDisplay == summaryLast {
		var err error
		latest, err = a.latestEntry(t.ID)
		if err != nil {
			return trackerCard{}, err
		}
	}
	return cardFor(t, s, latest, now, loc, viewerID), nil
}

// latestEntry is the entry with the latest OccurredAt. A later write of an older time does not win.
func (a *app) latestEntry(trackerID uint) (*Entry, error) {
	var entries []Entry
	err := a.db.Where("tracker_id = ?", trackerID).Order("occurred_at DESC, id DESC").Limit(1).Find(&entries).Error
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	return &entries[0], nil
}

func cardFor(t Tracker, s todayState, latest *Entry, now time.Time, loc *time.Location, viewerID *uint) trackerCard {
	var occurred *time.Time
	if latest != nil {
		occurred = &latest.OccurredAt
	}
	c := trackerCard{
		Tracker:  t,
		Count:    s.Count,
		Summary:  summaryLine(t.SummaryDisplay, s.Count, s.Zero != nil, occurred, loc, now),
		Zone:     loc.String(),
		Link:     fmt.Sprintf("/trackers/%d", t.ID),
		QuickURL: fmt.Sprintf("/trackers/%d/quick", t.ID),
	}
	if viewerID == nil {
		c.Link = ""
		c.QuickURL = "/s/" + *t.ShareToken + "/quick"
	}
	var undoable *Entry
	for i, e := range s.Entries {
		mine := e.ViaLink
		if viewerID != nil {
			mine = e.RecordedByID != nil && *e.RecordedByID == *viewerID
		}
		if mine && recent(e.CreatedAt, now) && (undoable == nil || e.CreatedAt.After(undoable.CreatedAt)) {
			undoable = &s.Entries[i]
		}
	}
	if undoable != nil {
		c.UndoURL = entryUndoURL(t, undoable.ID, viewerID == nil)
	}
	return c
}

func entryUndoURL(t Tracker, entryID uint, viaLink bool) string {
	if viaLink {
		return fmt.Sprintf("/s/%s/entries/%d/undo", *t.ShareToken, entryID)
	}
	return fmt.Sprintf("/entries/%d/undo", entryID)
}

type entryView struct {
	Entry
	Time      string // "3:04 PM" in the viewer's zone
	TimeValue string // the owner's edit field
	By        string // who logged it, when that isn't the viewer
	UndoURL   string // set while it can be undone
	Owner     bool   // show the owner's edit and delete controls
}

// entryViews labels entries for display. emails maps user IDs to addresses; a nil map
// (share pages) hides who logged what.
func entryViews(t Tracker, entries []Entry, loc *time.Location, now time.Time, viewerID *uint, emails map[uint]string, owner bool) []entryView {
	views := make([]entryView, 0, len(entries))
	for _, e := range entries {
		v := entryView{
			Entry:     e,
			Time:      e.OccurredAt.In(loc).Format("3:04 PM"),
			TimeValue: e.OccurredAt.In(loc).Format(timeInputLayout),
			Owner:     owner,
		}
		if emails != nil {
			switch {
			case e.ViaLink:
				v.By = "share link"
			case e.RecordedByID != nil && (viewerID == nil || *e.RecordedByID != *viewerID):
				v.By = emails[*e.RecordedByID]
			}
		}
		if recent(e.CreatedAt, now) {
			v.UndoURL = entryUndoURL(t, e.ID, viewerID == nil)
		}
		views = append(views, v)
	}
	return views
}

func (a *app) emailsFor(entries []Entry) (map[uint]string, error) {
	var ids []uint
	for _, e := range entries {
		if e.RecordedByID != nil {
			ids = append(ids, *e.RecordedByID)
		}
	}
	emails := map[uint]string{}
	if len(ids) == 0 {
		return emails, nil
	}
	var users []User
	if err := a.db.Select("id, email").Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, u := range users {
		emails[u.ID] = u.Email
	}
	return emails, nil
}

// The tracker page.

type historyDay struct {
	dayCount
	Label   string
	Entries []entryView
	Zero    *RecordedZero
	ZeroNew bool // the recorded zero can still be undone
}

type trackerPage struct {
	Card     trackerCard
	IsOwner  bool
	Today    todayState
	ZeroNew  bool
	Entries  []entryView
	History  []historyDay
	NowValue string // default for the owner's time field
	Note     string
	Error    string
}

func (p trackerPage) Tracker() Tracker { return p.Card.Tracker }

func (a *app) trackerPage(t Tracker, role string, u *User) (trackerPage, error) {
	loc := location(u.TimeZone)
	now := a.now()
	today, err := a.loadToday(t, loc, now)
	if err != nil {
		return trackerPage{}, err
	}
	card, err := a.cardView(t, today, loc, now, &u.ID)
	if err != nil {
		return trackerPage{}, err
	}
	p := trackerPage{
		Card:     card,
		IsOwner:  role == roleOwner,
		Today:    today,
		ZeroNew:  today.Zero != nil && recent(today.Zero.CreatedAt, now),
		NowValue: now.In(loc).Format(timeInputLayout),
	}

	first, err := time.ParseInLocation(dayLayout, today.Day, loc)
	if err != nil {
		return p, err
	}
	from := first.AddDate(0, 0, -historyDays)
	lastDay := first.AddDate(0, 0, -1).Format(dayLayout)

	var older []Entry
	err = a.db.Where("tracker_id = ? AND occurred_at >= ? AND occurred_at < ?", t.ID, from.UTC(), first.UTC()).
		Order("occurred_at DESC, id DESC").Find(&older).Error
	if err != nil {
		return p, err
	}
	var zeros []RecordedZero
	if err := a.db.Where("tracker_id = ? AND day >= ? AND day <= ?", t.ID, from.Format(dayLayout), lastDay).Find(&zeros).Error; err != nil {
		return p, err
	}

	emails, err := a.emailsFor(append(older, today.Entries...))
	if err != nil {
		return p, err
	}
	p.Entries = entryViews(t, today.Entries, loc, now, &u.ID, emails, p.IsOwner)

	times := make([]time.Time, len(older))
	byDay := map[string][]Entry{}
	for i, e := range older {
		times[i] = e.OccurredAt
		d := localDay(e.OccurredAt, loc)
		byDay[d] = append(byDay[d], e)
	}
	zeroDays := make([]string, len(zeros))
	zeroByDay := map[string]*RecordedZero{}
	for i := range zeros {
		zeroDays[i] = zeros[i].Day
		zeroByDay[zeros[i].Day] = &zeros[i]
	}
	counts, err := countsByDay(times, zeroDays, loc, lastDay, historyDays)
	if err != nil {
		return p, err
	}
	for _, c := range counts {
		h := historyDay{
			dayCount: c,
			Label:    c.Date.Format("Mon, Jan 2"),
			Entries:  entryViews(t, byDay[c.Day], loc, now, &u.ID, emails, p.IsOwner),
		}
		if c.Zero {
			h.Zero = zeroByDay[c.Day]
			h.ZeroNew = recent(h.Zero.CreatedAt, now)
		}
		p.History = append(p.History, h)
	}
	return p, nil
}

func (a *app) handleShowTracker(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	t, role, err := trackerForUser(a.db, u.ID, pathID(r, "id"))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	p, err := a.trackerPage(t, role, u)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, "tracker_show.html", p)
}

// renderTrackerError shows the tracker page again with a message above the log form.
func (a *app) renderTrackerError(w http.ResponseWriter, r *http.Request, t Tracker, role, msg string) {
	u := currentUser(r)
	p, err := a.trackerPage(t, role, u)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p.Error = msg
	p.Note = r.PostFormValue("note")
	a.render(w, r, http.StatusUnprocessableEntity, "tracker_show.html", p)
}

// Create and edit.

type trackerForm struct {
	Households     []Household // households the user owns, for a new tracker
	HouseholdID    uint
	Name           string
	Icon           string
	Accent         string
	LogLabel       string
	SummaryDisplay string
	Error          string
	Icons          []choice
	Accents        []choice
	Summaries      []choice
	Tracker        *Tracker // set when editing
	ShareURL       string
}

func (a *app) newTrackerForm(u *User) (trackerForm, error) {
	f := trackerForm{Icons: trackerIcons, Accents: trackerAccents, Summaries: summaryDisplays, SummaryDisplay: summaryTimes}
	err := a.db.Table("households").
		Select("households.*").
		Joins("JOIN household_members ON household_members.household_id = households.id").
		Where("household_members.user_id = ? AND household_members.role = ?", u.ID, roleOwner).
		Order("household_members.created_at, households.id").
		Find(&f.Households).Error
	if len(f.Households) > 0 {
		f.HouseholdID = f.Households[0].ID
	}
	return f, err
}

// readTrackerForm fills the editable fields from the request and returns a message
// for the first invalid one.
func readTrackerForm(r *http.Request, f *trackerForm) string {
	f.Name = strings.TrimSpace(r.PostFormValue("name"))
	f.Icon = r.PostFormValue("icon")
	f.Accent = r.PostFormValue("accent")
	f.LogLabel = strings.TrimSpace(r.PostFormValue("log_label"))
	f.SummaryDisplay = r.PostFormValue("summary_display")
	if f.SummaryDisplay == "" {
		f.SummaryDisplay = summaryTimes
	}
	switch {
	case f.Name == "":
		return "Give the tracker a name."
	case utf8.RuneCountInString(f.Name) > maxTrackerName:
		return fmt.Sprintf("Keep the name to %d characters.", maxTrackerName)
	case utf8.RuneCountInString(f.LogLabel) > maxLogLabel:
		return fmt.Sprintf("Keep the button label to %d characters.", maxLogLabel)
	case !listed(trackerIcons, f.Icon):
		return "Pick an icon from the list."
	case !listed(trackerAccents, f.Accent):
		return "Pick a color from the list."
	case !listed(summaryDisplays, f.SummaryDisplay):
		return "Pick a summary from the list."
	}
	return ""
}

func (a *app) handleNewTracker(w http.ResponseWriter, r *http.Request) {
	f, err := a.newTrackerForm(currentUser(r))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	// A household page links here with its own id. Ignore one the user doesn't own.
	if id := parseID(r.URL.Query().Get("household")); id != 0 {
		for _, h := range f.Households {
			if h.ID == id {
				f.HouseholdID = id
				break
			}
		}
	}
	a.render(w, r, http.StatusOK, "tracker_form.html", f)
}

func (a *app) handleCreateTracker(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	f, err := a.newTrackerForm(u)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	f.HouseholdID = parseID(r.PostFormValue("household"))
	_, role, err := householdForUser(a.db, u.ID, f.HouseholdID)
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	if role != roleOwner {
		a.ownerOnly(w, r)
		return
	}
	if f.Error = readTrackerForm(r, &f); f.Error != "" {
		a.render(w, r, http.StatusUnprocessableEntity, "tracker_form.html", f)
		return
	}

	var last int
	if err := a.db.Model(&Tracker{}).Where("household_id = ?", f.HouseholdID).Select("COALESCE(MAX(position), 0)").Scan(&last).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	t := Tracker{
		HouseholdID:    f.HouseholdID,
		Name:           f.Name,
		Icon:           f.Icon,
		Accent:         f.Accent,
		LogLabel:       f.LogLabel,
		SummaryDisplay: f.SummaryDisplay,
		Kind:           "count",
		Position:       last + 1,
	}
	if err := a.db.Create(&t).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/trackers/%d", t.ID), http.StatusSeeOther)
}

// ownedTracker loads an active tracker for an owner-only action, writing the 404 or 403
// itself. ok is false when the handler should stop.
func (a *app) ownedTracker(w http.ResponseWriter, r *http.Request) (t Tracker, ok bool) {
	t, role, err := trackerForUser(a.db, currentUser(r).ID, pathID(r, "id"))
	if err != nil {
		a.lookupError(w, r, err)
		return t, false
	}
	if role != roleOwner {
		a.ownerOnly(w, r)
		return t, false
	}
	return t, true
}

func (a *app) editForm(t Tracker) trackerForm {
	f := trackerForm{
		Name:           t.Name,
		Icon:           t.Icon,
		Accent:         t.Accent,
		LogLabel:       t.LogLabel,
		SummaryDisplay: t.SummaryDisplay,
		Icons:          trackerIcons,
		Accents:        trackerAccents,
		Summaries:      summaryDisplays,
		Tracker:        &t,
	}
	if t.ShareToken != nil {
		f.ShareURL = a.cfg.BaseURL + "/s/" + *t.ShareToken
	}
	return f
}

func (a *app) handleEditTracker(w http.ResponseWriter, r *http.Request) {
	t, ok := a.ownedTracker(w, r)
	if !ok {
		return
	}
	a.render(w, r, http.StatusOK, "tracker_form.html", a.editForm(t))
}

func (a *app) handleUpdateTracker(w http.ResponseWriter, r *http.Request) {
	t, ok := a.ownedTracker(w, r)
	if !ok {
		return
	}
	f := a.editForm(t)
	if f.Error = readTrackerForm(r, &f); f.Error != "" {
		a.render(w, r, http.StatusUnprocessableEntity, "tracker_form.html", f)
		return
	}
	err := a.db.Model(&t).Updates(map[string]any{
		"name": f.Name, "icon": f.Icon, "accent": f.Accent, "log_label": f.LogLabel, "summary_display": f.SummaryDisplay,
	}).Error
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/trackers/%d", t.ID), http.StatusSeeOther)
}

// Archiving clears the share link, so restoring never brings an old link back.
func (a *app) handleArchiveTracker(w http.ResponseWriter, r *http.Request) {
	t, ok := a.ownedTracker(w, r)
	if !ok {
		return
	}
	err := a.db.Model(&t).Updates(map[string]any{"archived_at": a.now(), "share_token": nil}).Error
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/households/%d", t.HouseholdID), http.StatusSeeOther)
}

func (a *app) handleRestoreTracker(w http.ResponseWriter, r *http.Request) {
	// Archived trackers are invisible to members, so a member gets the same 404 as anyone else.
	t, err := archivedTrackerForOwner(a.db, currentUser(r).ID, pathID(r, "id"))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	if err := a.db.Model(&t).Update("archived_at", nil).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	redirectBack(w, r, fmt.Sprintf("/households/%d", t.HouseholdID))
}

func (a *app) handleShareOn(w http.ResponseWriter, r *http.Request) {
	t, ok := a.ownedTracker(w, r)
	if !ok {
		return
	}
	token, err := newToken()
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.db.Model(&t).Update("share_token", token).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/trackers/%d/edit", t.ID), http.StatusSeeOther)
}

func (a *app) handleShareOff(w http.ResponseWriter, r *http.Request) {
	t, ok := a.ownedTracker(w, r)
	if !ok {
		return
	}
	if err := a.db.Model(&t).Update("share_token", nil).Error; err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/trackers/%d/edit", t.ID), http.StatusSeeOther)
}
