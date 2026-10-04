package main

import (
	"bytes"
	"encoding/json"
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

func TestSummaryLine(t *testing.T) {
	la := location("America/Los_Angeles")
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, la)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	ptr := func(v time.Time) *time.Time { return &v }

	now := at("2026-06-15 12:00")
	otherDay := at("2026-06-01 09:00")
	for _, tc := range []struct {
		display string
		count   int
		zero    bool
		latest  *time.Time
		now     time.Time
		want    string
	}{
		{"", 0, false, nil, now, "Nothing logged today"},
		{"", 0, true, nil, now, "None today"},
		{"", 1, false, nil, now, "1 time today"},
		{summaryTimes, 3, false, nil, now, "3 times today"},
		{summaryDone, 0, false, nil, now, "Not done today"},
		{summaryDone, 0, true, ptr(otherDay), now, "Not done today"},
		{summaryDone, 1, false, nil, now, "Done today"},
		{summaryDone, 4, true, nil, now, "Done today"},
		{summaryLast, 0, true, nil, now, "Never logged"},
		{summaryLast, 3, false, nil, now, "Never logged"},
	} {
		if got := summaryLine(tc.display, tc.count, tc.zero, tc.latest, la, tc.now); got != tc.want {
			t.Errorf("summaryLine(%q, %d, %v) = %q, want %q", tc.display, tc.count, tc.zero, got, tc.want)
		}
	}

	same := at("2026-06-15 18:42")
	if got, want := summaryLine(summaryLast, 0, false, ptr(same), la, same), "Last: "+same.Format("3:04 PM"); got != want {
		t.Errorf("same day = %q, want %q", got, want)
	}

	yesterday := at("2026-06-14 20:15")
	if got, want := summaryLine(summaryLast, 0, true, ptr(yesterday), la, now), "Last: Yesterday, "+yesterday.Format("3:04 PM"); got != want {
		t.Errorf("yesterday = %q, want %q", got, want)
	}

	// Spring forward: 23:30 the evening before is still yesterday on the 23-hour day.
	springEntry := at("2026-03-07 23:30")
	springNow := at("2026-03-08 12:00")
	if got := summaryLine(summaryLast, 0, false, ptr(springEntry), la, springNow); got != "Last: Yesterday, 11:30 PM" {
		t.Errorf("spring forward = %q", got)
	}

	older := at("2026-01-02 15:04")
	later := at("2026-01-05 12:00")
	if got, want := summaryLine(summaryLast, 9, true, ptr(older), la, later), "Last: "+older.Format("Mon, Jan 2, 3:04 PM"); got != want {
		t.Errorf("older = %q, want %q", got, want)
	}
}

func TestChartSeriesLocalDays(t *testing.T) {
	la := location("America/Los_Angeles")
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, la)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	events := []time.Time{
		at("2026-06-14 23:30"),
		at("2026-06-15 00:30"), // just after local midnight, a different day
		at("2026-06-15 18:00"),
		at("2026-06-16 09:00"),
	}
	// The 16th has an event, so its recorded zero does not become the day's value.
	zeros := []string{"2026-06-13", "2026-06-16"}

	days, err := countsByDay(events, zeros, la, "2026-06-17", 5)
	if err != nil {
		t.Fatal(err)
	}
	series, err := chartSeries(events, zeros, la, "2026-06-17", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != len(days) {
		t.Fatalf("%d chart days, %d history days", len(series), len(days))
	}
	assertConsecutiveDays(t, series, la)
	if series[0].Day != "2026-06-13" || series[len(series)-1].Day != "2026-06-17" {
		t.Fatalf("order %+v", series)
	}
	for i, d := range days {
		got := series[len(series)-1-i]
		if got.Day != d.Day || got.Label != d.Date.Format("Jan 2") {
			t.Fatalf("day %d chart %+v, history %+v", i, got, d)
		}
		switch {
		case !d.Logged():
			if got.Count != nil {
				t.Fatalf("%s gap plotted as %d", d.Day, *got.Count)
			}
		case got.Count == nil || *got.Count != d.Count:
			t.Fatalf("%s chart count %v, history %d", d.Day, got.Count, d.Count)
		}
	}

	zero, one, two := 0, 1, 2
	want := map[string]*int{
		"2026-06-13": &zero,
		"2026-06-14": &one,
		"2026-06-15": &two,
		"2026-06-16": &one,
		"2026-06-17": nil,
	}
	for _, d := range series {
		w := want[d.Day]
		if (w == nil) != (d.Count == nil) || (w != nil && *w != *d.Count) {
			t.Fatalf("%s = %v, want %v", d.Day, d.Count, w)
		}
	}

	b, err := json.Marshal(series)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"day":"2026-06-17","label":"Jun 17","count":null`)) {
		t.Fatalf("gap was not null: %s", b)
	}
	if !bytes.Contains(b, []byte(`"day":"2026-06-13","label":"Jun 13","count":0`)) {
		t.Fatalf("recorded zero was not 0: %s", b)
	}

	empty, err := chartSeries(nil, nil, la, "2026-06-17", chartDays)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != chartDays || chartHasData(empty) {
		t.Fatalf("empty window len %d usable %v", len(empty), chartHasData(empty))
	}
	if !chartHasData(series) {
		t.Fatal("a recorded zero should be enough to draw")
	}
}

func TestChartSeriesUsesLocationNotUTC(t *testing.T) {
	// 06:30 UTC is still the previous evening in Los Angeles.
	instant := time.Date(2026, 6, 15, 6, 30, 0, 0, time.UTC)
	la := location("America/Los_Angeles")
	if localDay(instant, time.UTC) == localDay(instant, la) {
		t.Fatal("fixture is the same calendar day in both zones")
	}

	utc, err := chartSeries([]time.Time{instant}, nil, time.UTC, "2026-06-15", 3)
	if err != nil {
		t.Fatal(err)
	}
	pacific, err := chartSeries([]time.Time{instant}, nil, la, "2026-06-14", 3)
	if err != nil {
		t.Fatal(err)
	}
	if utc[0].Day != "2026-06-13" || utc[2].Day != "2026-06-15" || utc[2].Count == nil || *utc[2].Count != 1 || utc[1].Count != nil {
		t.Fatalf("utc %+v", utc)
	}
	if pacific[0].Day != "2026-06-12" || pacific[2].Day != "2026-06-14" || pacific[2].Count == nil || *pacific[2].Count != 1 {
		t.Fatalf("pacific %+v", pacific)
	}
	for _, d := range pacific {
		if d.Day == "2026-06-15" {
			t.Fatal("Los Angeles series included the UTC date")
		}
	}
}

func TestChartSeriesDSTDoesNotSkipOrRepeatDays(t *testing.T) {
	la := location("America/Los_Angeles")
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, la)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// 2026-03-08 is the spring-forward day: 02:00 becomes 03:00.
	spring, err := chartSeries([]time.Time{
		at("2026-03-07 23:30"),
		at("2026-03-08 00:30"),
		at("2026-03-08 03:30"),
	}, nil, la, "2026-03-20", chartDays)
	if err != nil {
		t.Fatal(err)
	}
	if len(spring) != chartDays {
		t.Fatalf("spring len %d", len(spring))
	}
	assertConsecutiveDays(t, spring, la)
	var march7, march8 int
	seen := map[string]int{}
	for _, d := range spring {
		seen[d.Day]++
		if d.Day == "2026-03-07" && d.Count != nil {
			march7 = *d.Count
		}
		if d.Day == "2026-03-08" && d.Count != nil {
			march8 = *d.Count
		}
	}
	if seen["2026-03-08"] != 1 || march7 != 1 || march8 != 2 {
		t.Fatalf("spring-forward day seen %d, march 7 = %d, march 8 = %d", seen["2026-03-08"], march7, march8)
	}

	// Fall back: 01:15 happens twice. Both instants belong to November 1.
	pdt := time.Date(2026, 11, 1, 8, 15, 0, 0, time.UTC) // 01:15 PDT
	pst := time.Date(2026, 11, 1, 9, 15, 0, 0, time.UTC) // 01:15 PST
	if localDay(pdt, la) != "2026-11-01" || localDay(pst, la) != "2026-11-01" {
		t.Fatalf("fall-back local days %s and %s", localDay(pdt, la), localDay(pst, la))
	}
	fall, err := chartSeries([]time.Time{pdt, pst}, []string{"2026-11-02"}, la, "2026-11-05", chartDays)
	if err != nil {
		t.Fatal(err)
	}
	if len(fall) != chartDays {
		t.Fatalf("fall len %d", len(fall))
	}
	assertConsecutiveDays(t, fall, la)
	var nov1, nov2 *int
	seen = map[string]int{}
	for _, d := range fall {
		seen[d.Day]++
		if d.Day == "2026-11-01" {
			nov1 = d.Count
		}
		if d.Day == "2026-11-02" {
			nov2 = d.Count
		}
	}
	if seen["2026-11-01"] != 1 || nov1 == nil || *nov1 != 2 {
		t.Fatalf("nov 1 seen %d count %v", seen["2026-11-01"], nov1)
	}
	if nov2 == nil || *nov2 != 0 {
		t.Fatalf("nov 2 recorded zero = %v", nov2)
	}
}

func assertConsecutiveDays(t *testing.T, series []chartDay, loc *time.Location) {
	t.Helper()
	if len(series) == 0 {
		t.Fatal("no days")
	}
	prev, err := time.ParseInLocation(dayLayout, series[0].Day, loc)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(series); i++ {
		want := prev.AddDate(0, 0, 1).Format(dayLayout)
		if series[i].Day != want {
			t.Fatalf("day %d = %s, want %s after %s", i, series[i].Day, want, prev.Format(dayLayout))
		}
		prev, err = time.ParseInLocation(dayLayout, series[i].Day, loc)
		if err != nil {
			t.Fatal(err)
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
