package main

import (
	"encoding/json"
	"errors"
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
	{kindCumulative, "Cumulative — amounts that add up, such as points"},
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
	Tracker        Tracker
	Count          int
	Summary        string
	Zone           string // IANA name used for this summary, for the optimistic last-occurrence clock
	Link           string // tracker page; empty on the share page
	QuickURL       string
	UndoURL        string // set while the viewer's latest entry can still be undone
	Back           string // where the log and undo buttons return; empty means the tracker page
	EntriesURL     string // number Log again posts the value field here
	Carry          string // "Last value: 10 lb" or "Last duration: 42 min"; empty when nothing carries forward
	CarryValue     string // number input, without the unit
	CarryHours     string
	CarryMinutes   string
	CarrySeconds   string
	LogAgain       bool   // a duration repeats Carry in one tap; a number's button says Log again. False when there is no previous value
	NumberField    bool   // home shows the number input even when nothing carries forward. An inline error sets it so that field stays
	GoalProgress   bool   // cumulative tracker with a repeating goal
	ProgressValue  string // current progress toward the next goal, already formatted
	ProgressMax    string // the goal, already formatted
	GoalsCompleted string // "1 goal completed"; empty when there is no goal
	TotalEarned    string // "40 points total earned"; empty when the headline is the total
	Rewards        string // "1 reward available · 2 redeemed."; empty when there is no goal
	CanRedeem      bool   // a household member, an active goal, and at least one reward left
	RedeemToken    string // one redeem submission; not part of the refresh rev
	RewardError    string // a failed redeem, shown beside that action
	RewardNote     string // the note from a failed redeem, kept in the field
	Notice         string // "Goal completed!" after an entry that finished one or more goals
	Error          string // validation message for this tracker; empty when the last submit worked
	Reorder        bool   // dashboard only: the card has a handle for the viewer's own order
	HouseholdLabel string // dashboard only: household name on a shared tracker; empty on a personal one
	HouseholdURL   string
	DataRev        string // displayed values, used to highlight a real change. Undo windows are not part of it
	ViewRev        string // displayed values plus undo controls, used to decide whether a refresh swaps the card
	OOB            string // out-of-band swap for a refresh fragment; empty on a full page
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
	c := cardFor(t, s, latest, now, loc, viewerID)
	if !t.IsCumulative() {
		return c, nil
	}
	amounts, err := a.entryNumbers(t.ID)
	if err != nil {
		return trackerCard{}, err
	}
	applyCumulative(&c, t, amounts)
	redeemed, err := a.redemptionCount(t.ID)
	if err != nil {
		return trackerCard{}, err
	}
	applyRewards(&c, t, amounts, redeemed, viewerID != nil)
	if c.CanRedeem {
		key, err := newRequestKey()
		if err != nil {
			return trackerCard{}, err
		}
		c.RedeemToken = key
	}
	return c, nil
}

func applyRewards(c *trackerCard, t Tracker, amounts []float64, redeemed int64, member bool) {
	if !t.IsCumulative() || t.Goal == nil || *t.Goal <= 0 {
		return
	}
	avail := rewardsAvailable(earnedGoals(amounts, t.Goal), redeemed)
	c.Rewards = rewardsLine(avail, redeemed)
	c.CanRedeem = member && avail.Sign() > 0
}

func applyCumulative(c *trackerCard, t Tracker, amounts []float64) {
	st := progressStateOf(amounts, t.Goal, t.Unit)
	c.Summary = st.Headline
	c.GoalProgress = st.HasGoal
	c.ProgressValue = st.Progress
	c.ProgressMax = st.Goal
	c.GoalsCompleted = st.Completed
	c.TotalEarned = st.Earned
}

func (a *app) entryNumbers(trackerID uint) ([]float64, error) {
	return loadEntryAmounts(a.db, trackerID)
}

func loadEntryAmounts(db *gorm.DB, trackerID uint) ([]float64, error) {
	var entries []Entry
	err := db.Select("number").Where("tracker_id = ? AND number IS NOT NULL", trackerID).Find(&entries).Error
	if err != nil {
		return nil, err
	}
	amounts := make([]float64, 0, len(entries))
	for _, e := range entries {
		if e.Number != nil {
			amounts = append(amounts, *e.Number)
		}
	}
	return amounts, nil
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
	// Times today names the latest log today. Those rows are already newest-first.
	// Last occurrence keeps the latest entry on any day.
	summaryAt := occurred
	if t.SummaryMode() == summaryTimes && len(s.Entries) > 0 {
		summaryAt = &s.Entries[0].OccurredAt
	}
	c := trackerCard{
		Tracker:    t,
		Count:      s.Count,
		Summary:    summaryLine(t.SummaryDisplay, s.Count, s.Zero != nil, summaryAt, loc, now),
		Zone:       loc.String(),
		Link:       fmt.Sprintf("/trackers/%d", t.ID),
		QuickURL:   fmt.Sprintf("/trackers/%d/quick", t.ID),
		EntriesURL: fmt.Sprintf("/trackers/%d/entries", t.ID),
	}
	if viewerID == nil {
		c.Link = ""
		c.QuickURL = "/s/" + *t.ShareToken + "/quick"
		c.EntriesURL = "/s/" + *t.ShareToken + "/entries"
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
	Running     string // cumulative: lifetime total as of this entry, such as "40 points total"
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
		case (t.IsNumber() || t.IsCumulative()) && e.Number != nil:
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
		emails[u.ID] = u.Label()
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
	Card             trackerCard
	IsOwner          bool
	Today            todayState
	ZeroNew          bool
	Entries          []entryView
	History          []historyDay
	ChartScript      template.HTML // daily count series; empty when there is nothing to plot
	OverlayScript    template.HTML // overlay days; empty when no overlay is drawn
	OverlayChoices   []Tracker     // other count trackers this person can see
	Overlay          *Tracker      // selected overlay; nil means none
	OverlayID        uint          // 0 when none; safe to compare in the template
	OverlayEmpty     bool          // selected overlay has no events in the chart window
	PageURL          string        // this page, including ?overlay= when one is selected
	NowValue         string        // default for the owner's time field
	Note             string
	NumberValue      string // log form; the carried value unless this render is correcting a post
	Hours            string
	Minutes          string
	Seconds          string
	Error            string
	PeriodNote       string // cumulative history subtotal, set only when older entries exist
	Redemptions      []redemptionView
	CanUndoRewards   bool
	Keep             keepPrompt
	RewardGap        string // redeemed ahead of goals earned; detail page only
	RewardWindowNote string // history dates do not limit the redemption list
	LogDataRev       string
	LogViewRev       string
	TrendDataRev     string
	TrendViewRev     string
	HistoryDataRev   string
	HistoryViewRev   string
	OOBLog           bool
	OOBTrend         bool
	OOBHistory       bool
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
	var lifetime []Entry
	if t.IsCumulative() {
		if err := a.db.Where("tracker_id = ?", t.ID).Order("occurred_at, id").Find(&lifetime).Error; err != nil {
			return p, err
		}
		labels := runningLabels(lifetime, t.Unit)
		stampRunning(p.Entries, labels)
		for i := range p.History {
			stampRunning(p.History[i].Entries, labels)
		}
		p.PeriodNote = historySubtotalNote(lifetime, from, t.Unit)
		rows, err := a.listRedemptions(t.ID)
		if err != nil {
			return p, err
		}
		p.Redemptions, err = a.redemptionViews(rows, loc, now)
		if err != nil {
			return p, err
		}
		p.CanUndoRewards = len(rows) > 0
		var amounts []float64
		for _, e := range lifetime {
			if e.Number != nil {
				amounts = append(amounts, *e.Number)
			}
		}
		p.RewardGap = rewardGapText(int64(len(rows)), earnedGoals(amounts, t.Goal), t.Goal != nil && *t.Goal > 0)
		if p.PeriodNote != "" && len(rows) > 0 {
			p.RewardWindowNote = "Every redemption. The dates in History do not limit this list."
		}
	}
	choices, err := visibleCountTrackers(a.db, u.ID, t.ID)
	if err != nil {
		return p, err
	}
	p.OverlayChoices = choices
	var script template.HTML
	if t.IsCount() {
		script, err = countChartScript(today, older, zeros, loc)
	} else if t.IsCumulative() {
		script, err = embedValueChart(lifetime, t, loc, today.Day, chartDays)
	} else {
		script, err = valueChartScript(append(append([]Entry{}, today.Entries...), older...), t, loc, today.Day)
	}
	if err != nil {
		return p, err
	}
	p.ChartScript = script
	// No primary history means no chart, even when the overlay has events.
	if overlay != nil && script != "" {
		days, err := a.overlayEvents(overlay, loc, today.Day, chartDays)
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
	stampTrackerRegions(&p)
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
// The window is chartWindow for n local days, the same range the primary series uses.
func (a *app) overlayEvents(overlay *Tracker, loc *time.Location, lastDay string, n int) ([]overlayDay, error) {
	start, end, err := chartWindow(lastDay, loc, n)
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
	return overlayDays(times, loc, lastDay, n)
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
// page shows a sentence instead of a chart.
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
	return embedCountChart(events, zeroDays, loc, today.Day, chartDays)
}

// embedCountChart is the count series for n local days ending on lastDay.
// An empty result means every day in the window is a gap. json.Marshal escapes
// <, >, and &, so the series cannot close the script element.
func embedCountChart(events []time.Time, zeroDays []string, loc *time.Location, lastDay string, n int) (template.HTML, error) {
	series, err := chartSeries(events, zeroDays, loc, lastDay, n)
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
	return embedValueChart(entries, t, loc, lastDay, chartDays)
}

// embedValueChart is one point per entry over n local days. No points means no chart.
func embedValueChart(entries []Entry, t Tracker, loc *time.Location, lastDay string, n int) (template.HTML, error) {
	points, days, err := valuePoints(entries, t, loc, lastDay, n)
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
	p.Keep = keepPrompt{Show: a.showKeep(u)}
	if isTrackerRefresh(r) {
		a.writeTrackerRefresh(w, r, p)
		return
	}
	p.Card.Notice = goalNoticeFrom(r.URL.Query(), t.ID)
	if p.Card.Notice == "" {
		p.Card.Notice = rewardNoticeFrom(r.URL.Query(), t.ID)
	}
	a.render(w, r, http.StatusOK, "tracker_show.html", p)
}

// applyInlineLogError keeps a failed number Log again on this tracker: the message
// sits in the card, and the field keeps what was typed.
func applyInlineLogError(c *trackerCard, r *http.Request, t Tracker, msg string) {
	c.Back = r.PostFormValue("back")
	c.Error = msg
	if t.IsNumber() || t.IsCumulative() {
		c.CarryValue = r.PostFormValue("number")
		if t.IsNumber() {
			c.NumberField = true
		}
	}
}

// writeLogControl swaps just this tracker's log control. HX-Retarget keeps a
// validation error on the card instead of replacing the page.
func (a *app) writeLogControl(w http.ResponseWriter, r *http.Request, status int, page string, c trackerCard) {
	w.Header().Set("HX-Retarget", fmt.Sprintf("#log-%d", c.Tracker.ID))
	w.Header().Set("HX-Reswap", "outerHTML")
	a.renderBlock(w, r, status, page, "logControlFragment", c)
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
	Goal           string // cumulative repeating goal; blank means none
	KindLocked     bool   // history exists, so the kind cannot change
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
	Request        string
	Minimal        bool
}

func (a *app) newTrackerForm(u *User) (trackerForm, error) {
	key, err := newRequestKey()
	if err != nil {
		return trackerForm{}, err
	}
	f := trackerForm{
		Icons: trackerIcons, Accents: trackerAccents, Summaries: summaryDisplays, SummaryDisplay: summaryTimes,
		Kinds: trackerKinds, Kind: kindCount, Request: key,
	}
	hs, err := ownedHouseholds(a.db, u.ID)
	f.Households = hs
	if len(f.Households) > 0 {
		f.HouseholdID = f.Households[0].ID
	}
	if err != nil {
		return f, err
	}
	n, err := a.ownedTrackerCount(u.ID)
	if err != nil {
		return f, err
	}
	if n == 0 {
		f.Minimal = true
	}
	return f, nil
}

func (a *app) ownedTrackerCount(userID uint) (int64, error) {
	var n int64
	err := a.db.Model(&Tracker{}).
		Joins("JOIN household_members ON household_members.household_id = trackers.household_id").
		Where("household_members.user_id = ? AND household_members.role = ?", userID, roleOwner).
		Count(&n).Error
	return n, err
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
	f.Goal = strings.TrimSpace(r.PostFormValue("goal"))
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
		if f.Goal != "" {
			return "A goal is only for a cumulative tracker."
		}
		return validUnit(f.Unit)
	case f.Kind == kindCumulative:
		if msg := validUnitText(f.Unit); msg != "" {
			return msg
		}
		_, msg := parseGoal(f.Goal)
		return msg
	case f.Goal != "":
		return "A goal is only for a cumulative tracker."
	case f.Unit != "":
		return "A unit is only for a number or cumulative tracker."
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
	if name := strings.TrimSpace(r.URL.Query().Get("name")); trackerNameProblem(name) == "" {
		f.Name = name
		f.Minimal = true
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
	f.Request = r.PostFormValue("request")
	if posted := r.PostFormValue("minimal"); posted == "1" {
		f.Minimal = true
	}
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

	var t Tracker
	err = a.withImmediate(func(tx *gorm.DB) error {
		if _, role, err := householdForUser(tx, u.ID, f.HouseholdID); err != nil {
			return err
		} else if role != roleOwner {
			return errNotOwner
		}
		if u.IsGuest() {
			var n int64
			err := tx.Model(&Tracker{}).
				Joins("JOIN household_members ON household_members.household_id = trackers.household_id").
				Where("household_members.user_id = ? AND household_members.role = ?", u.ID, roleOwner).
				Count(&n).Error
			if err != nil {
				return err
			}
			if n >= maxGuestTrackers {
				return errGuestTrackers
			}
		}
		if validRequestKey(f.Request) {
			var prev TrackerCreate
			err := tx.Take(&prev, "request_key = ?", f.Request).Error
			if err == nil {
				if prev.UserID != u.ID {
					return errRequestUsed
				}
				return tx.Take(&t, prev.TrackerID).Error
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		var last int
		if err := tx.Model(&Tracker{}).Where("household_id = ?", f.HouseholdID).Select("COALESCE(MAX(position), 0)").Scan(&last).Error; err != nil {
			return err
		}
		t = Tracker{
			HouseholdID: f.HouseholdID, Name: f.Name, Icon: f.Icon, Accent: f.Accent,
			LogLabel: f.LogLabel, SummaryDisplay: f.SummaryDisplay, Kind: f.Kind,
			Unit: unitForKind(f.Kind, f.Unit), Goal: goalForKind(f.Kind, f.Goal), Position: last + 1,
		}
		if err := tx.Create(&t).Error; err != nil {
			return err
		}
		if !validRequestKey(f.Request) {
			return nil
		}
		return tx.Create(&TrackerCreate{RequestKey: f.Request, UserID: u.ID, TrackerID: t.ID, CreatedAt: a.now()}).Error
	})
	if errors.Is(err, errGuestTrackers) {
		f.Error = guestTrackLimitMessage
		a.render(w, r, http.StatusUnprocessableEntity, "tracker_form.html", f)
		return
	}
	if errors.Is(err, errRequestUsed) {
		f.Error = "Reload the page and try again."
		a.render(w, r, http.StatusConflict, "tracker_form.html", f)
		return
	}
	if errors.Is(err, errNotOwner) {
		a.ownerOnly(w, r)
		return
	}
	if err != nil {
		a.lookupError(w, r, err)
		return
	}
	redirectTo(w, r, fmt.Sprintf("/trackers/%d", t.ID))
}

var (
	errNotOwner      = errors.New("not the owner")
	errGuestTrackers = errors.New("guest tracker limit")
	errRequestUsed   = errors.New("create request used")
)

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
		Goal:           goalInput(t.Goal),
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
	if kind == kindNumber || kind == kindCumulative {
		return unit
	}
	return ""
}

// goalColumn is the value stored for Goal. A nil clears the column.
func goalColumn(kind, raw string) any {
	g := goalForKind(kind, raw)
	if g == nil {
		return nil
	}
	return *g
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
		"kind": f.Kind, "unit": unitForKind(f.Kind, f.Unit), "goal": goalColumn(f.Kind, f.Goal),
	}).Error
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	redirectTo(w, r, fmt.Sprintf("/trackers/%d", t.ID))
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
	redirectTo(w, r, fmt.Sprintf("/households/%d", t.HouseholdID))
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
