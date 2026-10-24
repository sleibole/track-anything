package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestInAppLinksBoostAndShellLinksDoNot(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")
	home := owner.get("/").body
	boost := `hx-boost="true" hx-target="#content" hx-swap="innerHTML show:window:top" `
	for _, href := range []string{`href="/"`, `href="/charts"`, `href="/settings"`} {
		if !strings.Contains(home, boost+href) {
			t.Errorf("signed-in home missing boosted %s", href)
		}
	}
	if !strings.Contains(home, `hx-history-elt`) || !strings.Contains(home, `id="content"`) {
		t.Fatal("content is not the history element")
	}
	if strings.Contains(home, `hx-post="/logout"`) || !strings.Contains(home, `action="/logout"`) {
		t.Fatal("log out should stay a normal post")
	}

	loggedOut := ts.browser(t).get("/").body
	for _, href := range []string{`href="/login"`, `href="/signup"`} {
		if strings.Contains(loggedOut, boost+href) || !strings.Contains(loggedOut, href) {
			t.Errorf("logged-out %s should be a normal link", href)
		}
	}

	h := ts.personalHousehold(t, "owner@example.com")
	tr := owner.createTracker(h, "Dog ate", nil)
	export := fmt.Sprintf(`href="/trackers/%d/export"`, tr.ID)
	page := owner.get(fmt.Sprintf("/trackers/%d", tr.ID)).body
	if strings.Contains(page, boost+export) || !strings.Contains(page, export) {
		t.Fatal("CSV export should stay a normal link")
	}

	settings := ts.confirmAndSetPassword(t, "pw@example.com", "long-password").get("/settings").body
	if strings.Contains(settings, `hx-post="/settings/password"`) || !strings.Contains(settings, `action="/settings/password"`) {
		t.Fatal("change password should stay a normal post")
	}
}

func TestHTMXPartialIncludesTitle(t *testing.T) {
	h := newTestApp(t).routes()
	code, body := get(t, h, "/", map[string]string{"HX-Request": "true"})
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if strings.Contains(body, "<!doctype") || strings.Contains(body, "<!DOCTYPE") {
		t.Fatal("HTMX response included a document")
	}
	if !strings.Contains(body, "<title>Track Anything</title>") {
		t.Fatalf("missing title: %s", body)
	}
	if !strings.Contains(body, "Track how often anything happens") {
		t.Fatal("HTMX response missing page content")
	}
}

func TestSignedOutHTMXRequestReloadsTheDocument(t *testing.T) {
	h := newTestApp(t).routes()
	req := httptest.NewRequest(http.MethodGet, "/charts", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "/login?next=%2Fcharts" {
		t.Fatalf("HX-Redirect %q", got)
	}
	if rec.Header().Get("HX-Location") != "" || rec.Header().Get("Location") != "" {
		t.Fatal("signed-out HTMX request also sent a swap or a 303")
	}

	plain := httptest.NewRequest(http.MethodGet, "/charts", nil)
	got := httptest.NewRecorder()
	h.ServeHTTP(got, plain)
	if got.Code != http.StatusSeeOther {
		t.Fatalf("normal status %d", got.Code)
	}
	if loc := got.Header().Get("Location"); loc != "/login?next=%2Fcharts" {
		t.Fatalf("Location %q", loc)
	}
	if got.Header().Get("HX-Redirect") != "" {
		t.Fatal("normal request sent HX-Redirect")
	}
}

func TestRedirectToSwapsInsideTheApp(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("owner@example.com", "")

	resp := doRequest(t, owner.c, hxPost(t, ts, "/households", url.Values{"name": {"Cabin"}}, ts.srv.URL+"/"))
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status %d\n%s", resp.StatusCode, body)
	}
	resp.Body.Close()
	if resp.Header.Get("HX-Redirect") != "" {
		t.Fatal("in-app redirect used HX-Redirect")
	}
	loc := decodeLocation(t, resp.Header.Get("HX-Location"))
	if loc.Target != "#content" || loc.Swap != "innerHTML show:window:top" || loc.Push != "true" {
		t.Fatalf("location %+v", loc)
	}
	if !strings.HasPrefix(loc.Path, "/households/") {
		t.Fatalf("path %q", loc.Path)
	}

	h := ts.personalHousehold(t, "owner@example.com")
	current := ts.srv.URL + fmt.Sprintf("/households/%d", h.ID)
	same := doRequest(t, owner.c, hxPost(t, ts, fmt.Sprintf("/households/%d", h.ID), url.Values{"name": {"Mine"}}, current))
	if same.StatusCode != http.StatusNoContent {
		same.Body.Close()
		t.Fatalf("rename status %d", same.StatusCode)
	}
	same.Body.Close()
	renamed := decodeLocation(t, same.Header.Get("HX-Location"))
	if renamed.Push != "false" || renamed.Path != fmt.Sprintf("/households/%d", h.ID) || renamed.Target != "#content" {
		t.Fatalf("same-page location %+v", renamed)
	}

	noFollow := &http.Client{Jar: owner.c.Jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := url.Values{"name": {"Shed"}}
	req, err := http.NewRequest(http.MethodPost, ts.srv.URL+"/households", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	normal, err := noFollow.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	normal.Body.Close()
	if normal.StatusCode != http.StatusSeeOther || normal.Header.Get("HX-Location") != "" || normal.Header.Get("HX-Redirect") != "" {
		t.Fatalf("normal redirect: %d location %q redirect %q", normal.StatusCode, normal.Header.Get("HX-Location"), normal.Header.Get("HX-Redirect"))
	}
	if !strings.HasPrefix(normal.Header.Get("Location"), "/households/") {
		t.Fatalf("Location %q", normal.Header.Get("Location"))
	}
}

type hxLoc struct {
	Path   string `json:"path"`
	Target string `json:"target"`
	Swap   string `json:"swap"`
	Push   string `json:"push"`
}

func decodeLocation(t *testing.T, raw string) hxLoc {
	t.Helper()
	var loc hxLoc
	if err := json.Unmarshal([]byte(raw), &loc); err != nil {
		t.Fatalf("HX-Location %q: %v", raw, err)
	}
	return loc
}

func hxPost(t *testing.T, ts *testServer, path string, form url.Values, current string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.srv.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Current-URL", current)
	return req
}

func doRequest(t *testing.T, c *http.Client, req *http.Request) *http.Response {
	t.Helper()
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
