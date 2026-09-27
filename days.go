package main

import (
	"fmt"
	"time"
)

const (
	undoWindow  = 15 * time.Minute
	historyDays = 30
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

func (d dayCount) Summary() string {
	switch {
	case d.Count > 0:
		return timesText(d.Count)
	case d.Zero:
		return "None"
	}
	return "Nothing logged"
}
