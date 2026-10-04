package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxUnit        = 16
	maxDurationSec = 99*3600 + 59*60 + 59
	maxNumberAbs   = 1e9
)

// formatDuration is the one human-facing duration: "45 sec", "25 min", "1 hr 10 min".
// A zero length is "0 sec". Callers that reject zero never show that.
func formatDuration(sec int) string {
	if sec < 0 {
		sec = 0
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	var parts []string
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%d hr", h))
	}
	if m > 0 {
		parts = append(parts, fmt.Sprintf("%d min", m))
	}
	if s > 0 {
		parts = append(parts, fmt.Sprintf("%d sec", s))
	}
	if len(parts) == 0 {
		return "0 sec"
	}
	return strings.Join(parts, " ")
}

// durationFields splits a stored duration into the hours, minutes, and seconds inputs.
func durationFields(sec int) (hours, minutes, seconds string) {
	if sec < 0 {
		sec = 0
	}
	return strconv.Itoa(sec / 3600), strconv.Itoa((sec % 3600) / 60), strconv.Itoa(sec % 60)
}

func formatNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func formatMeasurement(v float64, unit string) string {
	if unit == "" {
		return formatNumber(v)
	}
	return formatNumber(v) + " " + unit
}

func parseNumber(raw string) (float64, string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, "Enter a number."
	}
	lower := strings.ToLower(s)
	if strings.Contains(lower, "nan") || strings.Contains(lower, "inf") {
		return 0, "Enter a valid number."
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, "Enter a valid number."
	}
	if v > maxNumberAbs || v < -maxNumberAbs {
		return 0, "Enter a smaller number."
	}
	return v, ""
}

func parseDurationPart(raw string, max int) (int, string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, ""
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, "Enter hours, minutes, and seconds as whole numbers."
	}
	if n > max {
		if max == 99 {
			return 0, "Keep hours to 99."
		}
		return 0, fmt.Sprintf("Keep that to %d.", max)
	}
	return n, ""
}

// parseDurationFields reads the hours, minutes, and seconds inputs.
// Empty parts are zero. The total has to be at least one second.
func parseDurationFields(hours, minutes, seconds string) (int, string) {
	h, msg := parseDurationPart(hours, 99)
	if msg != "" {
		return 0, msg
	}
	m, msg := parseDurationPart(minutes, 59)
	if msg != "" {
		return 0, msg
	}
	s, msg := parseDurationPart(seconds, 59)
	if msg != "" {
		return 0, msg
	}
	total := h*3600 + m*60 + s
	if total <= 0 {
		return 0, "Enter a duration."
	}
	if total > maxDurationSec {
		return 0, "Keep the duration under 100 hours."
	}
	return total, ""
}

func validUnit(unit string) string {
	if unit == "" {
		return "Give a number tracker a unit, such as lb or °F."
	}
	if utf8.RuneCountInString(unit) > maxUnit {
		return fmt.Sprintf("Keep the unit to %d characters.", maxUnit)
	}
	for _, r := range unit {
		if r < 0x20 || r == 0x7f {
			return "Use a short unit, such as lb or °F."
		}
	}
	return ""
}

// valuePoint is one observation on a number or duration chart.
// X is days from the start of the chart window. Y is the number, or seconds.
// Text is what a person reads. Several entries stay several points.
type valuePoint struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Day   string  `json:"day"`
	Label string  `json:"label"`
	Text  string  `json:"text"`
}

// axisDay is one local day on a value chart's horizontal axis.
// X is the middle of that day, where an overlay band sits.
type axisDay struct {
	Day   string  `json:"day"`
	Label string  `json:"label"`
	X     float64 `json:"x"`
}

type valueChart struct {
	Kind   string       `json:"kind"`
	Unit   string       `json:"unit,omitempty"`
	Days   []axisDay    `json:"days"`
	Points []valuePoint `json:"points"`
}

// valuePoints keeps each observation in the same window as the count chart.
// Entries outside the window, and entries with no value for this kind, are left out.
// Points are oldest first so the line follows time.
func valuePoints(entries []Entry, t Tracker, loc *time.Location, lastDay string, n int) ([]valuePoint, []axisDay, error) {
	start, end, err := chartWindow(lastDay, loc, n)
	if err != nil {
		return nil, nil, err
	}
	first := start.In(loc)
	days := make([]axisDay, n)
	for i := range n {
		date := first.AddDate(0, 0, i)
		days[i] = axisDay{Day: date.Format(dayLayout), Label: date.Format("Jan 2"), X: float64(i) + 0.5}
	}
	pts := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.OccurredAt.Before(start) || !e.OccurredAt.Before(end) {
			continue
		}
		pts = append(pts, e)
	}
	slices.SortFunc(pts, func(a, b Entry) int {
		if c := a.OccurredAt.Compare(b.OccurredAt); c != 0 {
			return c
		}
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})
	points := make([]valuePoint, 0, len(pts))
	dayLen := 24 * time.Hour
	for _, e := range pts {
		y, text, ok := observation(e, t)
		if !ok {
			continue
		}
		points = append(points, valuePoint{
			X:     e.OccurredAt.Sub(start).Seconds() / dayLen.Seconds(),
			Y:     y,
			Day:   localDay(e.OccurredAt, loc),
			Label: e.OccurredAt.In(loc).Format("Jan 2, 3:04 PM"),
			Text:  text,
		})
	}
	return points, days, nil
}

func observation(e Entry, t Tracker) (y float64, text string, ok bool) {
	switch {
	case t.IsNumber():
		if e.Number == nil {
			return 0, "", false
		}
		return *e.Number, formatMeasurement(*e.Number, t.Unit), true
	case t.IsDuration():
		if e.DurationSec == nil || *e.DurationSec <= 0 {
			return 0, "", false
		}
		return float64(*e.DurationSec), formatDuration(*e.DurationSec), true
	default:
		return 0, "", false
	}
}
