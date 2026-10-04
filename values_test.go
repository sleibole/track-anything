package main

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		sec  int
		want string
	}{
		{0, "0 sec"},
		{45, "45 sec"},
		{60, "1 min"},
		{25 * 60, "25 min"},
		{42 * 60, "42 min"},
		{3600, "1 hr"},
		{3600 + 10*60, "1 hr 10 min"},
		{2*3600 + 3, "2 hr 3 sec"},
		{3600 + 60 + 5, "1 hr 1 min 5 sec"},
		{2700, "45 min"},
	}
	for _, tc := range cases {
		if got := formatDuration(tc.sec); got != tc.want {
			t.Errorf("formatDuration(%d) = %q, want %q", tc.sec, got, tc.want)
		}
	}
}

func TestParseNumberRejectsNonFinite(t *testing.T) {
	for _, raw := range []string{"", "nan", "NaN", "inf", "+Inf", "-Infinity", "1e999", "nope"} {
		if _, msg := parseNumber(raw); msg == "" {
			t.Errorf("parseNumber(%q) accepted a bad value", raw)
		}
	}
	v, msg := parseNumber("10.5")
	if msg != "" || v != 10.5 {
		t.Fatalf("10.5 -> %v %q", v, msg)
	}
	v, msg = parseNumber("-3")
	if msg != "" || v != -3 {
		t.Fatalf("-3 -> %v %q", v, msg)
	}
	if _, msg := parseNumber("1e10"); msg == "" {
		t.Fatal("huge number accepted")
	}
}

func TestParseDurationFields(t *testing.T) {
	sec, msg := parseDurationFields("", "45", "")
	if msg != "" || sec != 45*60 {
		t.Fatalf("45 min -> %d %q", sec, msg)
	}
	sec, msg = parseDurationFields("1", "10", "")
	if msg != "" || sec != 3600+10*60 {
		t.Fatalf("1 hr 10 min -> %d %q", sec, msg)
	}
	if _, msg := parseDurationFields("", "", ""); msg == "" {
		t.Fatal("empty duration accepted")
	}
	if _, msg := parseDurationFields("0", "0", "0"); msg == "" {
		t.Fatal("zero duration accepted")
	}
	if _, msg := parseDurationFields("-1", "0", "0"); msg == "" {
		t.Fatal("negative duration accepted")
	}
	if _, msg := parseDurationFields("1.5", "", ""); msg == "" {
		t.Fatal("fractional hours accepted")
	}
	if _, msg := parseDurationFields("100", "0", "0"); msg == "" {
		t.Fatal("100 hours accepted")
	}
}

func TestValuePointsKeepObservationsAndWindow(t *testing.T) {
	la := location("America/Los_Angeles")
	last := time.Date(2026, 6, 17, 0, 0, 0, 0, la)
	start, _, err := chartWindow("2026-06-17", la, 5)
	if err != nil {
		t.Fatal(err)
	}
	ten := 10.0
	twelve := 12.0
	outside := 4.0
	club := Tracker{Kind: kindNumber, Unit: "lb"}
	entries := []Entry{
		{ID: 1, OccurredAt: start.Add(-time.Hour), Number: &outside},
		{ID: 2, OccurredAt: start.Add(2 * time.Hour), Number: &ten},
		{ID: 3, OccurredAt: start.Add(3 * time.Hour), Number: &twelve},
		{ID: 4, OccurredAt: last.Add(15 * time.Hour), Number: &ten},
	}
	points, days, err := valuePoints(entries, club, la, "2026-06-17", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 5 || days[0].Day != "2026-06-13" || days[4].Day != "2026-06-17" {
		t.Fatalf("days %+v", days)
	}
	if len(points) != 3 {
		t.Fatalf("points %+v", points)
	}
	if points[0].Y != 10 || points[1].Y != 12 || points[0].Text != "10 lb" || points[1].Text != "12 lb" {
		t.Fatalf("same-day points were combined: %+v", points[:2])
	}
	if points[0].Day != "2026-06-13" || points[2].Day != "2026-06-17" {
		t.Fatalf("days %+v", points)
	}
	if points[0].X >= points[1].X || points[1].X >= points[2].X {
		t.Fatalf("points are not in time order: %+v", points)
	}
}

func TestValuePointsUseZoneAndDST(t *testing.T) {
	la := location("America/Los_Angeles")
	// 2026-03-08 is the US spring-forward. 06:30 UTC is 10:30 PM on March 7 in Los Angeles
	// and 11:30 PM on March 7 in a zone that has not changed, but 06:30 UTC on March 8
	// is 11:30 PM March 7 PDT (UTC-7) wait: after spring forward PDT is UTC-7.
	// March 8 2026 09:30 UTC is 01:30 PST (UTC-8) before the jump, and 10:30 UTC is 03:30 PDT.
	before := time.Date(2026, 3, 8, 9, 30, 0, 0, time.UTC) // 1:30 AM PST
	after := time.Date(2026, 3, 8, 10, 30, 0, 0, time.UTC) // 3:30 AM PDT
	mins := 30
	woods := Tracker{Kind: kindDuration}
	points, days, err := valuePoints([]Entry{
		{ID: 1, OccurredAt: before, DurationSec: &mins},
		{ID: 2, OccurredAt: after, DurationSec: &mins},
	}, woods, la, "2026-03-08", chartDays)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 || points[0].Day != "2026-03-08" || points[1].Day != "2026-03-08" {
		t.Fatalf("dst days %+v", points)
	}
	if points[0].Label != "Mar 8, 1:30 AM" || points[1].Label != "Mar 8, 3:30 AM" {
		t.Fatalf("labels %q %q", points[0].Label, points[1].Label)
	}
	if points[0].Y != 30 || points[1].Text != "30 sec" {
		t.Fatalf("duration %+v", points)
	}
	if days[len(days)-1].Day != "2026-03-08" || days[0].Day != "2026-02-07" {
		t.Fatalf("window %s .. %s", days[0].Day, days[len(days)-1].Day)
	}
	utcPoints, _, err := valuePoints([]Entry{{ID: 1, OccurredAt: before, DurationSec: &mins}}, woods, time.UTC, "2026-03-08", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(utcPoints) != 1 || utcPoints[0].Label != "Mar 8, 9:30 AM" {
		t.Fatalf("utc %+v", utcPoints)
	}
}
