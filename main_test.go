package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func newTestApp(t *testing.T) *app {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := loadViews(embedded)
	if err != nil {
		t.Fatal(err)
	}
	return &app{
		cfg:    config{Env: "dev"},
		db:     db,
		views:  v,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func get(t *testing.T, h http.Handler, path string, headers map[string]string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestHealthz(t *testing.T) {
	h := newTestApp(t).routes()
	code, body := get(t, h, "/healthz", nil)
	if code != http.StatusOK || body != "ok\n" {
		t.Fatalf("got %d %q", code, body)
	}
}

func TestHomeFullPage(t *testing.T) {
	h := newTestApp(t).routes()
	code, body := get(t, h, "/", nil)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	for _, want := range []string{"<!doctype html>", "/static/pico.min.css", "The skeleton is running"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestHomeHTMXPartial(t *testing.T) {
	h := newTestApp(t).routes()
	code, body := get(t, h, "/", map[string]string{"HX-Request": "true"})
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if strings.Contains(body, "<!doctype html>") {
		t.Error("HTMX request got the full layout")
	}
	if !strings.Contains(body, "The skeleton is running") {
		t.Error("HTMX request missing page content")
	}
}

func TestStaticFiles(t *testing.T) {
	h := newTestApp(t).routes()
	for _, path := range []string{"/static/pico.min.css", "/static/htmx.min.js", "/static/app.css"} {
		if code, _ := get(t, h, path, nil); code != http.StatusOK {
			t.Errorf("%s: status %d", path, code)
		}
	}
}

func TestUnknownPathIs404(t *testing.T) {
	h := newTestApp(t).routes()
	if code, _ := get(t, h, "/nope", nil); code != http.StatusNotFound {
		t.Fatalf("status %d", code)
	}
}
