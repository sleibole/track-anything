package main

import (
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// The charts page is a larger view of one tracker. The logging home and the
// tracker page's trend stay as they are. The query string is the whole state.

type chartsPage struct {
	Trackers       []Tracker
	OverlayChoices []Tracker
	Primary        *Tracker
	PrimaryID      uint
	Overlay        *Tracker
	OverlayID      uint
	Span           chartSpan
	Spans          []chartSpan
	ChartScript    template.HTML
	OverlayScript  template.HTML
	ChartNote      string
	Lifetime       string // cumulative lifetime headline; a range does not change it
	LifetimeDetail string // goals completed and total earned, when a goal is set
	OverlayNote    string // set when the overlay has no events in the window
	EmptyNote      string
	Aria           string
}

func (p chartsPage) HrefTracker(id uint) string {
	overlay := p.OverlayID
	if overlay == id {
		overlay = 0
	}
	return chartsURL(id, overlay, p.Span.Key)
}

func (p chartsPage) HrefOverlay(id uint) string {
	return chartsURL(p.PrimaryID, id, p.Span.Key)
}

func (p chartsPage) HrefSpan(key string) string {
	return chartsURL(p.PrimaryID, p.OverlayID, key)
}

func chartsURL(trackerID, overlayID uint, spanKey string) string {
	if spanKey == "" {
		spanKey = chartSpan30
	}
	v := url.Values{}
	if trackerID != 0 {
		v.Set("tracker", strconv.FormatUint(uint64(trackerID), 10))
	}
	if overlayID != 0 && overlayID != trackerID {
		v.Set("overlay", strconv.FormatUint(uint64(overlayID), 10))
	}
	if spanKey != chartSpan30 {
		v.Set("range", spanKey)
	}
	if len(v) == 0 {
		return "/charts"
	}
	return "/charts?" + v.Encode()
}

func (a *app) handleCharts(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	q := r.URL.Query()

	rawRange := q.Get("range")
	span, ok := chartSpanByKey(rawRange)
	if !ok {
		a.notFound(w, r)
		return
	}

	trackers, err := visibleTrackers(a.db, u.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	var primary *Tracker
	if raw := q.Get("tracker"); raw != "" {
		id := parseID(raw)
		if id == 0 {
			a.notFound(w, r)
			return
		}
		t, _, err := trackerForUser(a.db, u.ID, id)
		if err != nil {
			a.lookupError(w, r, err)
			return
		}
		primary = &t
	}

	overlay, err := a.chartsOverlay(u.ID, primary, q.Get("overlay"))
	if err != nil {
		a.lookupError(w, r, err)
		return
	}

	p := chartsPage{
		Trackers: trackers,
		Span:     span,
		Spans:    chartSpanChoices,
		Primary:  primary,
	}
	if primary != nil {
		p.PrimaryID = primary.ID
	}
	if overlay != nil {
		p.Overlay = overlay
		p.OverlayID = overlay.ID
	}
	skip := uint(0)
	if primary != nil {
		skip = primary.ID
	}
	choices, err := visibleCountTrackers(a.db, u.ID, skip)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p.OverlayChoices = choices

	if primary == nil {
		a.render(w, r, http.StatusOK, "charts.html", p)
		return
	}

	if primary.IsCumulative() {
		amounts, err := a.entryNumbers(primary.ID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		redeemed, err := a.redemptionCount(primary.ID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		st := progressStateOf(amounts, primary.Goal, primary.Unit)
		p.Lifetime = st.Headline
		if st.HasGoal {
			p.Lifetime += " toward next goal"
			p.LifetimeDetail = st.Completed + ". " + st.Earned + ". " + rewardsLine(rewardsAvailable(earnedGoals(amounts, primary.Goal), redeemed), redeemed)
		}
	}

	loc := location(u.TimeZone)
	now := a.now().In(loc)
	last := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	lastDay := last.Format(dayLayout)
	n := span.Days(last)
	script, err := a.loadChartScript(*primary, loc, lastDay, n)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	p.ChartScript = script
	if script == "" {
		p.EmptyNote = "Nothing logged in " + span.Phrase + "."
		a.render(w, r, http.StatusOK, "charts.html", p)
		return
	}

	note, aria := chartSentences(*primary, span)
	if overlay != nil {
		days, err := a.overlayEvents(overlay, loc, lastDay, n)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if len(days) == 0 {
			p.OverlayNote = "No " + overlay.Name + " events in " + span.Phrase + "."
		} else {
			note += " Shaded bands mark " + overlay.Name + " events on those days."
			aria += " Shaded bands mark " + overlay.Name + " events."
		}
		embedded, err := overlayChartScript(overlay, days)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		p.OverlayScript = embedded
	}
	p.ChartNote = note
	p.Aria = aria
	a.render(w, r, http.StatusOK, "charts.html", p)
}

// chartsOverlay loads one other count tracker from ?overlay=.
// A missing id, a non-numeric id, or the primary tracker's own id means no overlay.
// An id the user cannot see, an archived tracker, or a tracker that is not a count is a 404.
func (a *app) chartsOverlay(userID uint, primary *Tracker, raw string) (*Tracker, error) {
	if raw == "" {
		return nil, nil
	}
	id := parseID(raw)
	if id == 0 {
		return nil, nil
	}
	if primary != nil {
		return a.resolveOverlay(userID, *primary, id)
	}
	other, _, err := trackerForUser(a.db, userID, id)
	if err != nil {
		return nil, err
	}
	if other.Kind != "count" {
		return nil, gorm.ErrRecordNotFound
	}
	return &other, nil
}

func chartSentences(t Tracker, span chartSpan) (note, aria string) {
	switch {
	case t.IsDuration():
		note = "Duration over " + span.Phrase + ". Each point is one entry."
		aria = note
	case t.IsNumber():
		note = "Value over " + span.Phrase
		if t.Unit != "" {
			note += ", in " + t.Unit
		}
		note += ". Each point is one entry."
		aria = note
	case t.IsCumulative():
		note = "Running total over " + span.Phrase + ". Each point is the total earned through that entry, including anything earlier."
		aria = note
	default:
		note = "Count per day over " + span.Phrase + ". A gap is a day with nothing logged."
		aria = "Count per day for " + span.Phrase + ". A gap is a day with nothing logged. Zero is a recorded none."
	}
	return note, aria
}

// loadChartScript builds the same JSON the tracker page embeds, for n local days
// ending on lastDay. An empty result means there is nothing to plot.
func (a *app) loadChartScript(t Tracker, loc *time.Location, lastDay string, n int) (template.HTML, error) {
	start, end, err := chartWindow(lastDay, loc, n)
	if err != nil {
		return "", err
	}
	var entries []Entry
	q := a.db.Where("tracker_id = ? AND occurred_at >= ? AND occurred_at < ?", t.ID, start, end)
	if t.IsCumulative() {
		// Earlier amounts stay in the running total. The window only chooses which points are drawn.
		q = a.db.Where("tracker_id = ? AND occurred_at < ?", t.ID, end)
	}
	if err := q.Order("occurred_at, id").Find(&entries).Error; err != nil {
		return "", err
	}
	if !t.IsCount() {
		return embedValueChart(entries, t, loc, lastDay, n)
	}
	events := make([]time.Time, len(entries))
	for i, e := range entries {
		events[i] = e.OccurredAt
	}
	var zeros []RecordedZero
	if err := a.db.Where("tracker_id = ? AND day >= ? AND day <= ?", t.ID, localDay(start, loc), lastDay).
		Find(&zeros).Error; err != nil {
		return "", err
	}
	zeroDays := make([]string, len(zeros))
	for i := range zeros {
		zeroDays[i] = zeros[i].Day
	}
	return embedCountChart(events, zeroDays, loc, lastDay, n)
}
