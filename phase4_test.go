package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func valueChartFromPage(t *testing.T, body string) valueChart {
	t.Helper()
	const open = `<script type="application/json" id="value-chart">`
	i := strings.Index(body, open)
	if i < 0 {
		t.Fatal("page has no value chart")
	}
	rest := body[i+len(open):]
	j := strings.Index(rest, "</script>")
	if j < 0 {
		t.Fatal("value chart script is not closed")
	}
	var chart valueChart
	if err := json.Unmarshal([]byte(rest[:j]), &chart); err != nil {
		t.Fatal(err)
	}
	return chart
}

func TestTrackerKinds(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("club@example.com", "")
	h := ts.personalHousehold(t, "club@example.com")

	count := owner.createTracker(h, "Millie ate", nil)
	if count.Kind != kindCount || count.Unit != "" {
		t.Fatalf("count %+v", count)
	}
	number := owner.createTracker(h, "Club Mill", url.Values{"kind": {kindNumber}, "unit": {"lb"}})
	if number.Kind != kindNumber || number.Unit != "lb" {
		t.Fatalf("number %+v", number)
	}
	duration := owner.createTracker(h, "Woods run", url.Values{"kind": {kindDuration}})
	if duration.Kind != kindDuration || duration.Unit != "" {
		t.Fatalf("duration %+v", duration)
	}

	badKind := owner.post("/trackers", url.Values{"household": {fmt.Sprint(h.ID)}, "name": {"Nope"}, "kind": {"workout"}})
	if badKind.status != http.StatusUnprocessableEntity || !strings.Contains(badKind.body, "kind") {
		t.Fatalf("bad kind: %d", badKind.status)
	}
	if err := ts.app.db.Where("name = ?", "Nope").First(&Tracker{}).Error; err == nil {
		t.Fatal("invalid kind was stored")
	}
	noUnit := owner.post("/trackers", url.Values{"household": {fmt.Sprint(h.ID)}, "name": {"Weight"}, "kind": {kindNumber}})
	if noUnit.status != http.StatusUnprocessableEntity || !strings.Contains(noUnit.body, "unit") {
		t.Fatalf("missing unit: %d %s", noUnit.status, noUnit.body)
	}
	longUnit := owner.post("/trackers", url.Values{"household": {fmt.Sprint(h.ID)}, "name": {"Weight"}, "kind": {kindNumber}, "unit": {strings.Repeat("a", maxUnit+1)}})
	if longUnit.status != http.StatusUnprocessableEntity {
		t.Fatalf("long unit: %d", longUnit.status)
	}
	unitOnCount := owner.post("/trackers", url.Values{"household": {fmt.Sprint(h.ID)}, "name": {"Counted"}, "kind": {kindCount}, "unit": {"lb"}})
	if unitOnCount.status != http.StatusUnprocessableEntity || !strings.Contains(unitOnCount.body, "unit") {
		t.Fatalf("unit on count: %d", unitOnCount.status)
	}

	// Kind can change before anything is logged, and the unit is cleared when it no longer applies.
	renamed := owner.post(fmt.Sprintf("/trackers/%d", number.ID), url.Values{"name": {"Club Mill"}, "kind": {kindDuration}})
	if renamed.status != http.StatusOK {
		t.Fatalf("change kind: %d %s", renamed.status, renamed.body)
	}
	number = ts.tracker(t, number.ID)
	if number.Kind != kindDuration || number.Unit != "" {
		t.Fatalf("after change %+v", number)
	}
	owner.post(fmt.Sprintf("/trackers/%d", number.ID), url.Values{"name": {"Club Mill"}, "kind": {kindNumber}, "unit": {"lb"}})
	number = ts.tracker(t, number.ID)
	if number.Kind != kindNumber || number.Unit != "lb" {
		t.Fatalf("restored %+v", number)
	}

	owner.post(fmt.Sprintf("/trackers/%d/entries", number.ID), url.Values{"number": {"10"}})
	locked := owner.post(fmt.Sprintf("/trackers/%d", number.ID), url.Values{"name": {"Club Mill"}, "kind": {kindCount}})
	if locked.status != http.StatusUnprocessableEntity || !strings.Contains(locked.body, "already has history") {
		t.Fatalf("locked kind: %d %s", locked.status, locked.body)
	}
	if got := ts.tracker(t, number.ID); got.Kind != kindNumber || got.Unit != "lb" || got.Name != "Club Mill" {
		t.Fatalf("kind change with history applied %+v", got)
	}

	zeroed := owner.createTracker(h, "Maybe none", nil)
	owner.post(fmt.Sprintf("/trackers/%d/zero", zeroed.ID), nil)
	lockedZero := owner.post(fmt.Sprintf("/trackers/%d", zeroed.ID), url.Values{"name": {"Maybe none"}, "kind": {kindNumber}, "unit": {"lb"}})
	if lockedZero.status != http.StatusUnprocessableEntity {
		t.Fatalf("zero history: %d", lockedZero.status)
	}
	if ts.tracker(t, zeroed.ID).Kind != kindCount {
		t.Fatal("kind changed after a recorded zero")
	}
}

func TestNumberAndDurationEntries(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	club := owner.createTracker(h, "Club Mill", url.Values{"kind": {kindNumber}, "unit": {"lb"}})
	woods := owner.createTracker(h, "Woods run", url.Values{"kind": {kindDuration}})
	path := fmt.Sprintf("/trackers/%d", club.ID)
	woodsPath := fmt.Sprintf("/trackers/%d", woods.ID)

	empty := owner.get(path)
	if strings.Contains(empty.body, "Log again") || strings.Contains(empty.body, "Last value:") {
		t.Fatal("empty number tracker offered Log again")
	}
	if !strings.Contains(empty.body, `role="button"`) || !strings.Contains(empty.body, path) {
		t.Fatal("first log is not a link to the tracker")
	}
	home := owner.get("/").body
	if strings.Contains(home, "Log again") || strings.Contains(home, "Last value:") || !strings.Contains(home, ">&#43; Log</a>") {
		t.Fatal("dashboard invented a carried value")
	}

	logged := owner.post(path+"/entries", url.Values{"number": {"10"}, "note": {"warmup"}})
	if logged.status != http.StatusOK {
		t.Fatalf("log number: %d %s", logged.status, logged.body)
	}
	entries := ts.entries(t, club.ID)
	if len(entries) != 1 || entries[0].Number == nil || *entries[0].Number != 10 || entries[0].Note != "warmup" || entries[0].DurationSec != nil {
		t.Fatalf("stored %+v", entries)
	}
	page := owner.get(path).body
	if !strings.Contains(page, "Last value: 10 lb") || !strings.Contains(page, ">Log again</button>") {
		t.Fatal("10 lb did not carry forward")
	}
	noteAt := strings.Index(page, `name="note"`)
	if noteAt < 0 || strings.Contains(page[noteAt:noteAt+30], "warmup") {
		t.Fatal("note carried forward")
	}
	if !strings.Contains(page, "10 lb") || !strings.Contains(page, "— 10 lb") {
		t.Fatal("history omitted the value and unit")
	}
	marker := fmt.Sprintf(`action="%s/entries"`, path)
	form := page[strings.Index(page, marker):]
	form = form[:strings.Index(form, "</form>")]
	if !strings.Contains(form, `name="number"`) || !strings.Contains(form, `value="10"`) {
		t.Fatal("log form was not prefilled with 10")
	}

	// An older backfill does not replace the carried value.
	la := location("America/Los_Angeles")
	yesterday := ts.clock.now().In(la).AddDate(0, 0, -1).Format(timeInputLayout)
	if r := owner.post(path+"/entries", url.Values{"number": {"8"}, "time": {yesterday}}); r.status != http.StatusOK {
		t.Fatalf("backfill: %d", r.status)
	}
	page = owner.get(path).body
	if !strings.Contains(page, "Last value: 10 lb") || strings.Contains(page, "Last value: 8 lb") {
		t.Fatal("backfill replaced the carried value")
	}
	if !strings.Contains(page, "— 8 lb") {
		t.Fatal("backfilled value missing from history")
	}

	if r := owner.post(path+"/entries", url.Values{"number": {"12"}, "note": {"up"}}); r.status != http.StatusOK {
		t.Fatal(r.status)
	}
	page = owner.get(path).body
	if !strings.Contains(page, "Last value: 12 lb") {
		t.Fatal("new latest value did not carry forward")
	}

	again := owner.post(path+"/quick", url.Values{"number": {"99"}})
	if again.status != http.StatusOK {
		t.Fatalf("log again: %d %s", again.status, again.body)
	}
	entries = ts.entries(t, club.ID)
	last := entries[len(entries)-1]
	if last.Number == nil || *last.Number != 12 || last.Note != "" {
		t.Fatalf("log again stored %+v", last)
	}
	if last.OccurredAt.Before(ts.clock.now().Add(-time.Minute)) {
		t.Fatal("log again did not use now")
	}
	undo := owner.post(fmt.Sprintf("/entries/%d/undo", last.ID), nil)
	if undo.status != http.StatusOK || len(ts.entries(t, club.ID)) != 3 {
		t.Fatalf("undo log again: %d entries %d", undo.status, len(ts.entries(t, club.ID)))
	}

	// A member logs at the current time. A submitted time is ignored.
	past := "2020-01-02T03:04"
	if r := member.post(path+"/entries", url.Values{"number": {"11"}, "time": {past}, "note": {"member"}}); r.status != http.StatusOK {
		t.Fatalf("member log: %d %s", r.status, r.body)
	}
	got := ts.entries(t, club.ID)
	memberEntry := got[len(got)-1]
	if memberEntry.Number == nil || *memberEntry.Number != 11 || memberEntry.Note != "member" {
		t.Fatalf("member entry %+v", memberEntry)
	}
	if !memberEntry.OccurredAt.After(ts.clock.now().Add(-time.Minute)) {
		t.Fatalf("member time %s", memberEntry.OccurredAt)
	}
	if strings.Contains(member.get(path).body, `name="time"`) {
		t.Fatal("member sees a time field")
	}

	for _, bad := range []string{"", "nope", "NaN", "Infinity", "1e999"} {
		before := len(ts.entries(t, club.ID))
		r := owner.post(path+"/entries", url.Values{"number": {bad}})
		if r.status != http.StatusUnprocessableEntity || len(ts.entries(t, club.ID)) != before {
			t.Fatalf("number %q: %d entries %d", bad, r.status, len(ts.entries(t, club.ID)))
		}
	}
	if r := owner.post(path+"/entries", url.Values{"hours": {"1"}}); r.status != http.StatusUnprocessableEntity {
		t.Fatal("number tracker accepted a duration")
	}
	if r := owner.post(fmt.Sprintf("/trackers/%d/zero", club.ID), nil); r.status != http.StatusUnprocessableEntity || len(ts.zeros(t, club.ID)) != 0 {
		t.Fatal("number tracker recorded none")
	}

	if r := owner.post(woodsPath+"/entries", url.Values{"hours": {""}, "minutes": {"45"}, "seconds": {""}, "note": {"creek"}}); r.status != http.StatusOK {
		t.Fatalf("duration: %d %s", r.status, r.body)
	}
	woodEntries := ts.entries(t, woods.ID)
	if len(woodEntries) != 1 || woodEntries[0].DurationSec == nil || *woodEntries[0].DurationSec != 45*60 || woodEntries[0].Number != nil {
		t.Fatalf("duration stored %+v", woodEntries)
	}
	woodPage := owner.get(woodsPath).body
	if !strings.Contains(woodPage, "Last duration: 45 min") || !strings.Contains(woodPage, "— 45 min") || !strings.Contains(woodPage, ">Log again</button>") {
		t.Fatal("duration did not carry forward or show in history")
	}
	if r := owner.post(woodsPath+"/quick", url.Values{"minutes": {"5"}}); r.status != http.StatusOK {
		t.Fatal(r.status)
	}
	woodEntries = ts.entries(t, woods.ID)
	if woodEntries[1].DurationSec == nil || *woodEntries[1].DurationSec != 45*60 || woodEntries[1].Note != "" {
		t.Fatalf("duration log again %+v", woodEntries[1])
	}
	for _, form := range []url.Values{
		{"hours": {""}, "minutes": {""}, "seconds": {""}},
		{"hours": {"0"}, "minutes": {"0"}, "seconds": {"0"}},
		{"minutes": {"-5"}},
		{"hours": {"1.5"}},
		{"number": {"10"}},
	} {
		before := len(ts.entries(t, woods.ID))
		if r := owner.post(woodsPath+"/entries", form); r.status != http.StatusUnprocessableEntity || len(ts.entries(t, woods.ID)) != before {
			t.Fatalf("bad duration %v: %d", form, r.status)
		}
	}

	// Count logging is unchanged, and a value on a count tracker is rejected.
	millie := owner.createTracker(h, "Millie ate", nil)
	if r := owner.post(fmt.Sprintf("/trackers/%d/quick", millie.ID), nil); r.status != http.StatusOK || len(ts.entries(t, millie.ID)) != 1 {
		t.Fatal("count quick log broke")
	}
	if ts.entries(t, millie.ID)[0].Number != nil || ts.entries(t, millie.ID)[0].DurationSec != nil {
		t.Fatal("count entry stored a value")
	}
	if r := owner.post(fmt.Sprintf("/trackers/%d/entries", millie.ID), url.Values{"number": {"1"}, "note": {"nope"}}); r.status != http.StatusUnprocessableEntity {
		t.Fatal("count tracker accepted a number")
	}
	milliePage := owner.get(fmt.Sprintf("/trackers/%d", millie.ID)).body
	if !strings.Contains(milliePage, ">&#43; Log</button>") || strings.Contains(milliePage, "Log again") {
		t.Fatal("count tracker lost its one-tap log")
	}
	if !strings.Contains(milliePage, `src="/static/chart.js"`) {
		t.Fatal("count chart did not load")
	}
	logAt := strings.Index(milliePage, `data-quick`)
	chartAt := strings.Index(milliePage, "/static/chart.js")
	if logAt < 0 || chartAt < logAt {
		t.Fatal("logging depends on the chart script")
	}
}

func TestShareLinkLogsValues(t *testing.T) {
	ts, owner, _, h := sharedHouse(t)
	club := owner.createTracker(h, "Club Mill", url.Values{"kind": {kindNumber}, "unit": {"lb"}})
	owner.post(fmt.Sprintf("/trackers/%d/share", club.ID), nil)
	token := *ts.tracker(t, club.ID).ShareToken
	anon := ts.browser(t)
	share := "/s/" + token

	first := anon.get(share)
	if first.status != http.StatusOK || strings.Contains(first.body, "Log again") || strings.Contains(first.body, `name="note"`) || strings.Contains(first.body, `name="time"`) {
		t.Fatal("share page offered the wrong first log")
	}
	if r := anon.post(share+"/entries", url.Values{"number": {"10"}, "note": {"secret"}, "time": {"2020-01-02T03:04"}}); r.status != http.StatusOK {
		t.Fatalf("share log: %d %s", r.status, r.body)
	}
	got := ts.entries(t, club.ID)
	if len(got) != 1 || got[0].Number == nil || *got[0].Number != 10 || got[0].Note != "" || !got[0].ViaLink || got[0].RecordedByID != nil {
		t.Fatalf("share entry %+v", got)
	}
	if !got[0].OccurredAt.After(ts.clock.now().Add(-time.Minute)) {
		t.Fatal("share link backfilled")
	}
	page := anon.get(share).body
	if !strings.Contains(page, "Last value: 10 lb") || !strings.Contains(page, ">Log again</button>") {
		t.Fatal("share page did not offer Log again")
	}
	if r := anon.post(share+"/quick", url.Values{"number": {"99"}}); r.status != http.StatusOK {
		t.Fatalf("share log again: %d", r.status)
	}
	got = ts.entries(t, club.ID)
	if got[1].Number == nil || *got[1].Number != 10 || got[1].Note != "" {
		t.Fatalf("share log again trusted the client %+v", got[1])
	}
	if r := anon.post(fmt.Sprintf("/s/%s/entries/%d/undo", token, got[1].ID), nil); r.status != http.StatusOK || len(ts.entries(t, club.ID)) != 1 {
		t.Fatal("share undo failed")
	}
	if r := anon.post(share+"/zero", nil); r.status != http.StatusUnprocessableEntity {
		t.Fatal("share link recorded none on a number tracker")
	}

	woods := owner.createTracker(h, "Woods run", url.Values{"kind": {kindDuration}})
	owner.post(fmt.Sprintf("/trackers/%d/share", woods.ID), nil)
	wtoken := *ts.tracker(t, woods.ID).ShareToken
	if r := anon.post("/s/"+wtoken+"/entries", url.Values{"minutes": {"42"}}); r.status != http.StatusOK {
		t.Fatalf("share duration: %d %s", r.status, r.body)
	}
	w := ts.entries(t, woods.ID)
	if len(w) != 1 || w[0].DurationSec == nil || *w[0].DurationSec != 42*60 {
		t.Fatalf("share duration stored %+v", w)
	}
}

func TestValueChartsAndOverlay(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	club := owner.createTracker(h, "Club Mill", url.Values{"kind": {kindNumber}, "unit": {"lb"}})
	woods := owner.createTracker(h, "Woods run", url.Values{"kind": {kindDuration}, "icon": {"🌳"}})
	sore := owner.createTracker(h, "Sore back", nil)
	path := fmt.Sprintf("/trackers/%d", club.ID)

	owner.post(path+"/entries", url.Values{"number": {"10"}})
	owner.post(path+"/entries", url.Values{"number": {"12"}})
	page := owner.get(path + "?overlay=" + fmt.Sprint(sore.ID))
	chart := valueChartFromPage(t, page.body)
	if chart.Kind != kindNumber || chart.Unit != "lb" || len(chart.Points) != 2 || chart.Points[0].Y != 10 || chart.Points[1].Y != 12 {
		t.Fatalf("chart %+v", chart)
	}
	if chart.Points[0].Text != "10 lb" || chart.Points[1].Day != chart.Points[0].Day {
		t.Fatalf("points %+v", chart.Points)
	}
	if strings.Contains(page.body, `id="count-chart"`) {
		t.Fatal("number tracker rendered a count series")
	}
	if !strings.Contains(page.body, "count-overlay") || !strings.Contains(page.body, "No Sore back events") {
		t.Fatal("overlay did not stay on the value chart")
	}
	owner.post(fmt.Sprintf("/trackers/%d/quick", sore.ID), nil)
	marked := owner.get(path + "?overlay=" + fmt.Sprint(sore.ID)).body
	overlay := overlayFromPage(t, marked)
	if len(overlay.Days) != 1 || overlay.Name != "Sore back" {
		t.Fatalf("overlay %+v", overlay)
	}
	if !strings.Contains(marked, "Shaded bands mark Sore back") {
		t.Fatal("value chart did not describe the bands")
	}

	stranger := ts.browser(t)
	stranger.signup("stranger@example.com", "")
	secret := stranger.createTracker(ts.personalHousehold(t, "stranger@example.com"), "Secret", nil)
	denied := owner.get(fmt.Sprintf("%s?overlay=%d", path, secret.ID))
	if denied.status != http.StatusNotFound || strings.Contains(denied.body, "Secret") || strings.Contains(denied.body, "value-chart") {
		t.Fatalf("secret overlay: %d", denied.status)
	}

	owner.post(fmt.Sprintf("/trackers/%d/entries", woods.ID), url.Values{"minutes": {"45"}})
	woodChart := valueChartFromPage(t, owner.get(fmt.Sprintf("/trackers/%d", woods.ID)).body)
	if woodChart.Kind != kindDuration || len(woodChart.Points) != 1 || woodChart.Points[0].Y != 45*60 || woodChart.Points[0].Text != "45 min" {
		t.Fatalf("duration chart %+v", woodChart)
	}
	if len(woodChart.Days) != chartDays {
		t.Fatalf("window %d", len(woodChart.Days))
	}

	// The viewer's zone, not UTC, labels the point.
	la := location("America/Los_Angeles")
	want := ts.entries(t, woods.ID)[0].OccurredAt.In(la).Format("Jan 2, 3:04 PM")
	if woodChart.Points[0].Label != want {
		t.Fatalf("label %q want %q", woodChart.Points[0].Label, want)
	}
	memberChart := valueChartFromPage(t, member.get(fmt.Sprintf("/trackers/%d", woods.ID)).body)
	if memberChart.Points[0].Label != want || memberChart.Points[0].Y != 45*60 {
		t.Fatalf("member chart %+v", memberChart.Points)
	}

	if home := owner.get("/").body; strings.Contains(home, "value-chart") || strings.Contains(home, "/static/chart.js") {
		t.Fatal("home loaded a chart")
	}
	if r := owner.get("/charts"); r.status != http.StatusNotFound {
		t.Fatalf("/charts: %d", r.status)
	}
}

func TestTrackerCSV(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	millie := owner.createTracker(h, "Millie ate", nil)
	club := owner.createTracker(h, "Club Mill", url.Values{"kind": {kindNumber}, "unit": {"lb"}})
	woods := owner.createTracker(h, "Woods run", url.Values{"kind": {kindDuration}})

	owner.post(fmt.Sprintf("/trackers/%d/entries", millie.ID), url.Values{"note": {`say "hi",` + "\nthen"}})
	owner.post(fmt.Sprintf("/trackers/%d/zero", millie.ID), url.Values{"day": {ts.clock.now().In(location("America/Los_Angeles")).AddDate(0, 0, -1).Format(dayLayout)}})
	owner.post(fmt.Sprintf("/trackers/%d/entries", club.ID), url.Values{"number": {"10.5"}, "note": {"easy"}})
	owner.post(fmt.Sprintf("/trackers/%d/entries", woods.ID), url.Values{"minutes": {"45"}, "seconds": {"30"}})

	rows := csvRows(t, owner, millie.ID)
	if len(rows) != 3 {
		t.Fatalf("count rows %+v", rows)
	}
	if rows[0][0] != "type" {
		t.Fatalf("header %+v", rows[0])
	}
	var entry, zero []string
	for _, row := range rows[1:] {
		switch row[0] {
		case "entry":
			entry = row
		case "recorded_zero":
			zero = row
		}
	}
	if entry == nil || zero == nil {
		t.Fatalf("missing rows %+v", rows)
	}
	if !strings.Contains(entry[7], `say "hi",`) || !strings.Contains(entry[7], "then") {
		t.Fatalf("note was not quoted through the parser: %+v", entry)
	}
	if entry[1] == "" || entry[2] == "" || entry[9] != "false" || entry[8] == "" {
		t.Fatalf("count entry %+v", entry)
	}
	if zero[1] != "" || zero[2] == "" || zero[3] != "" {
		t.Fatalf("recorded zero %+v", zero)
	}

	number := csvRows(t, owner, club.ID)
	if len(number) != 2 || number[1][3] != "10.5" || number[1][4] != "lb" || number[1][5] != "" || number[1][7] != "easy" {
		t.Fatalf("number csv %+v", number)
	}
	duration := csvRows(t, owner, woods.ID)
	if len(duration) != 2 || duration[1][5] != fmt.Sprint(45*60+30) || duration[1][6] != "45 min 30 sec" {
		t.Fatalf("duration csv %+v", duration)
	}

	req, err := http.NewRequest(http.MethodGet, ts.srv.URL+fmt.Sprintf("/trackers/%d/export", club.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := owner.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/csv; charset=utf-8" || !strings.Contains(resp.Header.Get("Content-Disposition"), "Club-Mill.csv") {
		t.Fatalf("headers %v", resp.Header)
	}

	if r := member.get(fmt.Sprintf("/trackers/%d/export", club.ID)); r.status != http.StatusForbidden || strings.Contains(r.body, "10.5") {
		t.Fatalf("member export: %d", r.status)
	}
	stranger := ts.browser(t)
	stranger.signup("export-stranger@example.com", "")
	if r := stranger.get(fmt.Sprintf("/trackers/%d/export", club.ID)); r.status != http.StatusNotFound || strings.Contains(r.body, "Club Mill") || strings.Contains(r.body, "10.5") {
		t.Fatalf("stranger export: %d", r.status)
	}
	anon := ts.browser(t)
	if r := anon.get(fmt.Sprintf("/trackers/%d/export", club.ID)); r.url.Path != "/login" {
		t.Fatalf("logged out export landed on %s", r.url)
	}
}

func csvRows(t *testing.T, b *browser, trackerID uint) [][]string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, b.ts.srv.URL+fmt.Sprintf("/trackers/%d/export", trackerID), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := b.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("export %d %s", resp.StatusCode, body)
	}
	rows, err := csv.NewReader(resp.Body).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
