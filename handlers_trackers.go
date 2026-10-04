package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
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

// trackerKinds is the create/edit choice. An empty kind means count, which is what a form that omitted the field stored.
var trackerKinds = []choice{
	{kindCount, "Count — each log is one event"},
	{kindNumber, "Number — a value, such as weight"},
	{kindDuration, "Duration — how long it took"},
}

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
	Tracker      Tracker
	Count        int
	Summary      string
	Zone         string // IANA name used for this summary, for the optimistic last-occurrence clock
	Link         string // tracker page; empty on the share page
	QuickURL     string
	UndoURL      string // set while the viewer's latest entry can still be undone
	Back         string // where the log and undo buttons return; empty means the tracker page
	Carry        string // "Last value: 10 lb" or "Last duration: 42 min"; empty when nothing carries forward
	CarryValue   string // number input, without the unit
	CarryHours   string
	CarryMinutes string
	CarrySeconds string
	LogAgain     bool // one tap repeats Carry; false when there is no previous value
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
	if t.SummaryDisplay == summaryLast || t.IsNumber() || t.IsDuration() {
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
	applyCarry(&c, t, latest)
	return c
}

// applyCarry fills the one-tap value from the latest entry by OccurredAt.
// The note is not copied. A count tracker has nothing to carry.
func applyCarry(c *trackerCard, t Tracker, latest *Entry) {
	if latest == nil {
		return
	}
	switch {
	case t.IsNumber() && latest.Number != nil:
		c.Carry = "Last value: " + formatMeasurement(*latest.Number, t.Unit)
		c.CarryValue = formatNumber(*latest.Number)
		c.LogAgain = true
	case t.IsDuration() && latest.DurationSec != nil && *latest.DurationSec > 0:
		c.Carry = "Last duration: " + formatDuration(*latest.DurationSec)
		c.CarryHours, c.CarryMinutes, c.CarrySeconds = durationFields(*latest.DurationSec)
		c.LogAgain = true
	}
}

func entryUndoURL(t Tracker, entryID uint, viaLink bool) string {
	if viaLink {
		return fmt.Sprintf("/s/%s/entries/%d/undo", *t.ShareToken, entryID)
	}
	return fmt.Sprintf("/entries/%d/undo", entryID)
}

type entryView struct {
	Entry
	Time        string // "3:04 PM" in the viewer's zone
	TimeValue   string // the owner's edit field
	Value       string // "10 lb" or "42 min"; empty for a count
	NumberValue string
	Hours       string
	Minutes     string
	Seconds     string
	Kind        string
	Unit        string
	By          string // who logged it, when that isn't the viewer
	UndoURL     string // set while it can be undone
	Owner       bool   // show the owner's edit and delete controls
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
			Kind:      t.Kind,
			Unit:      t.Unit,
			Owner:     owner,
		}
		switch {
		case t.IsNumber() && e.Number != nil:
			v.Value = formatMeasurement(*e.Number, t.Unit)
			v.NumberValue = formatNumber(*e.Number)
		case t.IsDuration() && e.DurationSec != nil && *e.DurationSec > 0:
			v.Value = formatDuration(*e.DurationSec)
			v.Hours, v.Minutes, v.Seconds = durationFields(*e.DurationSec)
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
	Card           trackerCard
	IsOwner        bool
	Today          todayState
	ZeroNew        bool
	Entries        []entryView
	History        []historyDay
	ChartScript    template.HTML // daily count series; empty when there is nothing to plot
	OverlayScript  template.HTML // overlay days; empty when no overlay is drawn
	OverlayChoices []Tracker     // other count trackers this person can see
	Overlay        *Tracker      // selected overlay; nil means none
	OverlayID      uint          // 0 when none; safe to compare in the template
	OverlayEmpty   bool          // selected overlay has no events in the chart window
	PageURL        string        // this page, including ?overlay= when one is selected
	NowValue       string        // default for the owner's time field
	Note           string
	NumberValue    string // log form; the carried value unless this render is correcting a post
	Hours          string
	Minutes        string
	Seconds        string
	Error          string
}

func (p trackerPage) Tracker() Tracker { return p.Card.Tracker }

func (a *app) trackerPage(t Tracker, role string, u *User, overlay *Tracker) (trackerPage, error) {
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
		Card:        card,
		IsOwner:     role == roleOwner,
		Today:       today,
		ZeroNew:     today.Zero != nil && recent(today.Zero.CreatedAt, now),
		NowValue:    now.In(loc).Format(timeInputLayout),
		PageURL:     trackerPageURL(t.ID, 0),
		NumberValue: card.CarryValue,
		Hours:       card.CarryHours,
		Minutes:     card.CarryMinutes,
		Seconds:     card.CarrySeconds,
	}
	if overlay != nil {
		p.Overlay = overlay
		p.OverlayID = overlay.ID
		p.PageURL = trackerPageURL(t.ID, overlay.ID)
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
	// Number and duration history is the days that have entries. Empty days and
	// recorded zeros belong to count trackers.
	if !t.IsCount() {
		logged := p.History[:0]
		for _, h := range p.History {
			if len(h.Entries) > 0 {
				logged = append(logged, h)
			}
		}
		p.History = logged
	}
	choices, err := visibleCountTrackers(a.db, u.ID, t.ID)
	if err != nil {
		return p, err
	}
	p.OverlayChoices = choices
	var script template.HTML
	if t.IsCount() {
		script, err = countChartScript(today, older, zeros, loc)
	} else {
		script, err = valueChartScript(append(append([]Entry{}, today.Entries...), older...), t, loc, today.Day)
	}
	if err != nil {
		return p, err
	}
	p.ChartScript = script
	// No primary history means no chart, even when the overlay has events.
	if overlay != nil && script != "" {
		days, err := a.overlayEvents(overlay, loc, today.Day)
		if err != nil {
			return p, err
		}
		p.OverlayEmpty = len(days) == 0
		embedded, err := overlayChartScript(overlay, days)
		if err != nil {
			return p, err
		}
		p.OverlayScript = embedded
	}
	return p, nil
}

func trackerPageURL(id, overlayID uint) string {
	u := fmt.Sprintf("/trackers/%d", id)
	if overlayID == 0 {
		return u
	}
	return fmt.Sprintf("%s?overlay=%d", u, overlayID)
}

// requestedOverlayID reads ?overlay= from this request, or from the page a
// non-GET was submitted from. Zero means none.
func requestedOverlayID(r *http.Request) uint {
	if id := parseID(r.URL.Query().Get("overlay")); id != 0 {
		return id
	}
	if r.Method == http.MethodGet {
		return 0
	}
	raw := r.Header.Get("HX-Current-URL")
	if raw == "" {
		raw = r.Referer()
	}
	u, err := url.Parse(raw)
	if err != nil {
		return 0
	}
	return parseID(u.Query().Get("overlay"))
}

// resolveOverlay loads one other count tracker the user can see.
// The tracker's own id means no overlay. An id they cannot see is a 404.
// The primary tracker can be a count, a number, or a duration. The overlay is still events.
func (a *app) resolveOverlay(userID uint, primary Tracker, overlayID uint) (*Tracker, error) {
	if overlayID == 0 || overlayID == primary.ID {
		return nil, nil
	}
	other, _, err := trackerForUser(a.db, userID, overlayID)
	if err != nil {
		return nil, err
	}
	if other.Kind != "count" {
		return nil, gorm.ErrRecordNotFound
	}
	return &other, nil
}

// overlayEvents loads the overlay tracker's events in the chart window.
// The window is chartWindow, the same range chartSeries draws.
func (a *app) overlayEvents(overlay *Tracker, loc *time.Location, lastDay string) ([]overlayDay, error) {
	start, end, err := chartWindow(lastDay, loc, chartDays)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	err = a.db.Where("tracker_id = ? AND occurred_at >= ? AND occurred_at < ?", overlay.ID, start, end).
		Order("occurred_at, id").Find(&entries).Error
	if err != nil {
		return nil, err
	}
	times := make([]time.Time, len(entries))
	for i, e := range entries {
		times[i] = e.OccurredAt
	}
	return overlayDays(times, loc, lastDay, chartDays)
}

func overlayMark(icon string) string {
	if listed(trackerIcons, icon) && isEmoji(icon) {
		return icon
	}
	return ""
}

func overlayChartScript(t *Tracker, days []overlayDay) (template.HTML, error) {
	if days == nil {
		days = []overlayDay{}
	}
	b, err := json.Marshal(overlayChart{Name: t.Name, Mark: overlayMark(t.Icon), Days: days})
	if err != nil {
		return "", err
	}
	return template.HTML(`<script type="application/json" id="count-overlay">` + string(b) + `</script>`), nil
}

// countChartScript embeds the daily count series for the chartDays local days
// ending today, oldest first. History is the previous historyDays and does not
// include today. An empty result means every day in the window is a gap, so the
// page shows a sentence instead of a chart. json.Marshal escapes <, >, and &,
// so the series cannot close the script element.
func countChartScript(today todayState, older []Entry, zeros []RecordedZero, loc *time.Location) (template.HTML, error) {
	events := make([]time.Time, 0, len(today.Entries)+len(older))
	for _, e := range today.Entries {
		events = append(events, e.OccurredAt)
	}
	for _, e := range older {
		events = append(events, e.OccurredAt)
	}
	zeroDays := make([]string, len(zeros), len(zeros)+1)
	for i := range zeros {
		zeroDays[i] = zeros[i].Day
	}
	if today.Zero != nil {
		zeroDays = append(zeroDays, today.Day)
	}
	series, err := chartSeries(events, zeroDays, loc, today.Day, chartDays)
	if err != nil || !chartHasData(series) {
		return "", err
	}
	b, err := json.Marshal(series)
	if err != nil {
		return "", err
	}
	return template.HTML(`<script type="application/json" id="count-chart">` + string(b) + `</script>`), nil
}

// valueChartScript embeds number or duration observations for the same 30 local days
// as the count chart. No points means the page shows a sentence instead of a chart.
func valueChartScript(entries []Entry, t Tracker, loc *time.Location, lastDay string) (template.HTML, error) {
	points, days, err := valuePoints(entries, t, loc, lastDay, chartDays)
	if err != nil || len(points) == 0 {
		return "", err
	}
	b, err := json.Marshal(valueChart{Kind: t.Kind, Unit: t.Unit, Days: days, Points: points})
	if err != nil {
		return "", err
	}
	return template.HTML(`<script type="application/json" id="value-chart">` + string(b) + `</script>`), nil
}

func (a *app) handleShowTracker(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	t, role, err := trackerForUser(a.db, u.ID, pathID(r, "id"))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
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
	a.render(w, r, http.StatusOK, "tracker_show.html", p)
}

// renderTrackerError shows the tracker page again with a message above the log form.
func (a *app) renderTrackerError(w http.ResponseWriter, r *http.Request, t Tracker, role, msg string) {
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
	p.Error = msg
	p.Note = r.PostFormValue("note")
	if strings.HasSuffix(r.URL.Path, "/entries") {
		p.NumberValue = r.PostFormValue("number")
		p.Hours = r.PostFormValue("hours")
		p.Minutes = r.PostFormValue("minutes")
		p.Seconds = r.PostFormValue("seconds")
	}
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
	Kind           string
	Unit           string
	KindLocked     bool // history exists, so the kind cannot change
	Error          string
	Icons          []choice
	Accents        []choice
	Summaries      []choice
	Kinds          []choice
	Tracker        *Tracker // set when editing
	ShareURL       string
	MoveHouseholds []Household // other households the owner owns; empty hides the move control
	MoveConfirm    string
	MoveError      string
}

func (a *app) newTrackerForm(u *User) (trackerForm, error) {
	f := trackerForm{
		Icons: trackerIcons, Accents: trackerAccents, Summaries: summaryDisplays, SummaryDisplay: summaryTimes,
		Kinds: trackerKinds, Kind: kindCount,
	}
	hs, err := ownedHouseholds(a.db, u.ID)
	f.Households = hs
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
	f.Kind = r.PostFormValue("kind")
	if f.Kind == "" {
		f.Kind = kindCount
	}
	f.Unit = strings.TrimSpace(r.PostFormValue("unit"))
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
	case !listed(trackerKinds, f.Kind):
		return "Pick a kind from the list."
	case f.Kind == kindNumber:
		return validUnit(f.Unit)
	case f.Unit != "":
		return "A unit is only for a number tracker."
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
		Kind:           f.Kind,
		Unit:           unitForKind(f.Kind, f.Unit),
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
		Kind:           t.Kind,
		Unit:           t.Unit,
		Kinds:          trackerKinds,
		Icons:          trackerIcons,
		Accents:        trackerAccents,
		Summaries:      summaryDisplays,
		Tracker:        &t,
	}
	if f.Kind == "" {
		f.Kind = kindCount
	}
	if t.ShareToken != nil {
		f.ShareURL = a.cfg.BaseURL + "/s/" + *t.ShareToken
	}
	return f
}

// withMove fills the move control for households the user owns besides this tracker's.
func (a *app) withMove(u *User, t Tracker, f trackerForm) (trackerForm, error) {
	owned, err := ownedHouseholds(a.db, u.ID)
	if err != nil {
		return f, err
	}
	for _, h := range owned {
		if h.ID != t.HouseholdID {
			f.MoveHouseholds = append(f.MoveHouseholds, h)
		}
	}
	f.MoveConfirm = moveConfirm(f.MoveHouseholds, t.ShareToken != nil)
	return f, nil
}

func moveConfirm(dests []Household, shareOn bool) string {
	if len(dests) == 0 {
		return ""
	}
	var msg string
	if len(dests) == 1 {
		name := dests[0].Name
		msg = fmt.Sprintf("Move this tracker to %s? Members of this household who aren't in %s will lose access. Members of %s will gain access, including its history.", name, name, name)
	} else {
		msg = "Move this tracker to the household you selected? Members of this household who aren't in it will lose access. Members of that household will gain access, including its history."
	}
	if shareOn {
		msg += " The existing share link will keep working."
	}
	return msg
}

func (a *app) handleEditTracker(w http.ResponseWriter, r *http.Request) {
	t, ok := a.ownedTracker(w, r)
	if !ok {
		return
	}
	f, err := a.withMove(currentUser(r), t, a.editForm(t))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	f.KindLocked, err = a.trackerHasHistory(t.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, "tracker_form.html", f)
}

// trackerHasHistory is true when a kind change would reinterpret something already stored.
// Recorded zeros count: they are count-tracker history even though they are not entries.
func (a *app) trackerHasHistory(trackerID uint) (bool, error) {
	var n int64
	if err := a.db.Model(&Entry{}).Where("tracker_id = ?", trackerID).Count(&n).Error; err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	if err := a.db.Model(&RecordedZero{}).Where("tracker_id = ?", trackerID).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func unitForKind(kind, unit string) string {
	if kind == kindNumber {
		return unit
	}
	return ""
}

func (a *app) handleUpdateTracker(w http.ResponseWriter, r *http.Request) {
	t, ok := a.ownedTracker(w, r)
	if !ok {
		return
	}
	f, err := a.withMove(currentUser(r), t, a.editForm(t))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	f.KindLocked, err = a.trackerHasHistory(t.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if f.Error = readTrackerForm(r, &f); f.Error != "" {
		a.render(w, r, http.StatusUnprocessableEntity, "tracker_form.html", f)
		return
	}
	currentKind := t.Kind
	if currentKind == "" {
		currentKind = kindCount
	}
	if f.KindLocked && f.Kind != currentKind {
		f.Error = "This tracker already has history, so its kind can't change."
		a.render(w, r, http.StatusUnprocessableEntity, "tracker_form.html", f)
		return
	}
	err = a.db.Model(&t).Updates(map[string]any{
		"name": f.Name, "icon": f.Icon, "accent": f.Accent, "log_label": f.LogLabel, "summary_display": f.SummaryDisplay,
		"kind": f.Kind, "unit": unitForKind(f.Kind, f.Unit),
	}).Error
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/trackers/%d", t.ID), http.StatusSeeOther)
}

func (a *app) handleMoveTracker(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	t, ok := a.ownedTracker(w, r)
	if !ok {
		return
	}
	dest, role, err := householdForUser(a.db, u.ID, parseID(r.PostFormValue("household")))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	if role != roleOwner {
		a.ownerOnly(w, r)
		return
	}
	if dest.ID == t.HouseholdID {
		f, err := a.withMove(u, t, a.editForm(t))
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		f.MoveError = "This tracker is already in that household."
		a.render(w, r, http.StatusUnprocessableEntity, "tracker_form.html", f)
		return
	}
	err = a.db.Transaction(func(tx *gorm.DB) error {
		var last int
		if err := tx.Model(&Tracker{}).Where("household_id = ?", dest.ID).Select("COALESCE(MAX(position), 0)").Scan(&last).Error; err != nil {
			return err
		}
		return tx.Model(&Tracker{}).Where("id = ?", t.ID).Updates(map[string]any{
			"household_id": dest.ID,
			"position":     last + 1,
		}).Error
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	redirectTo(w, r, fmt.Sprintf("/trackers/%d", t.ID))
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
