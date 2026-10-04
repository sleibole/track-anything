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
	logAt := strings.Index(empty.body, `data-quick`)
	trendAt := strings.Index(empty.body, ">Trend</h2>")
	historyAt := strings.Index(empty.body, ">History</h2>")
	if logAt < 0 || trendAt < logAt || historyAt < trendAt {
		t.Fatal("trend is not between the log control and history")
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
	if !strings.Contains(wrapper, "/static/chart.umd.min.js") || !strings.Contains(wrapper, "overlayBands") || !strings.Contains(wrapper, "count-overlay") {
		t.Fatal("wrapper does not load the vendored build and the overlay bands")
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

func overlayFromPage(t *testing.T, body string) overlayChart {
	t.Helper()
	const open = `<script type="application/json" id="count-overlay">`
	i := strings.Index(body, open)
	if i < 0 {
		t.Fatal("page has no overlay")
	}
	rest := body[i+len(open):]
	j := strings.Index(rest, "</script>")
	if j < 0 {
		t.Fatal("overlay script is not closed")
	}
	var overlay overlayChart
	if err := json.Unmarshal([]byte(rest[:j]), &overlay); err != nil {
		t.Fatal(err)
	}
	return overlay
}

func TestOverlayPicker(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	millie := owner.createTracker(h, "Millie ate", nil)
	woods := owner.createTracker(h, "Woods run", url.Values{"icon": {"trees"}})
	old := owner.createTracker(h, "Old run", nil)
	if r := owner.post(fmt.Sprintf("/trackers/%d/archive", old.ID), nil); r.status != http.StatusOK {
		t.Fatalf("archive: %d", r.status)
	}
	numbered := owner.createTracker(h, "Numbered", nil)
	if err := ts.app.db.Model(&Tracker{}).Where("id = ?", numbered.ID).Update("kind", "number").Error; err != nil {
		t.Fatal(err)
	}
	privateHouse := ts.personalHousehold(t, "member@example.com")
	private := member.createTracker(privateHouse, "Private run", nil)

	stranger := ts.browser(t)
	stranger.signup("stranger@example.com", "")
	secret := stranger.createTracker(ts.personalHousehold(t, "stranger@example.com"), "Secret outing", nil)
	la := location("America/Los_Angeles")
	secretAt := time.Date(2026, 4, 4, 4, 44, 0, 0, la)
	stranger.post(fmt.Sprintf("/trackers/%d/entries", secret.ID), url.Values{"time": {secretAt.Format(timeInputLayout)}})

	path := fmt.Sprintf("/trackers/%d", millie.ID)
	page := owner.get(path)
	if page.status != http.StatusOK {
		t.Fatal(page.status)
	}
	if !strings.Contains(page.body, "Show events from") || !strings.Contains(page.body, "Woods run") || !strings.Contains(page.body, "M16 5l3 3") {
		t.Fatal("picker is missing the woods icon or name")
	}
	if strings.Contains(page.body, fmt.Sprintf("?overlay=%d", millie.ID)) {
		t.Fatal("primary tracker is an overlay choice")
	}
	if !strings.Contains(page.body, fmt.Sprintf("?overlay=%d", woods.ID)) {
		t.Fatal("woods is not an overlay choice")
	}
	for _, hidden := range []string{"Old run", "Numbered", "Private run", "Secret outing"} {
		if strings.Contains(page.body, hidden) {
			t.Fatalf("picker includes %s", hidden)
		}
	}
	if strings.Contains(page.body, fmt.Sprintf("?overlay=%d", private.ID)) || strings.Contains(page.body, fmt.Sprintf("?overlay=%d", secret.ID)) {
		t.Fatal("picker links a tracker the owner cannot see")
	}
	if !strings.Contains(page.body, `aria-current="page">None`) {
		t.Fatal("none is not the current overlay")
	}

	memberPage := member.get(path).body
	if !strings.Contains(memberPage, "Woods run") || !strings.Contains(memberPage, "Private run") || strings.Contains(memberPage, "Secret outing") {
		t.Fatal("member picker has the wrong trackers")
	}

	// No primary history: an overlay with events still does not draw a chart.
	owner.post(fmt.Sprintf("/trackers/%d/quick", woods.ID), nil)
	empty := owner.get(fmt.Sprintf("%s?overlay=%d", path, woods.ID))
	if !strings.Contains(empty.body, "Nothing logged in the last 30 days.") || strings.Contains(empty.body, "count-chart") || strings.Contains(empty.body, "count-overlay") {
		t.Fatal("empty primary drew a chart for the overlay")
	}
	if !strings.Contains(empty.body, `aria-current="page"`) || !strings.Contains(empty.body, "Woods run") {
		t.Fatal("selected overlay is not marked")
	}

	denied := owner.get(fmt.Sprintf("%s?overlay=%d", path, secret.ID))
	if denied.status != http.StatusNotFound || strings.Contains(denied.body, "Secret outing") || strings.Contains(denied.body, "4:44") || strings.Contains(denied.body, "count-overlay") {
		t.Fatalf("secret overlay: %d", denied.status)
	}
	archived := owner.get(fmt.Sprintf("%s?overlay=%d", path, old.ID))
	if archived.status != http.StatusNotFound || strings.Contains(archived.body, "Old run") {
		t.Fatalf("archived overlay: %d", archived.status)
	}
	otherKind := owner.get(fmt.Sprintf("%s?overlay=%d", path, numbered.ID))
	if otherKind.status != http.StatusNotFound || strings.Contains(otherKind.body, "Numbered") {
		t.Fatalf("number overlay: %d", otherKind.status)
	}
	if !strings.Contains(owner.get(path).body, `method="post"`) || !strings.Contains(owner.get(path).body, fmt.Sprintf(`action="/trackers/%d/quick"`, millie.ID)) {
		t.Fatal("log form is not a plain post")
	}
}

func TestOverlayEventsOnChart(t *testing.T) {
	ts, owner, _, h := sharedHouse(t)
	millie := owner.createTracker(h, "Millie ate", nil)
	woods := owner.createTracker(h, "Woods run", url.Values{"icon": {"🌳"}})
	path := fmt.Sprintf("/trackers/%d", millie.ID)
	woodsPath := fmt.Sprintf("/trackers/%d", woods.ID)

	la := location("America/Los_Angeles")
	now := ts.clock.now().In(la)
	today := localDay(now, la)
	yesterday := now.AddDate(0, 0, -1)
	outside := now.AddDate(0, 0, -chartDays)
	first := now.AddDate(0, 0, -(chartDays - 1))

	owner.post(path+"/quick", nil)
	owner.post(path+"/zero", url.Values{"day": {yesterday.Format(dayLayout)}})

	none := owner.get(path)
	if strings.Contains(none.body, "count-overlay") {
		t.Fatal("no overlay request embedded overlay data")
	}
	plain := chartFromPage(t, none.body)

	// A recorded zero on the overlay tracker is not an event.
	owner.post(woodsPath+"/zero", url.Values{"day": {yesterday.Format(dayLayout)}})
	quiet := owner.get(fmt.Sprintf("%s?overlay=%d", path, woods.ID))
	if !strings.Contains(quiet.body, "No Woods run events in the last 30 days.") {
		t.Fatal("missing empty overlay message")
	}
	quietOverlay := overlayFromPage(t, quiet.body)
	if quietOverlay.Name != "Woods run" || len(quietOverlay.Days) != 0 {
		t.Fatalf("empty overlay %+v", quietOverlay)
	}
	if !strings.Contains(quiet.body, "/static/chart.js") || strings.Contains(quiet.body, "chart.umd") {
		t.Fatal("overlay page should reference the local wrapper only")
	}

	morning := time.Date(first.Year(), first.Month(), first.Day(), 9, 0, 0, 0, la)
	afternoon := time.Date(first.Year(), first.Month(), first.Day(), 15, 30, 0, 0, la)
	owner.post(woodsPath+"/entries", url.Values{"time": {afternoon.Format(timeInputLayout)}})
	owner.post(woodsPath+"/entries", url.Values{"time": {morning.Format(timeInputLayout)}})
	owner.post(woodsPath+"/entries", url.Values{"time": {outside.Format(timeInputLayout)}})
	owner.post(woodsPath+"/quick", nil)

	page := owner.get(fmt.Sprintf("%s?overlay=%d", path, woods.ID))
	if strings.Contains(page.body, "No Woods run events in the last 30 days.") {
		t.Fatal("events were described as absent")
	}
	if !strings.Contains(page.body, "Shaded bands mark Woods run events on those days.") {
		t.Fatal("page does not describe the bands")
	}
	series := chartFromPage(t, page.body)
	if len(series) != len(plain) {
		t.Fatalf("overlay changed the series length %d vs %d", len(series), len(plain))
	}
	for i := range series {
		if series[i].Day != plain[i].Day || !sameCount(series[i].Count, plain[i].Count) {
			t.Fatalf("day %d overlay %+v plain %+v", i, series[i], plain[i])
		}
	}
	end := series[len(series)-1]
	if end.Day != today || end.Count == nil || *end.Count != 1 {
		t.Fatalf("today %+v", end)
	}
	if series[len(series)-2].Count == nil || *series[len(series)-2].Count != 0 {
		t.Fatalf("yesterday %+v", series[len(series)-2])
	}
	if series[len(series)-3].Count != nil {
		t.Fatalf("gap %+v", series[len(series)-3])
	}
	if series[0].Count != nil {
		t.Fatalf("first chart day was logged on the primary %+v", series[0])
	}

	overlay := overlayFromPage(t, page.body)
	if overlay.Name != "Woods run" || overlay.Mark != "🌳" {
		t.Fatalf("overlay %+v", overlay)
	}
	if len(overlay.Days) != 2 {
		t.Fatalf("days %+v", overlay.Days)
	}
	if overlay.Days[0].Day != first.Format(dayLayout) || len(overlay.Days[0].Times) != 2 || overlay.Days[0].Times[0] != "9:00 AM" || overlay.Days[0].Times[1] != "3:30 PM" {
		t.Fatalf("first overlay day %+v", overlay.Days[0])
	}
	if overlay.Days[1].Day != today || len(overlay.Days[1].Times) != 1 {
		t.Fatalf("today overlay %+v", overlay.Days[1])
	}
	for _, day := range overlay.Days {
		if day.Day == outside.Format(dayLayout) {
			t.Fatal("overlay included a day outside the chart window")
		}
	}
	raw, err := json.Marshal(overlay)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), outside.Format(dayLayout)) {
		t.Fatal("overlay JSON included a day outside the chart window")
	}

	self := owner.get(fmt.Sprintf("%s?overlay=%d", path, millie.ID))
	if self.status != http.StatusOK || strings.Contains(self.body, "count-overlay") {
		t.Fatal("overlaying a tracker on itself returned overlay data")
	}
	if owner.get(path+"?overlay=nope").status != http.StatusOK || strings.Contains(owner.get(path+"?overlay=nope").body, "count-overlay") {
		t.Fatal("a non-numeric overlay was not ignored")
	}

	req, err := http.NewRequest(http.MethodPost, ts.srv.URL+path+"/quick", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Referer", ts.srv.URL+fmt.Sprintf("%s?overlay=%d", path, woods.ID))
	logged := owner.do(req)
	if logged.status != http.StatusOK || logged.url.Query().Get("overlay") != fmt.Sprint(woods.ID) {
		t.Fatalf("log dropped the overlay: %d %s", logged.status, logged.url)
	}
	if !strings.Contains(logged.body, "count-overlay") || !strings.Contains(logged.body, `data-quick`) {
		t.Fatal("returned page lost the chart or the log form")
	}
	if len(ts.entries(t, millie.ID)) != 2 {
		t.Fatalf("entries %d", len(ts.entries(t, millie.ID)))
	}

	partialReq, err := http.NewRequest(http.MethodGet, ts.srv.URL+fmt.Sprintf("%s?overlay=%d", path, woods.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	partialReq.Header.Set("HX-Request", "true")
	partial := owner.do(partialReq)
	if strings.Contains(partial.body, "<!doctype") || !strings.Contains(partial.body, "count-overlay") || !strings.Contains(partial.body, "Show events from") {
		t.Fatal("refresh dropped the overlay")
	}
	if !strings.Contains(partial.body, fmt.Sprintf(`hx-get="/trackers/%d?overlay=%d"`, millie.ID, woods.ID)) {
		t.Fatal("refresh target dropped the overlay query")
	}
}

func TestOverlayFollowsStoredZone(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.post("/signup", url.Values{"email": {"utc@example.com"}, "timezone": {"UTC"}})
	h := ts.personalHousehold(t, "utc@example.com")
	owner.post(fmt.Sprintf("/households/%d/invite", h.ID), nil)
	h = ts.household(t, h.ID)
	member := ts.browser(t)
	member.signup("la@example.com", "")
	member.post("/join/"+*h.InviteToken, nil)

	now := ts.clock.now().UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), 6, 30, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	ts.clock.t = next

	millie := owner.createTracker(h, "Millie ate", nil)
	woods := owner.createTracker(h, "Woods run", nil)
	owner.post(fmt.Sprintf("/trackers/%d/quick", millie.ID), nil)
	owner.post(fmt.Sprintf("/trackers/%d/quick", woods.ID), nil)

	utcToday := localDay(next, time.UTC)
	laToday := localDay(next, location("America/Los_Angeles"))
	if utcToday == laToday {
		t.Fatal("fixture did not cross a local midnight")
	}

	path := fmt.Sprintf("/trackers/%d?overlay=%d", millie.ID, woods.ID)
	utcOverlay := overlayFromPage(t, owner.get(path).body)
	laOverlay := overlayFromPage(t, member.get(path).body)
	if len(utcOverlay.Days) != 1 || utcOverlay.Days[0].Day != utcToday || utcOverlay.Days[0].Times[0] != next.In(time.UTC).Format("3:04 PM") {
		t.Fatalf("utc %+v", utcOverlay.Days)
	}
	if len(laOverlay.Days) != 1 || laOverlay.Days[0].Day != laToday {
		t.Fatalf("la %+v", laOverlay.Days)
	}
	for _, day := range laOverlay.Days {
		if day.Day == utcToday {
			t.Fatal("Los Angeles overlay used the UTC date")
		}
	}
	utcSeries := chartFromPage(t, owner.get(path).body)
	laSeries := chartFromPage(t, member.get(path).body)
	if utcSeries[len(utcSeries)-1].Day != utcToday || laSeries[len(laSeries)-1].Day != laToday {
		t.Fatal("primary series did not follow the viewer zone")
	}
}
