package main

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTrackerRevIgnoresTheUndoWindow(t *testing.T) {
	card := trackerCard{Tracker: Tracker{Name: "Dog ate"}, Summary: "1 time today - Last: 9:00 PM", Count: 1, UndoURL: "/entries/1/undo"}
	stampCard(&card)
	data, view := card.DataRev, card.ViewRev
	card.UndoURL = ""
	stampCard(&card)
	if card.DataRev != data {
		t.Fatal("dropping undo changed the data revision")
	}
	if card.ViewRev == view {
		t.Fatal("dropping undo left the view revision unchanged")
	}
	card.Summary = "Nothing logged today"
	card.Count = 0
	stampCard(&card)
	if card.DataRev == data {
		t.Fatal("a new summary kept the data revision")
	}
}

func TestHomeRefreshSwapsOnlyChangedTrackers(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("refresh@example.com", "")
	h := ts.personalHousehold(t, "refresh@example.com")
	kept := owner.createTracker(h, "Kept", nil)
	changed := owner.createTracker(h, "Changed", nil)
	home := owner.get("/")
	if !strings.Contains(home.body, `hx-vals="js:{present: window.trackerRefreshVals().present, rev: window.trackerRefreshVals().rev}"`) {
		t.Fatal("refresh values are not an object expression HTMX can evaluate")
	}
	ids, revs := pageViewRevs(t, home.body)
	if !strings.Contains(ids, "tracker-order") || !strings.Contains(ids, "tracker-empty") {
		t.Fatalf("home markers missing from %q", ids)
	}

	same := refreshGet(t, owner, "/?"+url.Values{"present": {ids}, "rev": {revs}}.Encode())
	if same.status != http.StatusNoContent || same.body != "" {
		t.Fatalf("unchanged refresh: %d %q", same.status, same.body)
	}

	if r := owner.post(trackerURL(changed)+"/quick", nil); r.status != http.StatusOK {
		t.Fatalf("log: %d", r.status)
	}
	body := refreshGet(t, owner, "/?"+url.Values{"present": {ids}, "rev": {revs}}.Encode()).body
	if !strings.Contains(body, `id="tracker-`+itoa(changed.ID)+`"`) || !strings.Contains(body, `hx-swap-oob="outerHTML"`) {
		t.Fatalf("changed card was not swapped out of band:\n%s", body)
	}
	if strings.Contains(body, `id="tracker-`+itoa(kept.ID)+`"`) || strings.Contains(body, "<title>") || strings.Contains(body, "Your trackers") || strings.Contains(body, "<!doctype") {
		t.Fatalf("refresh replaced more than the changed tracker:\n%s", body)
	}
	if !strings.Contains(body, "1 time today") {
		t.Fatalf("swapped card omitted the new summary:\n%s", body)
	}

	// The data revision is what the highlight compares. An undo-only swap keeps it.
	nextIDs, nextRevs := pageViewRevs(t, owner.get("/").body)
	dataBefore := attr(t, owner.get("/").body, "tracker-"+itoa(changed.ID), "data-tracker-rev")
	ts.clock.advance(undoWindow + time.Minute)
	undoBody := refreshGet(t, owner, "/?"+url.Values{"present": {nextIDs}, "rev": {nextRevs}}.Encode()).body
	if !strings.Contains(undoBody, `id="tracker-`+itoa(changed.ID)+`"`) {
		t.Fatalf("undo expiry did not refresh the card:\n%s", undoBody)
	}
	if got := attr(t, undoBody, "tracker-"+itoa(changed.ID), "data-tracker-rev"); got != dataBefore {
		t.Fatalf("undo expiry changed the data revision %q -> %q", dataBefore, got)
	}
}

func TestHomeRefreshAddsRemovesAndReorders(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("refresh-list@example.com", "")
	h := ts.personalHousehold(t, "refresh-list@example.com")
	first := owner.createTracker(h, "First", nil)
	second := owner.createTracker(h, "Second", nil)
	ids, revs := pageViewRevs(t, owner.get("/").body)

	third := owner.createTracker(h, "Third", nil)
	added := refreshGet(t, owner, "/?"+url.Values{"present": {ids}, "rev": {revs}}.Encode()).body
	if !strings.Contains(added, `hx-swap-oob="beforeend:#tracker-list"`) || !strings.Contains(added, "Third") {
		t.Fatalf("new tracker was not appended:\n%s", added)
	}
	if !strings.Contains(added, `id="tracker-order"`) {
		t.Fatalf("new tracker did not update the order:\n%s", added)
	}

	ids, revs = pageViewRevs(t, owner.get("/").body)
	if r := owner.post(trackerURL(second)+"/archive", nil); r.status != http.StatusOK {
		t.Fatalf("archive: %d", r.status)
	}
	removed := refreshGet(t, owner, "/?"+url.Values{"present": {ids}, "rev": {revs}}.Encode()).body
	if !strings.Contains(removed, `hx-swap-oob="delete:#tracker-`+itoa(second.ID)+`"`) {
		t.Fatalf("archived tracker was not deleted:\n%s", removed)
	}

	ids, revs = pageViewRevs(t, owner.get("/").body)
	if r := owner.post("/trackers/order", idValues(third.ID, first.ID)); r.status != http.StatusNoContent {
		t.Fatalf("reorder: %d %s", r.status, r.body)
	}
	order := refreshGet(t, owner, "/?"+url.Values{"present": {ids}, "rev": {revs}}.Encode()).body
	if !strings.Contains(order, `data-order="`+itoa(third.ID)+`,`+itoa(first.ID)+`"`) {
		t.Fatalf("reorder fragment:\n%s", order)
	}
	if strings.Contains(order, "hx-swap-oob=\"outerHTML\"") && strings.Contains(order, "tracker-card") {
		t.Fatalf("reorder swapped card contents:\n%s", order)
	}
}

func TestTrackerRefreshAndInPlaceLog(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("refresh-page@example.com", "")
	h := ts.personalHousehold(t, "refresh-page@example.com")
	tr := owner.createTracker(h, "Dog ate", nil)
	path := trackerURL(tr)
	ids, revs := pageViewRevs(t, owner.get(path).body)
	for _, id := range []string{logRegionID(tr.ID), trendRegionID(tr.ID), historyRegionID(tr.ID)} {
		if !strings.Contains(ids, id) {
			t.Errorf("tracker page missing %s in %q", id, ids)
		}
	}
	same := refreshGet(t, owner, path+"?"+url.Values{"present": {ids}, "rev": {revs}}.Encode())
	if same.status != http.StatusNoContent {
		t.Fatalf("unchanged tracker refresh: %d %s", same.status, same.body)
	}

	if r := owner.post(path+"/quick", nil); r.status != http.StatusOK {
		t.Fatalf("log: %d", r.status)
	}
	body := refreshGet(t, owner, path+"?"+url.Values{"present": {ids}, "rev": {revs}}.Encode()).body
	if !strings.Contains(body, `id="`+logRegionID(tr.ID)+`"`) || !strings.Contains(body, "1 time today") {
		t.Fatalf("tracker refresh missed the log:\n%s", body)
	}
	if strings.Contains(body, "<title>") || strings.Contains(body, "<!doctype") {
		t.Fatal("tracker refresh returned a document")
	}

	req, err := http.NewRequest(http.MethodPost, ts.srv.URL+path+"/quick", strings.NewReader(url.Values{"back": {"/"}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Current-URL", ts.srv.URL+"/")
	logged := refreshDo(t, owner, req)
	if logged.status != http.StatusOK || logged.header.Get("HX-Reswap") != "none" {
		t.Fatalf("in-place log: %d reswap %q\n%s", logged.status, logged.header.Get("HX-Reswap"), logged.body)
	}
	if !strings.Contains(logged.body, `hx-swap-oob="outerHTML"`) || !strings.Contains(logged.body, "2 times today") {
		t.Fatalf("in-place log body:\n%s", logged.body)
	}
	if strings.Contains(logged.body, "Your trackers") || strings.Contains(logged.body, "<title>") {
		t.Fatal("in-place log replaced the page")
	}
}

func TestDayRolloverChangesTheTrackerRevision(t *testing.T) {
	ts := newTestServer(t)
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	setClock(ts.clock, time.Date(2026, 10, 24, 23, 0, 0, 0, la))
	owner := ts.browser(t)
	owner.signup("rollover@example.com", "")
	h := ts.personalHousehold(t, "rollover@example.com")
	tr := owner.createTracker(h, "Dog ate", nil)
	if r := owner.post(trackerURL(tr)+"/quick", nil); r.status != http.StatusOK {
		t.Fatalf("log: %d", r.status)
	}
	home := owner.get("/").body
	if !strings.Contains(home, "1 time today") {
		t.Fatalf("before midnight:\n%s", home)
	}
	ids, revs := pageViewRevs(t, home)
	dataBefore := attr(t, home, "tracker-"+itoa(tr.ID), "data-tracker-rev")
	setClock(ts.clock, time.Date(2026, 10, 25, 0, 30, 0, 0, la))
	body := refreshGet(t, owner, "/?"+url.Values{"present": {ids}, "rev": {revs}}.Encode()).body
	if !strings.Contains(body, "Nothing logged today") {
		t.Fatalf("after midnight:\n%s", body)
	}
	if got := attr(t, body, "tracker-"+itoa(tr.ID), "data-tracker-rev"); got == "" || got == dataBefore {
		t.Fatalf("midnight kept data revision %q", got)
	}
}

func TestShareRefresh(t *testing.T) {
	ts, owner, tr, link := shareLink(t)
	ids, revs := pageViewRevs(t, ts.browser(t).get(link).body)
	guest := ts.browser(t)
	same := refreshGet(t, guest, link+"?"+url.Values{"present": {ids}, "rev": {revs}}.Encode())
	if same.status != http.StatusNoContent {
		t.Fatalf("unchanged share refresh: %d %s", same.status, same.body)
	}
	if r := owner.post(trackerURL(tr)+"/quick", nil); r.status != http.StatusOK {
		t.Fatalf("log: %d", r.status)
	}
	body := refreshGet(t, guest, link+"?"+url.Values{"present": {ids}, "rev": {revs}}.Encode()).body
	if !strings.Contains(body, "1 time today") || !strings.Contains(body, `hx-swap-oob="outerHTML"`) {
		t.Fatalf("share refresh:\n%s", body)
	}
	if strings.Contains(body, "<!doctype") || strings.Contains(body, "Secret tracker") {
		t.Fatal("share refresh leaked the document or another tracker")
	}
}

type refreshResponse struct {
	status int
	body   string
	header http.Header
}

func refreshGet(t *testing.T, b *browser, path string) refreshResponse {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, b.ts.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Trigger", trackerRefreshID)
	return refreshDo(t, b, req)
}

func refreshDo(t *testing.T, b *browser, req *http.Request) refreshResponse {
	t.Helper()
	resp, err := b.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return refreshResponse{status: resp.StatusCode, body: string(buf), header: resp.Header}
}

func pageViewRevs(t *testing.T, body string) (string, string) {
	t.Helper()
	re := regexp.MustCompile(`id="([^"]+)"[^>]*data-view-rev="([^"]*)"`)
	var ids, revs []string
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		ids = append(ids, m[1])
		revs = append(revs, m[2])
	}
	if len(ids) == 0 {
		t.Fatalf("no view revisions in page:\n%s", body)
	}
	return strings.Join(ids, ","), strings.Join(revs, ",")
}

func attr(t *testing.T, body, id, name string) string {
	t.Helper()
	re := regexp.MustCompile(`id="` + regexp.QuoteMeta(id) + `"[^>]*` + regexp.QuoteMeta(name) + `="([^"]*)"`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no %s on %s", name, id)
	}
	return m[1]
}

func itoa(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}

func setClock(c *fakeClock, at time.Time) {
	c.mu.Lock()
	c.t = at
	c.mu.Unlock()
}
