package main

import (
	"fmt"
	"time"
)

const (
	undoWindow  = 15 * time.Minute
	historyDays = 30
	chartDays   = 30
	dayLayout   = "2006-01-02"
)

// location returns the zone for a stored IANA name. Names are validated on the way in,
// so the UTC fallback only covers a zone that later left the tz database.
func location(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

func localDay(t time.Time, loc *time.Location) string {
	return t.In(loc).Format(dayLayout)
}

// dayBounds returns the UTC instants a local calendar day starts and ends. A day can be
// 23 or 25 hours long across a daylight-saving change.
func dayBounds(day string, loc *time.Location) (start, end time.Time, err error) {
	d, err := time.ParseInLocation(dayLayout, day, loc)
	if err != nil {
		return start, end, err
	}
	return d.UTC(), d.AddDate(0, 0, 1).UTC(), nil
}

func recent(created, now time.Time) bool {
	return now.Sub(created) < undoWindow
}

// dayCount is one local day of a count tracker: events, a recorded zero, or neither.
type dayCount struct {
	Day   string
	Date  time.Time
	Count int
	Zero  bool
}

func (d dayCount) Logged() bool { return d.Count > 0 || d.Zero }

// countsByDay groups event times by local day for the n days ending on lastDay,
// newest first. Days with neither events nor a recorded zero come back with Logged() false.
func countsByDay(events []time.Time, zeroDays []string, loc *time.Location, lastDay string, n int) ([]dayCount, error) {
	last, err := time.ParseInLocation(dayLayout, lastDay, loc)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, t := range events {
		counts[localDay(t, loc)]++
	}
	zeros := map[string]bool{}
	for _, d := range zeroDays {
		zeros[d] = true
	}
	days := make([]dayCount, 0, n)
	for i := range n {
		date := last.AddDate(0, 0, -i)
		day := date.Format(dayLayout)
		c := counts[day]
		days = append(days, dayCount{Day: day, Date: date, Count: c, Zero: c == 0 && zeros[day]})
	}
	return days, nil
}

func timesText(n int) string {
	if n == 1 {
		return "1 time"
	}
	return fmt.Sprintf("%d times", n)
}

// todaySummary is the card and share-page summary: "2 times today", "None today",
// or "Nothing logged today".
func todaySummary(count int, zero bool) string {
	switch {
	case count > 0:
		return timesText(count) + " today"
	case zero:
		return "None today"
	}
	return "Nothing logged today"
}

// summaryLine is the sentence above the log button.
// latest is nil when the tracker has no entries. zero is ignored unless display is times.
func summaryLine(display string, count int, zero bool, latest *time.Time, loc *time.Location, now time.Time) string {
	switch display {
	case summaryDone:
		if count > 0 {
			return "Done today"
		}
		return "Not done today"
	case summaryLast:
		return lastOccurrenceSummary(latest, loc, now)
	default:
		return todaySummary(count, zero)
	}
}

// lastOccurrenceSummary formats the latest entry in the page's zone.
// Yesterday is the previous local calendar day, not 24 hours earlier.
func lastOccurrenceSummary(latest *time.Time, loc *time.Location, now time.Time) string {
	if latest == nil {
		return "Never logged"
	}
	when := latest.In(loc)
	clock := when.Format("3:04 PM")
	day := localDay(*latest, loc)
	if day == localDay(now, loc) {
		return "Last: " + clock
	}
	if day == localDay(now.In(loc).AddDate(0, 0, -1), loc) {
		return "Last: Yesterday, " + clock
	}
	return "Last: " + when.Format("Mon, Jan 2, 3:04 PM")
}

func (d dayCount) Summary() string {
	switch {
	case d.Count > 0:
		return timesText(d.Count)
	case d.Zero:
		return "None"
	}
	return "Nothing logged"
}

// chartDay is one local calendar day on the count chart.
// Count is nil when nothing was logged. A recorded zero is 0, not a gap.
type chartDay struct {
	Day   string `json:"day"`
	Label string `json:"label"`
	Count *int   `json:"count"`
}

// chartSeries is the n local days ending on lastDay, oldest first.
// It uses countsByDay, so a day's count means the same thing as a history row.
// A later week or month chart should bucket these days rather than count events again.
func chartSeries(events []time.Time, zeroDays []string, loc *time.Location, lastDay string, n int) ([]chartDay, error) {
	days, err := countsByDay(events, zeroDays, loc, lastDay, n)
	if err != nil {
		return nil, err
	}
	series := make([]chartDay, len(days))
	for i, d := range days {
		series[len(days)-1-i] = chartDay{
			Day:   d.Day,
			Label: d.Date.Format("Jan 2"),
			Count: loggedCount(d),
		}
	}
	return series, nil
}

func loggedCount(d dayCount) *int {
	if !d.Logged() {
		return nil
	}
	n := d.Count
	return &n
}

// chartHasData is false when every day in the window is a gap.
// A single recorded zero is enough to draw.
func chartHasData(series []chartDay) bool {
	for _, d := range series {
		if d.Count != nil {
			return true
		}
	}
	return false
}
