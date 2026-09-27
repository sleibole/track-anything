package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func (ts *testServer) entries(t *testing.T, trackerID uint) []Entry {
	t.Helper()
	var es []Entry
	if err := ts.app.db.Where("tracker_id = ?", trackerID).Order("id").Find(&es).Error; err != nil {
		t.Fatal(err)
	}
	return es
}

func (ts *testServer) tracker(t *testing.T, id uint) Tracker {
	t.Helper()
	var tr Tracker
	if err := ts.app.db.Take(&tr, id).Error; err != nil {
		t.Fatal(err)
	}
	return tr
}

func (ts *testServer) zeros(t *testing.T, trackerID uint) []RecordedZero {
	t.Helper()
	var zs []RecordedZero
	if err := ts.app.db.Where("tracker_id = ?", trackerID).Order("day").Find(&zs).Error; err != nil {
		t.Fatal(err)
	}
	return zs
}

// Trackers

func TestCreateTrackerDefaults(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("dog@example.com", "")
	h := ts.personalHousehold(t, "dog@example.com")

	if r := b.get("/trackers/new"); !strings.Contains(r.body, `name="icon" value="" aria-label="Tally marks" checked`) {
		t.Fatal("tally mark isn't the default icon")
	}
	tr := b.createTracker(h, "Dog ate", nil)
	if tr.Icon != "" || tr.Accent != "" || tr.LogLabel != "" || tr.Kind != "count" {
		t.Fatalf("stored %+v", tr)
	}
	home := b.get("/").body
	tally, _ := icon("tallymarks")
	for _, want := range []string{"Dog ate", ">&#43; Log</button>", "Nothing logged today", string(tally)} {
		if !strings.Contains(home, want) {
			t.Errorf("card missing %q", want)
		}
	}
	if strings.Contains(home, "accent-") {
		t.Error("empty accent added an accent class")
	}
}

func TestCreateTrackerWithIconAccentAndLabel(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("dog@example.com", "")
	h := ts.personalHousehold(t, "dog@example.com")

	b.createTracker(h, "Dog ate", url.Values{"icon": {"paw"}, "accent": {"green"}, "log_label": {"+ Ate"}})
	b.createTracker(h, "Woods", url.Values{"icon": {"🌳"}})
	home := b.get("/").body
	paw, _ := icon("paw")
	for _, want := range []string{"accent-green", ">&#43; Ate</button>", string(paw), "Dog ate", `<span class="emoji" aria-hidden="true">🌳</span>`, "Woods"} {
		if !strings.Contains(home, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestCreateTrackerValidation(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("dog@example.com", "")
	h := fmt.Sprint(ts.personalHousehold(t, "dog@example.com").ID)

	for name, form := range map[string]url.Values{
		"no name":      {"name": {"  "}},
		"long label":   {"name": {"Dog"}, "log_label": {strings.Repeat("x", 25)}},
		"unknown icon": {"name": {"Dog"}, "icon": {"rocket"}},
		"any emoji":    {"name": {"Dog"}, "icon": {"🚀"}},
		"bad accent":   {"name": {"Dog"}, "accent": {"neon"}},
	} {
		form.Set("household", h)
		if r := b.post("/trackers", form); r.status != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d", name, r.status)
		}
	}
	var n int64
	ts.app.db.Model(&Tracker{}).Count(&n)
	if n != 0 {
		t.Fatalf("%d trackers created from invalid forms", n)
	}
	if r := b.post("/trackers", url.Values{"household": {h}, "name": {"Dog"}, "log_label": {strings.Repeat("x", 24)}}); r.status != http.StatusOK {
		t.Fatalf("24-character label rejected: %d", r.status)
	}
}

func TestNewTrackerFormSelectsRequestedHousehold(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	personal := ts.personalHousehold(t, "member@example.com")
	owner.post(fmt.Sprintf("/households/%d/members/%d/owner", h.ID, ts.user(t, "member@example.com").ID), nil)

	page := member.get(fmt.Sprintf("/trackers/new?household=%d", h.ID)).body
	if !strings.Contains(page, fmt.Sprintf(`value="%d" selected`, h.ID)) {
		t.Fatal("requested household isn't selected")
	}
	if strings.Contains(page, fmt.Sprintf(`value="%d" selected`, personal.ID)) {
		t.Fatal("personal household stayed selected")
	}

	// A household this person doesn't own is ignored.
	ignored := member.get(fmt.Sprintf("/trackers/new?household=%d", h.ID+1000)).body
	if strings.Contains(ignored, `selected`) && strings.Contains(ignored, fmt.Sprintf(`value="%d" selected`, h.ID+1000)) {
		t.Fatal("unowned household was selected")
	}
}

func TestCantCreateTrackerInAHouseholdYouDontOwn(t *testing.T) {
	ts, _, member, h := sharedHouse(t)
	if r := member.post("/trackers", url.Values{"household": {fmt.Sprint(h.ID)}, "name": {"Sneaky"}}); r.status != http.StatusForbidden {
		t.Fatalf("member: %d", r.status)
	}
	stranger := ts.browser(t)
	stranger.signup("stranger@example.com", "")
	if r := stranger.post("/trackers", url.Values{"household": {fmt.Sprint(h.ID)}, "name": {"Sneaky"}}); r.status != http.StatusNotFound {
		t.Fatalf("stranger: %d", r.status)
	}
	var n int64
	ts.app.db.Model(&Tracker{}).Count(&n)
	if n != 0 {
		t.Fatal("tracker created")
	}
}

func TestOwnerEditsTracker(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Dog", nil)
	path := fmt.Sprintf("/trackers/%d", tr.ID)

	owner.post(path, url.Values{"name": {"Dog ate"}, "icon": {"bone"}, "accent": {"amber"}, "log_label": {"+ Ate"}})
	got := ts.tracker(t, tr.ID)
	if got.Name != "Dog ate" || got.Icon != "bone" || got.Accent != "amber" || got.LogLabel != "+ Ate" {
		t.Fatalf("after edit %+v", got)
	}
	if r := member.get(path).body; !strings.Contains(r, ">&#43; Ate</button>") || !strings.Contains(r, "Dog ate") {
		t.Error("member doesn't see the edit")
	}
}

func TestOwnerOnlyTrackerRoutesRefuseMembers(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Dog ate", nil)
	base := fmt.Sprintf("/trackers/%d", tr.ID)

	if r := member.get(base + "/edit"); r.status != http.StatusForbidden {
		t.Errorf("GET edit: %d", r.status)
	}
	for path, form := range map[string]url.Values{
		base:              {"name": {"Renamed"}, "summary_display": {summaryDone}},
		base + "/archive": nil,
		base + "/share":   nil,
	} {
		if r := member.post(path, form); r.status != http.StatusForbidden {
			t.Errorf("POST %s: %d", path, r.status)
		}
	}
	got := ts.tracker(t, tr.ID)
	if got.Name != "Dog ate" || got.ArchivedAt != nil || got.ShareToken != nil || got.SummaryDisplay == summaryDone {
		t.Fatalf("member changed the tracker: %+v", got)
	}
	owner.post(base+"/share", nil)
	if r := member.post(base+"/share/delete", nil); r.status != http.StatusForbidden || ts.tracker(t, tr.ID).ShareToken == nil {
		t.Errorf("member turned the share link off: %d", r.status)
	}
	if strings.Contains(member.get(base).body, base+"/edit") {
		t.Error("member sees the edit link")
	}
}

func TestNonMembersGet404ForTrackers(t *testing.T) {
	ts, owner, _, h := sharedHouse(t)
	tr := owner.createTracker(h, "Dog ate", nil)
	owner.post(fmt.Sprintf("/trackers/%d/quick", tr.ID), nil)
	e := ts.entries(t, tr.ID)[0]

	stranger := ts.browser(t)
	stranger.signup("stranger@example.com", "")
	base := fmt.Sprintf("/trackers/%d", tr.ID)
	if r := stranger.get(base); r.status != http.StatusNotFound {
		t.Errorf("GET tracker: %d", r.status)
	}
	for _, path := range []string{base + "/quick", base + "/entries", base + "/zero", base + "/archive", fmt.Sprintf("/entries/%d/undo", e.ID), fmt.Sprintf("/entries/%d/delete", e.ID)} {
		if r := stranger.post(path, nil); r.status != http.StatusNotFound {
			t.Errorf("POST %s: %d", path, r.status)
		}
	}
	if n := len(ts.entries(t, tr.ID)); n != 1 {
		t.Fatalf("%d entries, want 1", n)
	}
}

func TestArchiveAndRestore(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Dog ate", nil)
	base := fmt.Sprintf("/trackers/%d", tr.ID)
	owner.post(base+"/quick", nil)
	owner.post(base+"/share", nil)
	oldLink := "/s/" + *ts.tracker(t, tr.ID).ShareToken

	r := owner.post(base+"/archive", nil)
	got := ts.tracker(t, tr.ID)
	if got.ArchivedAt == nil || got.ShareToken != nil {
		t.Fatalf("after archive %+v", got)
	}
	if !strings.Contains(r.body, "Archived trackers (1)") {
		t.Error("owner's household page doesn't list the archived tracker")
	}
	if strings.Contains(member.get(fmt.Sprintf("/households/%d", h.ID)).body, "Archived trackers") {
		t.Error("member sees archived trackers")
	}
	for name, b := range map[string]*browser{"owner": owner, "member": member} {
		if strings.Contains(b.get("/").body, "Dog ate") {
			t.Errorf("%s dashboard still shows the archived tracker", name)
		}
		if r := b.get(base); r.status != http.StatusNotFound {
			t.Errorf("%s tracker page: %d", name, r.status)
		}
		if r := b.post(base+"/quick", nil); r.status != http.StatusNotFound {
			t.Errorf("%s quick log: %d", name, r.status)
		}
	}
	if r := ts.browser(t).get(oldLink); r.status != http.StatusNotFound {
		t.Errorf("share link on an archived tracker: %d", r.status)
	}

	if r := member.post(base+"/restore", nil); r.status != http.StatusNotFound || ts.tracker(t, tr.ID).ArchivedAt == nil {
		t.Fatalf("member restore: %d", r.status)
	}
	owner.post(base+"/restore", nil)
	got = ts.tracker(t, tr.ID)
	if got.ArchivedAt != nil || got.ShareToken != nil {
		t.Fatalf("after restore %+v", got)
	}
	if !strings.Contains(owner.get("/").body, "1 time today") {
		t.Error("restored tracker lost its entries")
	}
	if r := ts.browser(t).get(oldLink); r.status != http.StatusNotFound {
		t.Errorf("old share link after restore: %d", r.status)
	}
}

func TestNoPermanentTrackerDeleteInPhase2(t *testing.T) {
	ts, owner, _, h := sharedHouse(t)
	tr := owner.createTracker(h, "Dog ate", nil)
	if r := owner.post(fmt.Sprintf("/trackers/%d/delete", tr.ID), nil); r.status != http.StatusNotFound {
		t.Fatalf("delete route answered %d", r.status)
	}
	ts.tracker(t, tr.ID)
}

// Logging

func TestQuickLogFromTheDashboard(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Dog ate", url.Values{"log_label": {"+ Ate"}})
	quick := fmt.Sprintf("/trackers/%d/quick", tr.ID)

	r := owner.post(quick, url.Values{"back": {"/"}})
	if r.url.Path != "/" || !strings.Contains(r.body, "1 time today") {
		t.Fatalf("after log: %s", r.url.Path)
	}
	if !strings.Contains(r.body, fmt.Sprintf(`action="/entries/%d/undo"`, ts.entries(t, tr.ID)[0].ID)) {
		t.Error("no Undo after logging")
	}
	r = member.post(quick, url.Values{"back": {"/"}})
	if !strings.Contains(r.body, "2 times today") {
		t.Fatal("member's log not counted")
	}
	es := ts.entries(t, tr.ID)
	ownerID, memberID := ts.user(t, "owner@example.com").ID, ts.user(t, "member@example.com").ID
	if *es[0].RecordedByID != ownerID || *es[1].RecordedByID != memberID || es[0].ViaLink || es[1].ViaLink {
		t.Fatalf("recorded by %v, %v", *es[0].RecordedByID, *es[1].RecordedByID)
	}
	if !strings.Contains(owner.get("/").body, "2 times today") {
		t.Error("owner doesn't see the member's log")
	}
	if r := owner.post(quick, url.Values{"back": {"https://evil.example"}}); r.url.Host != strings.TrimPrefix(ts.srv.URL, "http://") {
		t.Errorf("back redirected off-site to %s", r.url)
	}
}

func TestMembersLogNowWithANoteButCantBackfill(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Dog ate", nil)
	path := fmt.Sprintf("/trackers/%d", tr.ID)

	if page := member.get(path).body; strings.Contains(page, `type="datetime-local"`) || !strings.Contains(page, `name="note"`) {
		t.Error("member's log form should have a note and no time field")
	}
	if page := owner.get(path).body; !strings.Contains(page, `type="datetime-local" name="time"`) {
		t.Error("owner's log form has no time field")
	}

	yesterday := ts.clock.now().Add(-24 * time.Hour).In(location("America/Los_Angeles")).Format(timeInputLayout)
	member.post(path+"/entries", url.Values{"time": {yesterday}, "note": {"half a bowl"}})
	es := ts.entries(t, tr.ID)
	if len(es) != 1 || es[0].Note != "half a bowl" {
		t.Fatalf("entries %+v", es)
	}
	if d := ts.clock.now().Sub(es[0].OccurredAt); d < 0 || d > time.Minute {
		t.Fatalf("member's entry happened %v ago, want now", d)
	}
}

func TestOwnerBackfills(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Dog ate", nil)
	path := fmt.Sprintf("/trackers/%d", tr.ID)
	la := location("America/Los_Angeles")
	when := ts.clock.now().In(la).AddDate(0, 0, -1).Truncate(time.Minute)

	r := owner.post(path+"/entries", url.Values{"time": {when.Format(timeInputLayout)}, "note": {"forgot"}})
	es := ts.entries(t, tr.ID)
	if len(es) != 1 || !es[0].OccurredAt.Equal(when) || es[0].Note != "forgot" {
		t.Fatalf("entries %+v, want one at %v", es, when)
	}
	if !strings.Contains(r.body, "Nothing logged today") {
		t.Error("backfilled entry counted as today")
	}
	if !strings.Contains(r.body, when.Format("3:04 PM")) {
		t.Error("history doesn't show the backfilled entry")
	}
	if !strings.Contains(member.get(path).body, "by owner@example.com") {
		t.Error("member doesn't see who logged it")
	}

	future := ts.clock.now().In(la).Add(time.Hour).Format(timeInputLayout)
	for name, v := range map[string]string{"future": future, "garbage": "yesterday-ish"} {
		if r := owner.post(path+"/entries", url.Values{"time": {v}}); r.status != http.StatusUnprocessableEntity {
			t.Errorf("%s time: %d", name, r.status)
		}
	}
	if r := owner.post(path+"/entries", url.Values{"note": {strings.Repeat("x", 501)}}); r.status != http.StatusUnprocessableEntity {
		t.Errorf("long note: %d", r.status)
	}
	if n := len(ts.entries(t, tr.ID)); n != 1 {
		t.Fatalf("%d entries after invalid posts", n)
	}
}

func TestUndoWithinFifteenMinutes(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Dog ate", nil)
	quick := fmt.Sprintf("/trackers/%d/quick", tr.ID)

	owner.post(quick, nil)
	ts.clock.advance(14 * time.Minute)
	e := ts.entries(t, tr.ID)[0]
	// Anyone who can log can undo a recent entry, not just the person who logged it.
	member.post(fmt.Sprintf("/entries/%d/undo", e.ID), nil)
	if n := len(ts.entries(t, tr.ID)); n != 0 {
		t.Fatal("undo within the window failed")
	}

	owner.post(quick, nil)
	e = ts.entries(t, tr.ID)[0]
	ts.clock.advance(16 * time.Minute)
	for name, b := range map[string]*browser{"member": member, "owner": owner} {
		if r := b.post(fmt.Sprintf("/entries/%d/undo", e.ID), nil); r.status != http.StatusForbidden {
			t.Errorf("%s undo after the window: %d", name, r.status)
		}
	}
	if n := len(ts.entries(t, tr.ID)); n != 1 {
		t.Fatal("entry undone after the window")
	}
	if strings.Contains(owner.get("/").body, "Undo") {
		t.Error("Undo still offered after the window")
	}
}

func TestOwnerEditsAndDeletesEntries(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Dog ate", nil)
	owner.post(fmt.Sprintf("/trackers/%d/quick", tr.ID), nil)
	e := ts.entries(t, tr.ID)[0]
	la := location("America/Los_Angeles")
	earlier := ts.clock.now().In(la).Add(-3 * time.Hour).Truncate(time.Minute)
	edit := url.Values{"time": {earlier.Format(timeInputLayout)}, "note": {"breakfast"}}

	if r := member.post(fmt.Sprintf("/entries/%d", e.ID), edit); r.status != http.StatusForbidden {
		t.Errorf("member edit: %d", r.status)
	}
	if r := member.post(fmt.Sprintf("/entries/%d/delete", e.ID), nil); r.status != http.StatusForbidden {
		t.Errorf("member delete: %d", r.status)
	}
	if got := ts.entries(t, tr.ID); len(got) != 1 || got[0].Note != "" || !got[0].OccurredAt.Equal(e.OccurredAt) {
		t.Fatalf("member changed the entry: %+v", got)
	}

	owner.post(fmt.Sprintf("/entries/%d", e.ID), edit)
	got := ts.entries(t, tr.ID)[0]
	if !got.OccurredAt.Equal(earlier) || got.Note != "breakfast" {
		t.Fatalf("after owner edit %+v", got)
	}

	ts.clock.advance(time.Hour) // long past undo
	owner.post(fmt.Sprintf("/entries/%d/delete", e.ID), nil)
	if n := len(ts.entries(t, tr.ID)); n != 0 {
		t.Fatal("owner delete failed")
	}
}

// Recorded zeros

func TestRecordedZero(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Dog ate", nil)
	base := fmt.Sprintf("/trackers/%d", tr.ID)

	r := member.post(base+"/zero", nil)
	if !strings.Contains(r.body, "None today") || len(ts.entries(t, tr.ID)) != 0 {
		t.Fatal("recording none didn't show None today, or created an entry")
	}
	if !strings.Contains(owner.get("/").body, "None today") {
		t.Error("dashboard doesn't show None today")
	}

	r = member.post(base+"/quick", nil)
	if !strings.Contains(r.body, "1 time today") || len(ts.zeros(t, tr.ID)) != 0 {
		t.Fatal("logging didn't clear the recorded zero")
	}
	if r := member.post(base+"/zero", nil); r.status != http.StatusUnprocessableEntity || len(ts.zeros(t, tr.ID)) != 0 {
		t.Fatalf("zero on a day with entries: %d", r.status)
	}
}

func TestRecordedZeroOnEarlierDaysIsOwnerOnly(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Dog ate", nil)
	base := fmt.Sprintf("/trackers/%d", tr.ID)
	la := location("America/Los_Angeles")
	yesterday := ts.clock.now().In(la).AddDate(0, 0, -1).Format(dayLayout)
	tomorrow := ts.clock.now().In(la).AddDate(0, 0, 1).Format(dayLayout)

	if r := member.post(base+"/zero", url.Values{"day": {yesterday}}); r.status != http.StatusForbidden || len(ts.zeros(t, tr.ID)) != 0 {
		t.Fatalf("member zero for yesterday: %d", r.status)
	}
	if r := owner.post(base+"/zero", url.Values{"day": {tomorrow}}); r.status != http.StatusUnprocessableEntity {
		t.Errorf("zero for tomorrow: %d", r.status)
	}
	r := owner.post(base+"/zero", url.Values{"day": {yesterday}})
	zs := ts.zeros(t, tr.ID)
	if len(zs) != 1 || zs[0].Day != yesterday {
		t.Fatalf("zeros %+v", zs)
	}
	// History: yesterday is None, the day before is Nothing logged. Today is still unlogged.
	if !strings.Contains(r.body, `<span class="day-summary">None</span>`) || !strings.Contains(r.body, `<span class="day-summary">Nothing logged</span>`) {
		t.Error("history doesn't tell none from nothing logged")
	}
	if !strings.Contains(r.body, "Nothing logged today") {
		t.Error("yesterday's zero changed today's summary")
	}

	z := zs[0]
	if r := member.post(fmt.Sprintf("/zeros/%d/delete", z.ID), nil); r.status != http.StatusForbidden || len(ts.zeros(t, tr.ID)) != 1 {
		t.Fatalf("member cleared an older zero: %d", r.status)
	}
	owner.post(fmt.Sprintf("/zeros/%d/delete", z.ID), nil)
	if len(ts.zeros(t, tr.ID)) != 0 {
		t.Fatal("owner couldn't clear the zero")
	}
}

func TestUndoRecordedZero(t *testing.T) {
	ts, owner, member, h := sharedHouse(t)
	tr := owner.createTracker(h, "Dog ate", nil)
	base := fmt.Sprintf("/trackers/%d", tr.ID)

	member.post(base+"/zero", nil)
	z := ts.zeros(t, tr.ID)[0]
	if z.RecordedByID == nil || *z.RecordedByID != ts.user(t, "member@example.com").ID {
		t.Fatal("zero doesn't record who marked it")
	}
	member.post(fmt.Sprintf("/zeros/%d/undo", z.ID), nil)
	if len(ts.zeros(t, tr.ID)) != 0 {
		t.Fatal("undo within the window failed")
	}

	member.post(base+"/zero", nil)
	z = ts.zeros(t, tr.ID)[0]
	ts.clock.advance(16 * time.Minute)
	if r := member.post(fmt.Sprintf("/zeros/%d/undo", z.ID), nil); r.status != http.StatusForbidden || len(ts.zeros(t, tr.ID)) != 1 {
		t.Fatalf("undo after the window: %d", r.status)
	}
}

// Time zones

func TestTodayFollowsEachViewersStoredZone(t *testing.T) {
	ts := newTestServer(t)
	// 06:30 UTC is late evening the day before in Los Angeles, in both PST and PDT.
	day := time.Now().UTC().Truncate(24 * time.Hour)
	ts.clock.t = day.Add(6*time.Hour + 30*time.Minute)

	owner := ts.browser(t)
	owner.post("/signup", url.Values{"email": {"utc@example.com"}, "timezone": {"UTC"}})
	h := ts.personalHousehold(t, "utc@example.com")
	owner.post(fmt.Sprintf("/households/%d/invite", h.ID), nil)
	h = ts.household(t, h.ID)
	member := ts.browser(t)
	member.signup("la@example.com", "") // America/Los_Angeles
	member.post("/join/"+*h.InviteToken, nil)

	tr := owner.createTracker(h, "Dog ate", nil)
	member.post(fmt.Sprintf("/trackers/%d/quick", tr.ID), nil)

	// 09:00 UTC is after midnight in Los Angeles: a new day there, still the same day in UTC.
	ts.clock.t = day.Add(9 * time.Hour)
	if !strings.Contains(owner.get("/").body, "1 time today") {
		t.Error("UTC viewer should count the entry as today")
	}
	if !strings.Contains(member.get("/").body, "Nothing logged today") {
		t.Error("Los Angeles viewer should see a new day")
	}
}

func summaryText(t *testing.T, body string) string {
	t.Helper()
	const mark = `class="tracker-summary"`
	i := strings.Index(body, mark)
	if i < 0 {
		t.Fatalf("no summary in %s", body)
	}
	rest := body[i:]
	start := strings.Index(rest, ">")
	end := strings.Index(rest, "</p>")
	if start < 0 || end < start {
		t.Fatalf("summary tag in %s", rest)
	}
	return rest[start+1 : end]
}

func TestSummaryDisplayDefaultsToTimes(t *testing.T) {
	_, owner, _, h := sharedHouse(t)
	page := owner.get("/trackers/new?household=" + fmt.Sprint(h.ID)).body
	for _, want := range []string{
		"Times today — show how many times it happened today",
		"Done today — show whether it happened today",
		"Last occurrence — show when it last happened",
		`value="times" checked`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("new form missing %q", want)
		}
	}

	tr := owner.createTracker(h, "Millie ate", nil)
	if tr.SummaryDisplay != summaryTimes && tr.SummaryDisplay != "" {
		t.Fatalf("stored %q", tr.SummaryDisplay)
	}
	if summaryText(t, owner.get("/").body) != "Nothing logged today" {
		t.Fatal("omitted summary display did not render Times today")
	}
}

func TestSummaryDisplayIsIndependentOfTheLogLabel(t *testing.T) {
	ts, owner, _, h := sharedHouse(t)
	tr := owner.createTracker(h, "Logan Motrin", url.Values{
		"summary_display": {summaryDone},
		"log_label":       {"+ Ate"},
	})
	if tr.SummaryDisplay != summaryDone || tr.LogLabel != "+ Ate" {
		t.Fatalf("created %+v", tr)
	}
	path := fmt.Sprintf("/trackers/%d", tr.ID)
	owner.post(path, url.Values{
		"name": {"Logan Motrin"}, "log_label": {"Gave Motrin"}, "summary_display": {summaryDone},
	})
	got := ts.tracker(t, tr.ID)
	if got.LogLabel != "Gave Motrin" || got.SummaryDisplay != summaryDone {
		t.Fatalf("label change cleared the summary: %+v", got)
	}
	owner.post(path, url.Values{
		"name": {"Logan Motrin"}, "icon": {"💊"}, "accent": {"green"},
		"log_label": {"Gave Motrin"}, "summary_display": {summaryLast},
	})
	got = ts.tracker(t, tr.ID)
	if got.LogLabel != "Gave Motrin" || got.SummaryDisplay != summaryLast || got.Icon != "💊" || got.Accent != "green" {
		t.Fatalf("summary change cleared the label: %+v", got)
	}
	edit := owner.get(path + "/edit").body
	if !strings.Contains(edit, `value="last" checked`) || !strings.Contains(edit, `value="Gave Motrin"`) {
		t.Fatal("edit form didn't keep Last occurrence and the label")
	}
}

func TestSummaryDisplayRejectsUnknownValues(t *testing.T) {
	ts, owner, _, h := sharedHouse(t)
	before := owner.createTracker(h, "Dog ate", nil)
	path := fmt.Sprintf("/trackers/%d", before.ID)
	for _, bad := range []string{"habit", "boolean"} {
		r := owner.post(path, url.Values{"name": {"Dog ate"}, "summary_display": {bad}})
		if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, "Pick a summary from the list.") {
			t.Fatalf("%s: %d", bad, r.status)
		}
		if got := ts.tracker(t, before.ID); got.SummaryDisplay != before.SummaryDisplay || got.Name != "Dog ate" {
			t.Fatalf("%s changed the tracker to %+v", bad, got)
		}
	}
	n := 0
	ts.app.db.Model(&Tracker{}).Select("count(*)").Scan(&n)
	if r := owner.post("/trackers", url.Values{"household": {fmt.Sprint(h.ID)}, "name": {"Nope"}, "summary_display": {"habit"}}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("create: %d", r.status)
	}
	var after int
	ts.app.db.Model(&Tracker{}).Select("count(*)").Scan(&after)
	if after != n {
		t.Fatalf("invalid create stored a tracker: %d to %d", n, after)
	}
}

func TestTimesTodaySummaryOnHomeTrackerAndShare(t *testing.T) {
	ts, owner, _, h := sharedHouse(t)
	tr := owner.createTracker(h, "Millie ate", nil)
	base := fmt.Sprintf("/trackers/%d", tr.ID)
	la := location("America/Los_Angeles")
	yesterday := ts.clock.now().In(la).AddDate(0, 0, -1).Format(dayLayout)
	owner.post(base+"/zero", url.Values{"day": {yesterday}})
	owner.post(base+"/quick", nil)
	owner.post(base+"/quick", nil)

	owner.post(base+"/share", nil)
	tr = ts.tracker(t, tr.ID)
	link := "/s/" + *tr.ShareToken
	sitter := ts.browser(t)

	for name, body := range map[string]string{
		"home":    owner.get("/").body,
		"tracker": owner.get(base).body,
		"share":   sitter.get(link).body,
	} {
		if summaryText(t, body) != "2 times today" {
			t.Errorf("%s summary = %q", name, summaryText(t, body))
		}
	}
	page := owner.get(base).body
	if !strings.Contains(page, `<span class="day-summary">None</span>`) || strings.Contains(page, "Done today") {
		t.Error("history followed the summary display")
	}
}

func TestDoneTodaySummary(t *testing.T) {
	ts, owner, _, h := sharedHouse(t)
	tr := owner.createTracker(h, "McGill Big 3", url.Values{"summary_display": {summaryDone}})
	base := fmt.Sprintf("/trackers/%d", tr.ID)
	la := location("America/Los_Angeles")
	yesterday := ts.clock.now().In(la).AddDate(0, 0, -1).Truncate(time.Minute)

	owner.post(base+"/entries", url.Values{"time": {yesterday.Format(timeInputLayout)}})
	if summaryText(t, owner.get(base).body) != "Not done today" {
		t.Fatal("an earlier entry counted as today")
	}

	owner.post(base+"/quick", nil)
	if summaryText(t, owner.get(base).body) != "Done today" || len(ts.entries(t, tr.ID)) != 2 {
		t.Fatal("first log today")
	}
	owner.post(base+"/quick", nil)
	if summaryText(t, owner.get(base).body) != "Done today" || len(ts.entries(t, tr.ID)) != 3 {
		t.Fatal("second log today changed the summary or dropped the entry")
	}
	for _, e := range ts.entries(t, tr.ID) {
		if localDay(e.OccurredAt, la) == localDay(ts.clock.now(), la) {
			owner.post(fmt.Sprintf("/entries/%d/undo", e.ID), nil)
		}
	}
	if summaryText(t, owner.get(base).body) != "Not done today" {
		t.Fatal("undoing today's entries left the summary done")
	}

	for _, e := range ts.entries(t, tr.ID) {
		owner.post(fmt.Sprintf("/entries/%d/delete", e.ID), nil)
	}
	owner.post(base+"/zero", url.Values{"day": {yesterday.Format(dayLayout)}})
	page := owner.get(base).body
	if summaryText(t, page) != "Not done today" || !strings.Contains(page, `<span class="day-summary">None</span>`) {
		t.Fatal("a recorded zero on an earlier day changed Done today, or history hid it")
	}
	owner.post(base+"/zero", nil)
	page = owner.get(base).body
	if summaryText(t, page) != "Not done today" || !strings.Contains(page, "Recorded as none.") {
		t.Fatalf("today marked none: %s", summaryText(t, page))
	}
}

func TestLastOccurrenceSummary(t *testing.T) {
	ts, owner, _, h := sharedHouse(t)
	tr := owner.createTracker(h, "Logan Motrin", url.Values{
		"summary_display": {summaryLast},
		"log_label":       {"Gave Motrin"},
		"icon":            {"💊"},
		"accent":          {"green"},
	})
	base := fmt.Sprintf("/trackers/%d", tr.ID)
	la := location("America/Los_Angeles")

	page := owner.get(base).body
	if summaryText(t, page) != "Never logged" || !strings.Contains(page, "Nothing logged yet") {
		t.Fatalf("empty tracker: summary %q", summaryText(t, page))
	}

	now := ts.clock.now().In(la)
	at := time.Date(now.Year(), now.Month(), now.Day(), 6, 42, 0, 0, la)
	ts.clock.t = at
	owner.post(base+"/quick", nil)
	if summaryText(t, owner.get(base).body) != "Last: 6:42 AM" {
		t.Fatalf("today: %s", summaryText(t, owner.get(base).body))
	}

	earlier := at.AddDate(0, 0, -3).Truncate(time.Minute)
	owner.post(base+"/entries", url.Values{"time": {earlier.Format(timeInputLayout)}})
	if summaryText(t, owner.get(base).body) != "Last: 6:42 AM" {
		t.Fatal("an older backfill replaced today's occurrence")
	}

	var todayEntry Entry
	for _, e := range ts.entries(t, tr.ID) {
		if localDay(e.OccurredAt, la) == localDay(at, la) {
			todayEntry = e
		}
	}
	yesterday := at.AddDate(0, 0, -1).Truncate(time.Minute)
	owner.post(fmt.Sprintf("/entries/%d", todayEntry.ID), url.Values{"time": {yesterday.Format(timeInputLayout)}})
	if got, want := summaryText(t, owner.get(base).body), "Last: Yesterday, "+yesterday.Format("3:04 PM"); got != want {
		t.Fatalf("edited time: %q, want %q", got, want)
	}

	ts.clock.t = at
	owner.post(base+"/quick", nil)
	ts.clock.t = time.Date(at.Year(), at.Month(), at.Day(), 8, 0, 0, 0, la).AddDate(0, 0, 1)
	page = owner.get(base).body
	if summaryText(t, page) != "Last: Yesterday, 6:42 AM" || strings.Contains(page, "Nothing logged today") {
		t.Fatalf("next day: %q", summaryText(t, page))
	}

	older := owner.createTracker(h, "Older", url.Values{"summary_display": {summaryLast}})
	when := ts.clock.now().In(la).AddDate(0, 0, -3).Truncate(time.Minute)
	owner.post(fmt.Sprintf("/trackers/%d/entries", older.ID), url.Values{"time": {when.Format(timeInputLayout)}})
	if got, want := summaryText(t, owner.get(fmt.Sprintf("/trackers/%d", older.ID)).body), "Last: "+when.Format("Mon, Jan 2, 3:04 PM"); got != want {
		t.Fatalf("older: %q, want %q", got, want)
	}

	marked := owner.createTracker(h, "Marked none", url.Values{"summary_display": {summaryLast}})
	markedBase := fmt.Sprintf("/trackers/%d", marked.ID)
	prior := ts.clock.now().In(la).AddDate(0, 0, -1).Truncate(time.Minute)
	owner.post(markedBase+"/entries", url.Values{"time": {prior.Format(timeInputLayout)}})
	owner.post(markedBase+"/zero", nil)
	if got, want := summaryText(t, owner.get(markedBase).body), "Last: Yesterday, "+prior.Format("3:04 PM"); got != want {
		t.Fatalf("recorded zero counted as an occurrence: %q, want %q", got, want)
	}
}
