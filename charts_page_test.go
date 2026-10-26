package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func seriesDay(t *testing.T, series []chartDay, day string) chartDay {
	t.Helper()
	for _, d := range series {
		if d.Day == day {
			return d
		}
	}
	t.Fatalf("series has no %s", day)
	return chartDay{}
}

func seriesHas(series []chartDay, day string) bool {
	for _, d := range series {
		if d.Day == day {
			return true
		}
	}
	return false
}

func logAt(t *testing.T, b *browser, id uint, when time.Time) {
	t.Helper()
	r := b.post(fmt.Sprintf("/trackers/%d/entries", id), url.Values{"time": {when.Format(timeInputLayout)}})
	if r.status != http.StatusOK {
		t.Fatalf("log %s: %d %s", when.Format(time.RFC3339), r.status, r.body)
	}
}

func TestChartsRequiresLogin(t *testing.T) {
	ts := newTestServer(t)
	anon := ts.browser(t)
	r := anon.get("/charts")
	if r.url.Path != "/login" || r.url.Query().Get("next") != "/charts" || strings.Contains(r.body, "count-chart") {
		t.Fatalf("logged out landed on %s", r.url)
	}
	r = anon.get("/charts?tracker=4&overlay=9&range=90d")
	if r.url.Path != "/login" || r.url.Query().Get("next") != "/charts?tracker=4&overlay=9&range=90d" {
		t.Fatalf("logged out next %s", r.url)
	}
	if strings.Contains(r.body, "count-chart") || strings.Contains(r.body, "value-chart") {
		t.Fatal("login page included a chart")
	}
}

func TestChartsPickerAccess(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	millie := owner.createTracker(h, "Millie ate", url.Values{"icon": {"paw"}})
	woods := owner.createTracker(h, "Woods run", url.Values{"icon": {"trees"}})
	old := owner.createTracker(h, "Old run", nil)
	if r := owner.post(fmt.Sprintf("/trackers/%d/archive", old.ID), nil); r.status != http.StatusOK {
		t.Fatalf("archive: %d", r.status)
	}
	club := owner.createTracker(h, "Club mill", url.Values{"kind": {kindNumber}, "unit": {"lb"}})
	outing := owner.createTracker(h, "Long outing", url.Values{"kind": {kindDuration}})
	privateHouse := ts.personalHousehold(t, "member@example.com")
	private := member.createTracker(privateHouse, "Private run", nil)

	stranger := ts.browser(t)
	stranger.signup("stranger@example.com", "")
	secret := stranger.createTracker(ts.personalHousehold(t, "stranger@example.com"), "Secret outing", nil)
	la := location("America/Los_Angeles")
	secretAt := time.Date(2026, 4, 4, 4, 44, 0, 0, la)
	stranger.post(fmt.Sprintf("/trackers/%d/entries", secret.ID), url.Values{"time": {secretAt.Format(timeInputLayout)}})

	page := owner.get("/charts")
	if page.status != http.StatusOK {
		t.Fatal(page.status)
	}
	if !strings.Contains(page.body, "Choose a tracker") || strings.Contains(page.body, "count-chart") || strings.Contains(page.body, "trend-canvas") {
		t.Fatal("empty charts page drew a chart")
	}
	if !strings.Contains(page.body, "Millie ate") || !strings.Contains(page.body, "Woods run") || !strings.Contains(page.body, "Club mill") || !strings.Contains(page.body, "Long outing") {
		t.Fatal("primary picker is missing a visible tracker")
	}
	if !strings.Contains(page.body, "M16 5l3 3") {
		t.Fatal("picker is missing the woods icon")
	}
	for _, hidden := range []string{"Old run", "Private run", "Secret outing", "4:44"} {
		if strings.Contains(page.body, hidden) {
			t.Fatalf("picker includes %s", hidden)
		}
	}
	if strings.Contains(page.body, "overlay="+fmt.Sprint(club.ID)) || strings.Contains(page.body, "overlay="+fmt.Sprint(outing.ID)) || strings.Contains(page.body, "overlay="+fmt.Sprint(old.ID)) || strings.Contains(page.body, "overlay="+fmt.Sprint(secret.ID)) || strings.Contains(page.body, "tracker="+fmt.Sprint(private.ID)) {
		t.Fatal("picker includes a tracker the owner cannot use")
	}
	if !strings.Contains(page.body, `href="/charts"`) {
		t.Fatal("logged-in header is missing Charts")
	}

	memberPage := member.get("/charts").body
	if !strings.Contains(memberPage, "Millie ate") || !strings.Contains(memberPage, "Private run") || strings.Contains(memberPage, "Secret outing") {
		t.Fatal("member picker has the wrong trackers")
	}

	chosen := owner.get("/charts?tracker=" + fmt.Sprint(millie.ID))
	if chosen.status != http.StatusOK || !strings.Contains(chosen.body, "Nothing logged in the last 30 days.") || strings.Contains(chosen.body, "count-chart") {
		t.Fatal("empty primary drew a chart")
	}
	if strings.Contains(chosen.body, "overlay="+fmt.Sprint(millie.ID)) {
		t.Fatal("primary tracker is an overlay choice")
	}
	if !strings.Contains(chosen.body, "overlay="+fmt.Sprint(woods.ID)) || !strings.Contains(chosen.body, `aria-current="page">None`) {
		t.Fatal("woods is not an overlay choice, or None is not current")
	}
	if strings.Contains(chosen.body, `data-quick`) || strings.Contains(chosen.body, ">Trend</h2>") {
		t.Fatal("charts page took on the tracker page")
	}

	// An overlay with events still does not draw a chart when the primary has nothing logged.
	owner.post(fmt.Sprintf("/trackers/%d/quick", woods.ID), nil)
	empty := owner.get(fmt.Sprintf("/charts?tracker=%d&overlay=%d", millie.ID, woods.ID))
	if !strings.Contains(empty.body, "Nothing logged in the last 30 days.") || strings.Contains(empty.body, "count-chart") || strings.Contains(empty.body, "count-overlay") || strings.Contains(empty.body, "9:00") {
		t.Fatal("empty primary drew a chart for the overlay")
	}

	owner.post(fmt.Sprintf("/trackers/%d/quick", millie.ID), nil)
	plain := owner.get("/charts?tracker=" + fmt.Sprint(millie.ID))
	series := chartFromPage(t, plain.body)
	if len(series) != chartDays {
		t.Fatalf("default range %d", len(series))
	}
	explicit := chartFromPage(t, owner.get("/charts?tracker="+fmt.Sprint(millie.ID)+"&range=30d").body)
	if len(explicit) != len(series) || explicit[0].Day != series[0].Day || explicit[len(explicit)-1].Day != series[len(series)-1].Day {
		t.Fatal("range=30d did not match the default window")
	}

	self := owner.get(fmt.Sprintf("/charts?tracker=%d&overlay=%d", millie.ID, millie.ID))
	if self.status != http.StatusOK || strings.Contains(self.body, "count-overlay") || !strings.Contains(self.body, `aria-current="page">None`) {
		t.Fatal("a tracker overlaid itself")
	}
	if owner.get("/charts?tracker="+fmt.Sprint(millie.ID)+"&overlay=nope").status != http.StatusOK || strings.Contains(owner.get("/charts?tracker="+fmt.Sprint(millie.ID)+"&overlay=nope").body, "count-overlay") {
		t.Fatal("a non-numeric overlay was not ignored")
	}

	denied := owner.get(fmt.Sprintf("/charts?tracker=%d&overlay=%d", millie.ID, secret.ID))
	if denied.status != http.StatusNotFound || strings.Contains(denied.body, "Secret outing") || strings.Contains(denied.body, "4:44") || strings.Contains(denied.body, "count-overlay") || strings.Contains(denied.body, "count-chart") {
		t.Fatalf("secret overlay: %d", denied.status)
	}
	if owner.get("/charts?overlay="+fmt.Sprint(secret.ID)).status != http.StatusNotFound || strings.Contains(owner.get("/charts?overlay="+fmt.Sprint(secret.ID)).body, "Secret outing") {
		t.Fatal("secret overlay without a primary leaked")
	}
	archived := owner.get("/charts?tracker=" + fmt.Sprint(old.ID))
	if archived.status != http.StatusNotFound || strings.Contains(archived.body, "Old run") {
		t.Fatalf("archived primary: %d", archived.status)
	}
	archivedOverlay := owner.get(fmt.Sprintf("/charts?tracker=%d&overlay=%d", millie.ID, old.ID))
	if archivedOverlay.status != http.StatusNotFound || strings.Contains(archivedOverlay.body, "Old run") {
		t.Fatalf("archived overlay: %d", archivedOverlay.status)
	}
	otherKind := owner.get(fmt.Sprintf("/charts?tracker=%d&overlay=%d", millie.ID, club.ID))
	if otherKind.status != http.StatusNotFound || strings.Contains(otherKind.body, "Club mill") {
		t.Fatalf("number overlay: %d", otherKind.status)
	}
	if owner.get("/charts?tracker=nope").status != http.StatusNotFound {
		t.Fatal("non-numeric primary was not a 404")
	}
	if stranger.get("/charts?tracker="+fmt.Sprint(millie.ID)).status != http.StatusNotFound || strings.Contains(stranger.get("/charts?tracker="+fmt.Sprint(millie.ID)).body, "Millie ate") {
		t.Fatal("stranger saw Millie ate")
	}
	if owner.get("/charts?range=week").status != http.StatusNotFound {
		t.Fatal("unknown range was not a 404")
	}

	// Number and duration use the same chart as the tracker page, over the chosen range.
	if r := owner.post(fmt.Sprintf("/trackers/%d/entries", club.ID), url.Values{"number": {"10"}}); r.status != http.StatusOK {
		t.Fatalf("number log: %d %s", r.status, r.body)
	}
	if r := owner.post(fmt.Sprintf("/trackers/%d/entries", outing.ID), url.Values{"minutes": {"45"}}); r.status != http.StatusOK {
		t.Fatalf("duration log: %d %s", r.status, r.body)
	}
	clubPage := owner.get("/charts?tracker=" + fmt.Sprint(club.ID) + "&range=90d")
	clubChart := valueChartFromPage(t, clubPage.body)
	if clubChart.Kind != kindNumber || clubChart.Unit != "lb" || len(clubChart.Days) != 90 || len(clubChart.Points) != 1 || strings.Contains(clubPage.body, `id="count-chart"`) {
		t.Fatalf("number chart days %d points %d", len(clubChart.Days), len(clubChart.Points))
	}
	outingChart := valueChartFromPage(t, owner.get("/charts?tracker="+fmt.Sprint(outing.ID)+"&range=90d").body)
	if outingChart.Kind != kindDuration || len(outingChart.Days) != 90 || len(outingChart.Points) != 1 {
		t.Fatalf("duration chart %+v", outingChart)
	}
	if len(valueChartFromPage(t, owner.get("/trackers/"+fmt.Sprint(club.ID)).body).Days) != chartDays {
		t.Fatal("tracker page value chart changed length")
	}

	home := owner.get("/")
	if !strings.Contains(home.body, `data-quick`) || strings.Contains(home.body, "trend-canvas") || strings.Contains(home.body, "Show events from") || !strings.Contains(home.body, `href="/charts"`) {
		t.Fatal("home changed")
	}
	if strings.Contains(ts.browser(t).get("/").body, `href="/charts"`) {
		t.Fatal("logged-out home links to charts")
	}
	tracker := owner.get("/trackers/" + fmt.Sprint(millie.ID))
	if !strings.Contains(tracker.body, ">Trend</h2>") || !strings.Contains(tracker.body, `data-quick`) || len(chartFromPage(t, tracker.body)) != chartDays {
		t.Fatal("tracker page trend changed")
	}
	logAtIndex := strings.Index(tracker.body, `data-quick`)
	trendAt := strings.Index(tracker.body, ">Trend</h2>")
	if logAtIndex < 0 || trendAt < logAtIndex {
		t.Fatal("trend moved above the log control")
	}
	before := len(ts.entries(t, millie.ID))
	if r := owner.post(fmt.Sprintf("/trackers/%d/quick", millie.ID), nil); r.status != http.StatusOK || len(ts.entries(t, millie.ID)) != before+1 {
		t.Fatal("logging from the tracker changed")
	}
}

func TestChartsHistory(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	la := location("America/Los_Angeles")
	ts.clock.t = time.Date(2026, 10, 10, 20, 0, 0, 0, time.UTC)
	if localDay(ts.clock.now(), la) != "2026-10-10" {
		t.Fatalf("today %s", localDay(ts.clock.now(), la))
	}
	millie := owner.createTracker(h, "Millie ate", nil)
	woods := owner.createTracker(h, "Woods run", url.Values{"icon": {"🌳"}})
	quiet := owner.createTracker(h, "Quiet walk", nil)

	at := func(day, clock string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", day+" "+clock, la)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, day := range []string{"2025-10-09", "2025-10-10", "2026-04-09", "2026-04-10", "2026-07-12", "2026-07-13"} {
		logAt(t, owner, millie.ID, at(day, "12:00"))
	}
	owner.post(fmt.Sprintf("/trackers/%d/quick", millie.ID), nil)
	if r := owner.post(fmt.Sprintf("/trackers/%d/zero", millie.ID), url.Values{"day": {"2026-10-09"}}); r.status != http.StatusOK {
		t.Fatalf("zero: %d %s", r.status, r.body)
	}

	logAt(t, owner, woods.ID, at("2026-08-01", "09:00"))
	logAt(t, owner, woods.ID, at("2026-08-01", "15:30"))
	logAt(t, owner, woods.ID, at("2026-05-02", "08:00"))
	logAt(t, owner, woods.ID, at("2026-04-09", "08:00"))
	logAt(t, owner, woods.ID, at("2025-10-09", "08:00"))
	owner.post(fmt.Sprintf("/trackers/%d/quick", woods.ID), nil)
	if r := owner.post(fmt.Sprintf("/trackers/%d/zero", woods.ID), url.Values{"day": {"2026-08-02"}}); r.status != http.StatusOK {
		t.Fatalf("woods zero: %d", r.status)
	}

	base := "/charts?tracker=" + fmt.Sprint(millie.ID)
	checkEnds := func(series []chartDay, first, last string) {
		t.Helper()
		assertConsecutiveDays(t, series, la)
		if series[0].Day != first || series[len(series)-1].Day != last {
			t.Fatalf("window %s–%s, want %s–%s", series[0].Day, series[len(series)-1].Day, first, last)
		}
	}
	checkMeals := func(series []chartDay) {
		t.Helper()
		today := seriesDay(t, series, "2026-10-10")
		yesterday := seriesDay(t, series, "2026-10-09")
		gap := seriesDay(t, series, "2026-10-08")
		if today.Count == nil || *today.Count != 1 || yesterday.Count == nil || *yesterday.Count != 0 || gap.Count != nil {
			t.Fatalf("today %+v yesterday %+v gap %+v", today, yesterday, gap)
		}
	}

	d30 := chartFromPage(t, owner.get(base).body)
	if len(d30) != 30 {
		t.Fatalf("30d len %d", len(d30))
	}
	checkEnds(d30, "2026-09-11", "2026-10-10")
	checkMeals(d30)
	if seriesHas(d30, "2026-07-13") {
		t.Fatal("30-day window included July")
	}

	page90 := owner.get(base + "&overlay=" + fmt.Sprint(woods.ID) + "&range=90d")
	if !strings.Contains(page90.body, "Count per day over the last 90 days.") || strings.Contains(page90.body, "correlation") || strings.Contains(page90.body, "cause") {
		t.Fatal("90-day chart description changed")
	}
	d90 := chartFromPage(t, page90.body)
	if len(d90) != 90 {
		t.Fatalf("90d len %d", len(d90))
	}
	checkEnds(d90, "2026-07-13", "2026-10-10")
	checkMeals(d90)
	july := seriesDay(t, d90, "2026-07-13")
	if july.Count == nil || *july.Count != 1 || seriesHas(d90, "2026-07-12") {
		t.Fatalf("90d edge %+v has july 12 %v", july, seriesHas(d90, "2026-07-12"))
	}
	bands90 := overlayFromPage(t, page90.body)
	if bands90.Name != "Woods run" || bands90.Mark != "🌳" || len(bands90.Days) != 2 {
		t.Fatalf("90d overlay %+v", bands90)
	}
	if bands90.Days[0].Day != "2026-08-01" || len(bands90.Days[0].Times) != 2 || bands90.Days[0].Times[0] != "9:00 AM" || bands90.Days[0].Times[1] != "3:30 PM" {
		t.Fatalf("august band %+v", bands90.Days[0])
	}
	if bands90.Days[1].Day != "2026-10-10" || len(bands90.Days[1].Times) != 1 {
		t.Fatalf("today band %+v", bands90.Days[1])
	}
	for _, day := range bands90.Days {
		if day.Day == "2026-05-02" || day.Day == "2026-08-02" || day.Day == "2026-04-09" {
			t.Fatalf("90d overlay included %s", day.Day)
		}
	}

	none := owner.get(base + "&range=90d")
	if strings.Contains(none.body, "count-overlay") {
		t.Fatal("no overlay request embedded overlay data")
	}
	plain90 := chartFromPage(t, none.body)
	if len(plain90) != len(d90) {
		t.Fatal("removing the overlay changed the series")
	}
	for i := range d90 {
		if plain90[i].Day != d90[i].Day || !sameCount(plain90[i].Count, d90[i].Count) {
			t.Fatalf("day %d changed without the overlay", i)
		}
	}
	again := chartFromPage(t, owner.get(base+"&overlay="+fmt.Sprint(woods.ID)+"&range=90d").body)
	if again[0].Day != d90[0].Day || again[len(again)-1].Day != d90[len(again)-1].Day {
		t.Fatal("reloading the url changed the window")
	}

	quietPage := owner.get(base + "&overlay=" + fmt.Sprint(quiet.ID) + "&range=90d")
	if !strings.Contains(quietPage.body, "No Quiet walk events in the last 90 days.") || !strings.Contains(quietPage.body, "count-chart") {
		t.Fatal("empty overlay dropped the primary chart")
	}
	quietBands := overlayFromPage(t, quietPage.body)
	if len(quietBands.Days) != 0 || quietBands.Name != "Quiet walk" {
		t.Fatalf("quiet overlay %+v", quietBands)
	}

	d6 := chartFromPage(t, owner.get(base+"&range=6m").body)
	checkEnds(d6, "2026-04-10", "2026-10-10")
	checkMeals(d6)
	if seriesDay(t, d6, "2026-04-10").Count == nil || *seriesDay(t, d6, "2026-04-10").Count != 1 || seriesHas(d6, "2026-04-09") {
		t.Fatal("6-month edge is wrong")
	}
	if !strings.Contains(owner.get(base+"&range=6m").body, "the last 6 months") {
		t.Fatal("missing 6-month wording")
	}
	bands6 := overlayFromPage(t, owner.get(base+"&overlay="+fmt.Sprint(woods.ID)+"&range=6m").body)
	seen6 := map[string]int{}
	for _, day := range bands6.Days {
		seen6[day.Day] = len(day.Times)
	}
	if seen6["2026-05-02"] != 1 || seen6["2026-08-01"] != 2 || seen6["2026-10-10"] != 1 || seen6["2026-04-09"] != 0 || seen6["2025-10-09"] != 0 {
		t.Fatalf("6m bands %+v", bands6.Days)
	}

	d1 := chartFromPage(t, owner.get(base+"&range=1y").body)
	checkEnds(d1, "2025-10-10", "2026-10-10")
	checkMeals(d1)
	if seriesDay(t, d1, "2025-10-10").Count == nil || *seriesDay(t, d1, "2025-10-10").Count != 1 || seriesHas(d1, "2025-10-09") {
		t.Fatal("1-year edge is wrong")
	}
	bands1 := overlayFromPage(t, owner.get(base+"&overlay="+fmt.Sprint(woods.ID)+"&range=1y").body)
	seen1 := map[string]bool{}
	for _, day := range bands1.Days {
		seen1[day.Day] = true
	}
	if !seen1["2026-04-09"] || !seen1["2026-05-02"] || !seen1["2026-08-01"] || !seen1["2026-10-10"] || seen1["2025-10-09"] {
		t.Fatalf("1y bands %+v", bands1.Days)
	}
	if !strings.Contains(owner.get(base+"&range=1y").body, "the last year") {
		t.Fatal("missing year wording")
	}

	memberSeries := chartFromPage(t, member.get(base+"&range=90d").body)
	if len(memberSeries) != len(d90) || memberSeries[0].Day != d90[0].Day {
		t.Fatal("member did not see the same window")
	}

	blank := owner.createTracker(h, "Empty meals", nil)
	noData := owner.get(fmt.Sprintf("/charts?tracker=%d&overlay=%d&range=90d", blank.ID, woods.ID))
	if !strings.Contains(noData.body, "Nothing logged in the last 90 days.") || strings.Contains(noData.body, "count-chart") || strings.Contains(noData.body, "count-overlay") || strings.Contains(noData.body, "9:00 AM") {
		t.Fatal("empty primary drew the overlay")
	}

	req, err := http.NewRequest(http.MethodGet, ts.srv.URL+base+"&overlay="+fmt.Sprint(woods.ID)+"&range=90d", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("HX-Request", "true")
	partial := owner.do(req)
	if strings.Contains(partial.body, "<!doctype") || !strings.Contains(partial.body, "count-chart") || !strings.Contains(partial.body, "count-overlay") || !strings.Contains(partial.body, "Show events from") || !strings.Contains(partial.body, "range=90d") {
		t.Fatal("partial dropped the chart state")
	}
}

func TestChartsFollowStoredZone(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.post("/signup", url.Values{"email": {"utc@example.com"}, "timezone": {"UTC"}})
	owner.confirmEmail("utc@example.com")
	h := ts.personalHousehold(t, "utc@example.com")
	owner.post(fmt.Sprintf("/households/%d/invite", h.ID), nil)
	h = ts.household(t, h.ID)
	member := ts.browser(t)
	member.signup("la@example.com", "")
	member.confirmEmail("la@example.com")
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
	path := fmt.Sprintf("/charts?tracker=%d&overlay=%d&range=90d", millie.ID, woods.ID)
	utcSeries := chartFromPage(t, owner.get(path).body)
	laSeries := chartFromPage(t, member.get(path).body)
	if len(utcSeries) != 90 || len(laSeries) != 90 {
		t.Fatalf("utc %d la %d", len(utcSeries), len(laSeries))
	}
	assertConsecutiveDays(t, utcSeries, time.UTC)
	assertConsecutiveDays(t, laSeries, location("America/Los_Angeles"))
	if utcSeries[len(utcSeries)-1].Day != utcToday || utcSeries[len(utcSeries)-1].Count == nil || *utcSeries[len(utcSeries)-1].Count != 1 {
		t.Fatalf("utc end %+v", utcSeries[len(utcSeries)-1])
	}
	if laSeries[len(laSeries)-1].Day != laToday || laSeries[len(laSeries)-1].Count == nil || *laSeries[len(laSeries)-1].Count != 1 {
		t.Fatalf("la end %+v", laSeries[len(laSeries)-1])
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
	if utcSeries[0].Day != utcStart.AddDate(0, 0, -89).Format(dayLayout) {
		t.Fatalf("utc window starts %s", utcSeries[0].Day)
	}
	utcOverlay := overlayFromPage(t, owner.get(path).body)
	laOverlay := overlayFromPage(t, member.get(path).body)
	if len(utcOverlay.Days) != 1 || utcOverlay.Days[0].Day != utcToday {
		t.Fatalf("utc overlay %+v", utcOverlay.Days)
	}
	if len(laOverlay.Days) != 1 || laOverlay.Days[0].Day != laToday {
		t.Fatalf("la overlay %+v", laOverlay.Days)
	}
	for _, day := range laOverlay.Days {
		if day.Day == utcToday {
			t.Fatal("Los Angeles overlay used the UTC date")
		}
	}
}

func TestChartsDSTPage(t *testing.T) {
	ts, owner, _, h := sharedHouse(t)
	la := location("America/Los_Angeles")
	ts.clock.t = time.Date(2026, 3, 20, 20, 0, 0, 0, time.UTC)
	if localDay(ts.clock.now(), la) != "2026-03-20" {
		t.Fatalf("today %s", localDay(ts.clock.now(), la))
	}
	millie := owner.createTracker(h, "Millie ate", nil)
	woods := owner.createTracker(h, "Woods run", nil)
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, la)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	logAt(t, owner, millie.ID, at("2026-03-07 23:30"))
	logAt(t, owner, millie.ID, at("2026-03-08 00:30"))
	logAt(t, owner, millie.ID, at("2026-03-08 03:30"))
	logAt(t, owner, woods.ID, at("2026-03-08 00:30"))
	logAt(t, owner, woods.ID, at("2026-03-08 03:30"))

	page := owner.get(fmt.Sprintf("/charts?tracker=%d&overlay=%d&range=90d", millie.ID, woods.ID))
	series := chartFromPage(t, page.body)
	assertConsecutiveDays(t, series, la)
	if len(series) != 90 || series[0].Day != "2025-12-21" || series[len(series)-1].Day != "2026-03-20" {
		t.Fatalf("window %s–%s len %d", series[0].Day, series[len(series)-1].Day, len(series))
	}
	march7 := seriesDay(t, series, "2026-03-07")
	march8 := seriesDay(t, series, "2026-03-08")
	if march7.Count == nil || *march7.Count != 1 || march8.Count == nil || *march8.Count != 2 {
		t.Fatalf("march 7 %+v march 8 %+v", march7, march8)
	}
	bands := overlayFromPage(t, page.body)
	if len(bands.Days) != 1 || bands.Days[0].Day != "2026-03-08" || len(bands.Days[0].Times) != 2 {
		t.Fatalf("bands %+v", bands.Days)
	}
	if bands.Days[0].Times[0] != "12:30 AM" || bands.Days[0].Times[1] != "3:30 AM" {
		t.Fatalf("times %+v", bands.Days[0].Times)
	}
}
