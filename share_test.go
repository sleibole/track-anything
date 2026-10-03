package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// shareLink turns on a share link for a new "Dog ate" tracker and returns its path.
func shareLink(t *testing.T) (ts *testServer, owner *browser, tr Tracker, link string) {
	t.Helper()
	ts = newTestServer(t)
	owner = ts.browser(t)
	owner.signup("owner@example.com", "")
	h := ts.personalHousehold(t, "owner@example.com")
	tr = owner.createTracker(h, "Dog ate", url.Values{"log_label": {"+ Ate"}})
	owner.createTracker(h, "Secret tracker", nil)
	r := owner.post(fmt.Sprintf("/trackers/%d/share", tr.ID), nil)
	tr = ts.tracker(t, tr.ID)
	if tr.ShareToken == nil || !strings.Contains(r.body, "http://example.test/s/"+*tr.ShareToken) {
		t.Fatal("share link not shown to the owner")
	}
	return ts, owner, tr, "/s/" + *tr.ShareToken
}

func TestTrackerPagesRefreshThemselves(t *testing.T) {
	ts, owner, tr, link := shareLink(t)
	trigger := `hx-trigger="every 30s [trackerIdle()], visibilitychange[trackerIdle()] from:document"`
	for _, path := range []string{"/", fmt.Sprintf("/trackers/%d", tr.ID)} {
		body := owner.get(path).body
		if !strings.Contains(body, `data-refresh hx-get="`+path+`"`) || !strings.Contains(body, trigger) {
			t.Errorf("%s does not refresh itself", path)
		}
	}
	if body := ts.browser(t).get(link).body; !strings.Contains(body, `data-refresh hx-get="`+link+`"`) {
		t.Error("share page does not refresh itself")
	}
	for _, path := range []string{fmt.Sprintf("/trackers/%d/edit", tr.ID), fmt.Sprintf("/households/%d", tr.HouseholdID), "/trackers/new", "/settings"} {
		if strings.Contains(owner.get(path).body, "data-refresh") {
			t.Errorf("%s refreshes, but it is a form page", path)
		}
	}

	req, _ := http.NewRequest(http.MethodGet, ts.srv.URL+fmt.Sprintf("/trackers/%d", tr.ID), nil)
	req.Header.Set("HX-Request", "true")
	r := owner.do(req)
	if r.status != http.StatusOK || strings.Contains(r.body, "<!doctype html>") || !strings.Contains(r.body, "Dog ate") {
		t.Fatalf("refresh request: %d, full layout %v", r.status, strings.Contains(r.body, "<!doctype html>"))
	}

	script := owner.get("/static/app.js").body
	for _, want := range []string{"window.trackerIdle", `details[open], .htmx-request`, `"input, textarea, select"`, "htmx:abort"} {
		if !strings.Contains(script, want) {
			t.Errorf("app.js missing %q", want)
		}
	}
}

func TestShareLinkLogsWithoutAnAccount(t *testing.T) {
	ts, owner, tr, link := shareLink(t)
	sitter := ts.browser(t)

	req, _ := http.NewRequest(http.MethodGet, ts.srv.URL+link, nil)
	resp, err := sitter.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Referrer-Policy") != "no-referrer" || resp.Header.Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("share page: %d %v", resp.StatusCode, resp.Header)
	}

	page := sitter.get(link).body
	for _, want := range []string{"Dog ate", ">&#43; Ate</button>", "Nothing logged today", "Record none for today"} {
		if !strings.Contains(page, want) {
			t.Errorf("share page missing %q", want)
		}
	}
	for _, unwanted := range []string{"Secret tracker", "/edit", `type="datetime-local"`, `name="note"`, "owner@example.com"} {
		if strings.Contains(page, unwanted) {
			t.Errorf("share page shows %q", unwanted)
		}
	}

	r := sitter.post(link+"/quick", nil)
	if r.url.Path != link || !strings.Contains(r.body, "1 time today") {
		t.Fatalf("after log: %s", r.url.Path)
	}
	e := ts.entries(t, tr.ID)[0]
	if !e.ViaLink || e.RecordedByID != nil {
		t.Fatalf("entry %+v", e)
	}
	if !strings.Contains(owner.get(fmt.Sprintf("/trackers/%d", tr.ID)).body, "via share link") {
		t.Error("owner doesn't see that it came through the link")
	}

	sitter.post(fmt.Sprintf("%s/entries/%d/undo", link, e.ID), nil)
	if len(ts.entries(t, tr.ID)) != 0 {
		t.Fatal("undo through the link failed")
	}

	sitter.post(link+"/quick", nil)
	e = ts.entries(t, tr.ID)[0]
	ts.clock.advance(16 * time.Minute)
	if r := sitter.post(fmt.Sprintf("%s/entries/%d/undo", link, e.ID), nil); r.status != http.StatusForbidden || len(ts.entries(t, tr.ID)) != 1 {
		t.Fatalf("undo after the window: %d", r.status)
	}
}

func TestShareLinkRecordsNoneForToday(t *testing.T) {
	ts, _, tr, link := shareLink(t)
	sitter := ts.browser(t)

	r := sitter.post(link+"/zero", nil)
	zs := ts.zeros(t, tr.ID)
	if !strings.Contains(r.body, "None today") || len(zs) != 1 || !zs[0].ViaLink {
		t.Fatalf("zero through the link: %+v", zs)
	}
	sitter.post(fmt.Sprintf("%s/zeros/%d/undo", link, zs[0].ID), nil)
	if len(ts.zeros(t, tr.ID)) != 0 {
		t.Fatal("undo none through the link failed")
	}
	sitter.post(link+"/quick", nil)
	if r := sitter.post(link+"/zero", nil); r.status != http.StatusUnprocessableEntity || len(ts.zeros(t, tr.ID)) != 0 {
		t.Fatalf("zero after an entry: %d", r.status)
	}
}

func TestShareLinkCantReachOtherTrackers(t *testing.T) {
	ts, owner, tr, link := shareLink(t)
	var other Tracker
	ts.app.db.Take(&other, "name = ?", "Secret tracker")
	owner.post(fmt.Sprintf("/trackers/%d/quick", other.ID), nil)
	e := ts.entries(t, other.ID)[0]
	owner.post(fmt.Sprintf("/trackers/%d/zero", tr.ID), nil)

	sitter := ts.browser(t)
	if r := sitter.post(fmt.Sprintf("%s/entries/%d/undo", link, e.ID), nil); r.status != http.StatusNotFound {
		t.Fatalf("undo another tracker's entry: %d", r.status)
	}
	if len(ts.entries(t, other.ID)) != 1 {
		t.Fatal("entry removed through another tracker's link")
	}
	// Owner-only actions aren't reachable without a session.
	for _, path := range []string{fmt.Sprintf("/trackers/%d/archive", tr.ID), fmt.Sprintf("/entries/%d/delete", e.ID)} {
		if r := sitter.post(path, nil); r.url.Path != "/login" {
			t.Errorf("POST %s without a session ended at %s", path, r.url.Path)
		}
	}
}

func TestSharePageKeepsStricterReferrerPolicy(t *testing.T) {
	ts, _, _, link := shareLink(t)
	resp, err := ts.srv.Client().Get(ts.srv.URL + link)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("referrer: %q", resp.Header.Get("Referrer-Policy"))
	}
	if resp.Header.Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("robots: %q", resp.Header.Get("X-Robots-Tag"))
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("share page lost nosniff")
	}
}

func TestShareRegenerateAsksForConfirmation(t *testing.T) {
	_, owner, tr, _ := shareLink(t)
	body := owner.get(fmt.Sprintf("/trackers/%d/edit", tr.ID)).body
	if !strings.Contains(body, `hx-confirm="Make a new link? The current one will stop working."`) {
		t.Fatal("regenerating a share link has no confirmation")
	}
	if strings.Contains(body, `Turn on a share link`) {
		t.Fatal("confirmation test ran before a link existed")
	}
}

func TestShareLinkRegenerateAndTurnOff(t *testing.T) {
	ts, owner, tr, oldLink := shareLink(t)
	sitter := ts.browser(t)

	owner.post(fmt.Sprintf("/trackers/%d/share", tr.ID), nil)
	newLink := "/s/" + *ts.tracker(t, tr.ID).ShareToken
	if newLink == oldLink {
		t.Fatal("regenerate kept the same token")
	}
	if r := sitter.get(oldLink); r.status != http.StatusNotFound {
		t.Errorf("old link GET: %d", r.status)
	}
	if r := sitter.post(oldLink+"/quick", nil); r.status != http.StatusNotFound {
		t.Errorf("old link POST: %d", r.status)
	}
	if r := sitter.get(newLink); r.status != http.StatusOK {
		t.Errorf("new link: %d", r.status)
	}

	owner.post(fmt.Sprintf("/trackers/%d/share/delete", tr.ID), nil)
	if r := sitter.get(newLink); r.status != http.StatusNotFound {
		t.Errorf("link after turning off: %d", r.status)
	}
	if len(ts.entries(t, tr.ID)) != 0 {
		t.Fatal("dead links logged entries")
	}
}

func TestSharePageTodayFollowsTheHouseholdCreatorsZone(t *testing.T) {
	ts, _, tr, link := shareLink(t) // owner is in America/Los_Angeles
	day := time.Now().UTC().Truncate(24 * time.Hour)
	ts.clock.t = day.Add(6*time.Hour + 30*time.Minute) // evening before, in Los Angeles
	sitter := ts.browser(t)
	sitter.post(link+"/quick", nil)

	ts.clock.t = day.Add(9 * time.Hour) // after midnight in Los Angeles
	if !strings.Contains(sitter.get(link).body, "Nothing logged today") {
		t.Error("share page's today doesn't follow the creator's zone")
	}
	if len(ts.entries(t, tr.ID)) != 1 {
		t.Fatal("entry missing")
	}
}

func TestSharePageMatchesTheCardSummary(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	h := ts.personalHousehold(t, "owner@example.com")
	done := owner.createTracker(h, "McGill Big 3", url.Values{"summary_display": {summaryDone}})
	last := owner.createTracker(h, "Logan Motrin", url.Values{"summary_display": {summaryLast}})

	la := location("America/Los_Angeles")
	now := ts.clock.now().In(la)
	ts.clock.t = time.Date(now.Year(), now.Month(), now.Day(), 6, 42, 0, 0, la)
	owner.post(fmt.Sprintf("/trackers/%d/quick", done.ID), nil)
	owner.post(fmt.Sprintf("/trackers/%d/quick", last.ID), nil)
	for _, id := range []uint{done.ID, last.ID} {
		owner.post(fmt.Sprintf("/trackers/%d/share", id), nil)
	}
	done = ts.tracker(t, done.ID)
	last = ts.tracker(t, last.ID)
	sitter := ts.browser(t)

	for _, tr := range []Tracker{done, last} {
		card := summaryText(t, owner.get(fmt.Sprintf("/trackers/%d", tr.ID)).body)
		shared := summaryText(t, sitter.get("/s/"+*tr.ShareToken).body)
		if card != shared || card == "" {
			t.Errorf("%s card %q, share %q", tr.Name, card, shared)
		}
	}
	if summaryText(t, sitter.get("/s/"+*done.ShareToken).body) != "Done today" {
		t.Error("share page didn't show Done today")
	}
	if summaryText(t, sitter.get("/s/"+*last.ShareToken).body) != "Last: 6:42 AM" {
		t.Error("share page didn't show the last occurrence")
	}
}

func TestShareLinkPostsAreRateLimited(t *testing.T) {
	ts, _, _, link := shareLink(t)
	sitter := ts.browser(t)
	for i := range 30 {
		if r := sitter.post(link+"/quick", nil); r.status != http.StatusOK {
			t.Fatalf("post %d: %d", i+1, r.status)
		}
	}
	if r := sitter.post(link+"/quick", nil); r.status != http.StatusTooManyRequests {
		t.Fatalf("31st post: %d", r.status)
	}
}

func TestShareLinkPostsRequireSameOrigin(t *testing.T) {
	ts, _, tr, link := shareLink(t)
	req, _ := http.NewRequest(http.MethodPost, ts.srv.URL+link+"/quick", nil)
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || len(ts.entries(t, tr.ID)) != 0 {
		t.Fatalf("cross-origin share post: %d", resp.StatusCode)
	}
}
