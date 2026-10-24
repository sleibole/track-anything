package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRequestBodyLimit(t *testing.T) {
	a := newTestApp(t)
	h := a.routes()

	t.Run("content length", func(t *testing.T) {
		form := url.Values{
			"email":        {"big@example.com"},
			"password":     {""},
			"timezone":     {"America/Los_Angeles"},
			turnstileField: {"test-token"},
		}
		req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.ContentLength = maxRequestBody + 1
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
		}
		var n int64
		if err := a.db.Model(&User{}).Where("email = ?", "big@example.com").Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatal("oversized signup created a user")
		}
	})

	t.Run("chunked", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/signup", bytes.NewReader(bytes.Repeat([]byte("b"), maxRequestBody+1)))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.ContentLength = -1
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status %d", rec.Code)
		}
		var n int64
		if err := a.db.Model(&User{}).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("users %d", n)
		}
	})

	t.Run("exact limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(bytes.Repeat([]byte("a"), maxRequestBody)))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusRequestEntityTooLarge {
			t.Fatal("a body at the limit was rejected")
		}
	})

	t.Run("normal signup", func(t *testing.T) {
		form := url.Values{
			"email":        {"small@example.com"},
			"password":     {""},
			"timezone":     {"America/Los_Angeles"},
			turnstileField: {"test-token"},
		}
		req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var n int64
		if err := a.db.Model(&User{}).Where("email = ?", "small@example.com").Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("status %d users %d body %s", rec.Code, n, rec.Body.String())
		}
	})
}

func TestOversizedWriteDoesNotRecord(t *testing.T) {
	ts := newTestServer(t)
	b := ts.browser(t)
	b.signup("limit@example.com", "")
	h := ts.personalHousehold(t, "limit@example.com")
	tr := b.createTracker(h, "Pills", nil)
	huge := bytes.Repeat([]byte("x"), maxRequestBody+1)
	for _, path := range []string{
		fmt.Sprintf("/trackers/%d/quick", tr.ID),
		fmt.Sprintf("/trackers/%d/zero", tr.ID),
	} {
		req, err := http.NewRequest(http.MethodPost, ts.srv.URL+path, bytes.NewReader(huge))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := b.c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
	}
	var entries, zeros int64
	if err := ts.app.db.Model(&Entry{}).Where("tracker_id = ?", tr.ID).Count(&entries).Error; err != nil {
		t.Fatal(err)
	}
	if err := ts.app.db.Model(&RecordedZero{}).Where("tracker_id = ?", tr.ID).Count(&zeros).Error; err != nil {
		t.Fatal(err)
	}
	if entries != 0 || zeros != 0 {
		t.Fatalf("entries %d zeros %d", entries, zeros)
	}
}

func TestSensitiveResponsesAreNotStored(t *testing.T) {
	a := newTestApp(t)
	h := a.routes()

	code, headers := headerGet(t, h, "/")
	if code != http.StatusOK || headers.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("home %d cache %q", code, headers.Get("Cache-Control"))
	}
	code, headers = headerGet(t, h, "/healthz")
	if code != http.StatusOK || headers.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("healthz %d cache %q", code, headers.Get("Cache-Control"))
	}
	code, headers = headerGet(t, h, "/static/app.js")
	if code != http.StatusOK || headers.Get("Cache-Control") == "private, no-store" {
		t.Fatalf("static %d cache %q", code, headers.Get("Cache-Control"))
	}
	code, headers = headerGet(t, h, "/static/splash/light/1170x2532.png")
	if code != http.StatusOK || headers.Get("Cache-Control") != "public, max-age=86400" {
		t.Fatalf("splash %d cache %q", code, headers.Get("Cache-Control"))
	}
}

func TestHSTSOnlyInProduction(t *testing.T) {
	a := newTestApp(t)
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("dev sent %q", got)
	}
	if rec.Header().Get("Content-Security-Policy") != "frame-ancestors 'none'" {
		t.Fatalf("csp %q", rec.Header().Get("Content-Security-Policy"))
	}

	a.cfg.Env = "prod"
	rec = httptest.NewRecorder()
	a.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=15552000" {
		t.Fatalf("prod HSTS %q", got)
	}
	if strings.Contains(strings.ToLower(rec.Header().Get("Strict-Transport-Security")), "includesubdomains") || strings.Contains(strings.ToLower(rec.Header().Get("Strict-Transport-Security")), "preload") {
		t.Fatalf("prod HSTS %q", rec.Header().Get("Strict-Transport-Security"))
	}
}

func TestCSVFormulaText(t *testing.T) {
	n := -10.5
	id := uint(7)
	when := time.Date(2026, 10, 23, 15, 0, 0, 0, time.UTC)
	rows, err := exportRows(Tracker{Kind: kindNumber, Unit: "=lb"}, []Entry{{
		Number:       &n,
		Note:         "=1+1",
		RecordedByID: &id,
		OccurredAt:   when,
	}}, nil, map[uint]string{7: "=who@example.com"}, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	if row[3] != "-10.5" || row[4] != "'=lb" || row[7] != "'=1+1" || row[8] != "'=who@example.com" {
		t.Fatalf("row %+v", row)
	}
	if strings.HasPrefix(row[3], "'") || strings.HasPrefix(row[5], "'") || strings.HasPrefix(row[6], "'") {
		t.Fatalf("numeric columns changed %+v", row)
	}

	plain, err := exportRows(Tracker{Kind: kindCount}, []Entry{{
		Note:       "'=hello",
		OccurredAt: when,
	}}, nil, nil, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if plain[0][7] != "'=hello" {
		t.Fatalf("note %q", plain[0][7])
	}

	for _, note := range []string{"+1", "@cmd", "\tcmd", "\rcmd", "-note"} {
		if got := csvText(note); got != "'"+note {
			t.Fatalf("%q -> %q", note, got)
		}
	}
	if csvText("hello") != "hello" || csvText("") != "" || csvText("10.5") != "10.5" {
		t.Fatal("plain text changed")
	}

	ts := newTestServer(t)
	owner := ts.browser(t)
	owner.signup("csv@example.com", "")
	household := ts.personalHousehold(t, "csv@example.com")
	tr := owner.createTracker(household, "Weight", url.Values{"kind": {kindNumber}, "unit": {"=lb"}})
	if r := owner.post(fmt.Sprintf("/trackers/%d/entries", tr.ID), url.Values{"number": {"-10.5"}, "note": {"=1+1"}}); r.status != http.StatusOK {
		t.Fatalf("log: %d %s", r.status, r.body)
	}
	req, err := http.NewRequest(http.MethodGet, ts.srv.URL+fmt.Sprintf("/trackers/%d/export", tr.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := owner.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("export %d %s", resp.StatusCode, body)
	}
	if resp.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("cache %q", resp.Header.Get("Cache-Control"))
	}
	parsed, err := csv.NewReader(resp.Body).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 2 || parsed[1][3] != "-10.5" || parsed[1][4] != "'=lb" || parsed[1][7] != "'=1+1" || parsed[1][8] != "csv@example.com" {
		t.Fatalf("csv %+v", parsed)
	}
}

func headerGet(t *testing.T, h http.Handler, path string) (int, http.Header) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Header()
}
