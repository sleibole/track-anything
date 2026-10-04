package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func chartFromPage(t *testing.T, body string) []chartDay {
	t.Helper()
	const open = `<script type="application/json" id="count-chart">`
	i := strings.Index(body, open)
	if i < 0 {
		t.Fatal("page has no count chart")
	}
	rest := body[i+len(open):]
	j := strings.Index(rest, "</script>")
	if j < 0 {
		t.Fatal("chart script is not closed")
	}
	var series []chartDay
	if err := json.Unmarshal([]byte(rest[:j]), &series); err != nil {
		t.Fatal(err)
	}
	return series
}

func sameCount(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func TestCountChartSeriesOnTrackerPage(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Dog ate", nil)
	path := fmt.Sprintf("/trackers/%d", tr.ID)
	quick := fmt.Sprintf(`action="/trackers/%d/quick"`, tr.ID)

	empty := owner.get(path)
	if empty.status != http.StatusOK {
		t.Fatal(empty.status)
	}
	for _, unwanted := range []string{"count-chart", "/static/chart.js", "chart.umd"} {
		if strings.Contains(empty.body, unwanted) {
			t.Fatalf("empty tracker included %q", unwanted)
		}
	}
	if !strings.Contains(empty.body, "Nothing logged in the last 30 days.") || !strings.Contains(empty.body, ">Trend</h2>") {
		t.Fatal("empty trend is missing")
	}
	if !strings.Contains(empty.body, quick) || !strings.Contains(empty.body, `method="post"`) {
		t.Fatal("log form is not a plain post")
	}
	if logAt, trendAt := strings.Index(empty.body, `data-quick`), strings.Index(empty.body, ">Trend</h2>"); logAt < 0 || trendAt < logAt {
		t.Fatal("trend is not after the log control")
	}

	// The suite never executes JavaScript. The log post still has to save.
	if r := owner.post(path+"/quick", nil); r.status != http.StatusOK || len(ts.entries(t, tr.ID)) != 1 {
		t.Fatalf("log without chart.js: %d, %d entries", r.status, len(ts.entries(t, tr.ID)))
	}

	la := location("America/Los_Angeles")
	now := ts.clock.now().In(la)
	today := localDay(now, la)
	yesterday := now.AddDate(0, 0, -1)
	gapDay := now.AddDate(0, 0, -2)
	earlier := now.AddDate(0, 0, -3)

	second := now.Add(-time.Minute)
	if localDay(second, la) != today {
		second = now
	}
	owner.post(path+"/entries", url.Values{"time": {second.Format(timeInputLayout)}})
	owner.post(path+"/zero", url.Values{"day": {yesterday.Format(dayLayout)}})
	backfill := time.Date(earlier.Year(), earlier.Month(), earlier.Day(), 18, 0, 0, 0, la)
	owner.post(path+"/entries", url.Values{"time": {backfill.Format(timeInputLayout)}})
	if len(ts.entries(t, tr.ID)) != 3 || len(ts.zeros(t, tr.ID)) != 1 {
		t.Fatalf("entries %d zeros %d", len(ts.entries(t, tr.ID)), len(ts.zeros(t, tr.ID)))
	}

	page := owner.get(path)
	series := chartFromPage(t, page.body)
	if len(series) != chartDays {
		t.Fatalf("len %d", len(series))
	}
	assertConsecutiveDays(t, series, la)
	end := series[len(series)-1]
	if end.Day != today || end.Count == nil || *end.Count != 2 || end.Label != now.Format("Jan 2") {
		t.Fatalf("today %+v", end)
	}
	y := series[len(series)-2]
	if y.Day != yesterday.Format(dayLayout) || y.Count == nil || *y.Count != 0 {
		t.Fatalf("yesterday %+v", y)
	}
	gap := series[len(series)-3]
	if gap.Day != gapDay.Format(dayLayout) || gap.Count != nil {
		t.Fatalf("gap %+v", gap)
	}
	old := series[len(series)-4]
	if old.Day != earlier.Format(dayLayout) || old.Count == nil || *old.Count != 1 {
		t.Fatalf("earlier %+v", old)
	}
	start, err := time.ParseInLocation(dayLayout, today, la)
	if err != nil {
		t.Fatal(err)
	}
	if series[0].Day != start.AddDate(0, 0, -(chartDays-1)).Format(dayLayout) {
		t.Fatalf("window starts %s", series[0].Day)
	}

	rawGap := fmt.Sprintf(`"day":"%s","label":"%s","count":null`, gap.Day, gap.Label)
	rawZero := fmt.Sprintf(`"day":"%s","label":"%s","count":0`, y.Day, y.Label)
	if !strings.Contains(page.body, rawGap) || !strings.Contains(page.body, rawZero) {
		t.Fatal("embedded JSON does not keep a gap null and a recorded zero as 0")
	}
	if !strings.Contains(page.body, "/static/chart.js") || strings.Contains(page.body, "chart.umd") || strings.Contains(page.body, "cdn.") {
		t.Fatal("page should reference the local wrapper only")
	}
	if strings.Index(page.body, `data-quick`) > strings.Index(page.body, "count-chart") {
		t.Fatal("chart moved above the log control")
	}

	memberSeries := chartFromPage(t, member.get(path).body)
	if len(memberSeries) != len(series) {
		t.Fatalf("member len %d", len(memberSeries))
	}
	for i := range series {
		if memberSeries[i].Day != series[i].Day || !sameCount(memberSeries[i].Count, series[i].Count) {
			t.Fatalf("member day %d %+v, owner %+v", i, memberSeries[i], series[i])
		}
	}

	stranger := ts.browser(t)
	stranger.signup("stranger@example.com", "")
	denied := stranger.get(path)
	if denied.status != http.StatusNotFound || strings.Contains(denied.body, "count-chart") || strings.Contains(denied.body, rawZero) {
		t.Fatalf("stranger: %d", denied.status)
	}
	anon := ts.browser(t)
	loggedOut := anon.get(path)
	if loggedOut.url.Path != "/login" || strings.Contains(loggedOut.body, "count-chart") {
		t.Fatalf("logged out landed on %s", loggedOut.url)
	}

	if home := owner.get("/").body; strings.Contains(home, "count-chart") || strings.Contains(home, "/static/chart.js") {
		t.Fatal("home loaded the chart")
	}
	owner.post(path+"/share", nil)
	token := ts.tracker(t, tr.ID).ShareToken
	if token == nil {
		t.Fatal("no share token")
	}
	if share := anon.get("/s/" + *token).body; strings.Contains(share, "count-chart") || strings.Contains(share, "/static/chart.js") || strings.Contains(share, ">Trend</h2>") {
		t.Fatal("share page loaded the chart")
	}

	req, err := http.NewRequest(http.MethodGet, ts.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("HX-Request", "true")
	partial := owner.do(req)
	if strings.Contains(partial.body, "<!doctype") || !strings.Contains(partial.body, rawZero) || !strings.Contains(partial.body, `data-quick`) || !strings.Contains(partial.body, "/static/chart.js") {
		t.Fatal("refresh dropped the chart or the log form")
	}

	if err := ts.app.db.Model(&Tracker{}).Where("id = ?", tr.ID).Update("kind", "number").Error; err != nil {
		t.Fatal(err)
	}
	number := owner.get(path).body
	if strings.Contains(number, "count-chart") || strings.Contains(number, ">Trend</h2>") || strings.Contains(number, "/static/chart.js") {
		t.Fatal("number tracker rendered a count chart")
	}
	if !strings.Contains(number, quick) {
		t.Fatal("number tracker lost the log form")
	}

	if r := owner.get("/charts"); r.status != http.StatusNotFound {
		t.Fatalf("/charts: %d", r.status)
	}
}

func TestChartWindowFollowsStoredZone(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.post("/signup", url.Values{"email": {"utc@example.com"}, "timezone": {"UTC"}})
	h := ts.personalHousehold(t, "utc@example.com")
	owner.post(fmt.Sprintf("/households/%d/invite", h.ID), nil)
	h = ts.household(t, h.ID)
	member := ts.browser(t)
	member.signup("la@example.com", "")
	member.post("/join/"+*h.InviteToken, nil)

	// 06:30 UTC is still the previous evening in Los Angeles, in both PST and PDT.
	now := ts.clock.now().UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), 6, 30, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	ts.clock.t = next

	tr := owner.createTracker(h, "Dog ate", nil)
	path := fmt.Sprintf("/trackers/%d", tr.ID)
	owner.post(path+"/quick", nil)

	utcToday := localDay(next, time.UTC)
	laToday := localDay(next, location("America/Los_Angeles"))
	if utcToday == laToday {
		t.Fatal("fixture did not cross a local midnight")
	}

	utcSeries := chartFromPage(t, owner.get(path).body)
	laSeries := chartFromPage(t, member.get(path).body)
	if len(utcSeries) != chartDays || len(laSeries) != chartDays {
		t.Fatalf("utc %d la %d", len(utcSeries), len(laSeries))
	}
	assertConsecutiveDays(t, utcSeries, time.UTC)
	assertConsecutiveDays(t, laSeries, location("America/Los_Angeles"))

	utcEnd := utcSeries[len(utcSeries)-1]
	laEnd := laSeries[len(laSeries)-1]
	if utcEnd.Day != utcToday || utcEnd.Count == nil || *utcEnd.Count != 1 || utcSeries[len(utcSeries)-2].Count != nil {
		t.Fatalf("utc end %+v previous %v", utcEnd, utcSeries[len(utcSeries)-2].Count)
	}
	if laEnd.Day != laToday || laEnd.Count == nil || *laEnd.Count != 1 {
		t.Fatalf("la end %+v", laEnd)
	}
	for _, d := range laSeries {
		if d.Day == utcToday {
			t.Fatal("Los Angeles chart included the UTC date")
		}
	}

	utcStart, err := time.ParseInLocation(dayLayout, utcToday, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	laStart, err := time.ParseInLocation(dayLayout, laToday, location("America/Los_Angeles"))
	if err != nil {
		t.Fatal(err)
	}
	if utcSeries[0].Day != utcStart.AddDate(0, 0, -(chartDays-1)).Format(dayLayout) {
		t.Fatalf("utc window starts %s", utcSeries[0].Day)
	}
	if laSeries[0].Day != laStart.AddDate(0, 0, -(chartDays-1)).Format(dayLayout) {
		t.Fatalf("la window starts %s", laSeries[0].Day)
	}
}

func TestChartJSIsVendored(t *testing.T) {
	h := newTestApp(t).routes()
	code, wrapper := get(t, h, "/static/chart.js", nil)
	if code != http.StatusOK {
		t.Fatalf("chart.js: %d", code)
	}
	if !strings.Contains(wrapper, "/static/chart.umd.min.js") {
		t.Fatal("wrapper does not load the vendored build")
	}
	for _, cdn := range []string{"cdn.", "jsdelivr", "unpkg", "cdnjs"} {
		if strings.Contains(wrapper, cdn) {
			t.Fatalf("wrapper references %s", cdn)
		}
	}
	code, umd := get(t, h, "/static/chart.umd.min.js", nil)
	if code != http.StatusOK || !strings.Contains(umd, "Chart.js v4.5.1") || !strings.Contains(umd, "window.Chart") {
		t.Fatalf("umd: %d", code)
	}
	if strings.Contains(umd, "sourceMappingURL") {
		t.Fatal("vendored build points at a source map")
	}
}
