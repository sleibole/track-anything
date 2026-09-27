package main

import (
	"testing"
	"time"
)

func TestDayBoundsAcrossDST(t *testing.T) {
	la := location("America/Los_Angeles")
	for day, hours := range map[string]float64{
		"2026-03-08": 23, // spring forward
		"2026-11-01": 25, // fall back
		"2026-06-15": 24,
	} {
		start, end, err := dayBounds(day, la)
		if err != nil {
			t.Fatal(err)
		}
		if got := end.Sub(start).Hours(); got != hours {
			t.Errorf("%s: %v hours, want %v", day, got, hours)
		}
		if localDay(start, la) != day || localDay(end.Add(-time.Second), la) != day {
			t.Errorf("%s: bounds %v–%v fall outside the day", day, start, end)
		}
	}
	if _, _, err := dayBounds("2026-02-30", la); err == nil {
		t.Error("invalid day accepted")
	}
}

func TestCountsByDay(t *testing.T) {
	la := location("America/Los_Angeles")
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, la)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	events := []time.Time{
		at("2026-03-07 23:30"),
		at("2026-03-08 00:30"), // after the spring-forward night starts, same local day
		at("2026-03-08 23:30"),
		at("2026-03-10 08:00"),
	}
	zeros := []string{"2026-03-09", "2026-03-10"} // the 10th has an event, so its zero doesn't count

	days, err := countsByDay(events, zeros, la, "2026-03-11", 5)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		day    string
		count  int
		zero   bool
		logged bool
		text   string
	}{
		{"2026-03-11", 0, false, false, "Nothing logged"},
		{"2026-03-10", 1, false, true, "1 time"},
		{"2026-03-09", 0, true, true, "None"},
		{"2026-03-08", 2, false, true, "2 times"},
		{"2026-03-07", 1, false, true, "1 time"},
	}
	if len(days) != len(want) {
		t.Fatalf("%d days, want %d", len(days), len(want))
	}
	for i, w := range want {
		d := days[i]
		if d.Day != w.day || d.Count != w.count || d.Zero != w.zero || d.Logged() != w.logged || d.Summary() != w.text {
			t.Errorf("day %d = %+v (logged %v, %q), want %+v", i, d, d.Logged(), d.Summary(), w)
		}
	}
}

func TestTodaySummary(t *testing.T) {
	for _, tc := range []struct {
		count int
		zero  bool
		want  string
	}{
		{0, false, "Nothing logged today"},
		{0, true, "None today"},
		{1, false, "1 time today"},
		{3, false, "3 times today"},
	} {
		if got := todaySummary(tc.count, tc.zero); got != tc.want {
			t.Errorf("todaySummary(%d, %v) = %q, want %q", tc.count, tc.zero, got, tc.want)
		}
	}
}

func TestUndoWindow(t *testing.T) {
	now := time.Now()
	if !recent(now.Add(-14*time.Minute-59*time.Second), now) {
		t.Error("an entry from 14:59 ago should be recent")
	}
	if recent(now.Add(-15*time.Minute), now) {
		t.Error("an entry from 15 minutes ago should not be recent")
	}
}

func TestTrackerIconFallsBackToTallyMarks(t *testing.T) {
	tally, _ := icon("tallymarks")
	paw, _ := icon("paw")
	for v, want := range map[string]string{
		"":       string(tally),
		"rocket": string(tally), // not on the picker, e.g. removed later
		"paw":    string(paw),
		"🐶":      `<span class="emoji" aria-hidden="true">🐶</span>`,
	} {
		got, err := trackerIcon(v)
		if err != nil || string(got) != want {
			t.Errorf("trackerIcon(%q) = %q, %v", v, got, err)
		}
	}
	for _, c := range trackerIcons {
		if _, err := trackerIcon(c.Value); err != nil {
			t.Errorf("picker icon %q: %v", c.Value, err)
		}
	}
}
