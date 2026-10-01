package main

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// memMailer records emails instead of sending them. setErr makes Send fail
// without recording a message, for delivery-failure tests.
type memMailer struct {
	mu   sync.Mutex
	sent []sentEmail
	err  error
}

type sentEmail struct {
	To, Subject, Body string
}

func (m *memMailer) setErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

func (m *memMailer) Send(to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, sentEmail{to, subject, body})
	return nil
}

func (m *memMailer) messages() []sentEmail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sentEmail(nil), m.sent...)
}

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
	cfg := config{Env: "dev", BaseURL: "http://example.test"}
	return newApp(cfg, db, v, slog.New(slog.NewTextHandler(io.Discard, nil)), &memMailer{})
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
	for _, headers := range []map[string]string{nil, {"Cookie": "session=not-a-session"}} {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
			t.Fatalf("got %d %q", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
			t.Fatalf("content-type %q", ct)
		}
	}
}

func TestHealthzDBFailure(t *testing.T) {
	var buf bytes.Buffer
	a := newTestApp(t)
	a.logger = slog.New(slog.NewTextHandler(&buf, nil))
	sqlDB, err := a.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	h := a.routes()
	for _, headers := range []map[string]string{nil, {"Cookie": "session=not-a-session"}} {
		buf.Reset()
		code, body := get(t, h, "/healthz", headers)
		if code != http.StatusServiceUnavailable || body != "unavailable\n" {
			t.Fatalf("got %d %q", code, body)
		}
		logged := buf.String()
		if !strings.Contains(logged, "healthz: database check failed") || !strings.Contains(logged, "database is closed") {
			t.Fatalf("log: %s", logged)
		}
		if strings.Contains(body, "database is closed") || strings.Contains(body, "sqlite") {
			t.Fatalf("response leaked details: %q", body)
		}
	}
}

func TestHomeFullPage(t *testing.T) {
	h := newTestApp(t).routes()
	code, body := get(t, h, "/", nil)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	for _, want := range []string{"<!doctype html>", "/static/pico.min.css", "Track how often anything happens", `href="/signup"`} {
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
	if !strings.Contains(body, "Track how often anything happens") {
		t.Error("HTMX request missing page content")
	}
}

func TestStaticFiles(t *testing.T) {
	h := newTestApp(t).routes()
	for _, path := range []string{"/static/pico.min.css", "/static/htmx.min.js", "/static/app.css", "/static/app.js"} {
		if code, _ := get(t, h, path, nil); code != http.StatusOK {
			t.Errorf("%s: status %d", path, code)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := newTestApp(t).routes()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("nosniff: %q", rec.Header().Get("X-Content-Type-Options"))
	}
	if rec.Header().Get("Content-Security-Policy") != "frame-ancestors 'none'" {
		t.Errorf("csp: %q", rec.Header().Get("Content-Security-Policy"))
	}
	if rec.Header().Get("Referrer-Policy") != "same-origin" {
		t.Errorf("referrer: %q", rec.Header().Get("Referrer-Policy"))
	}
}

func TestRequestLogsHideBearerTokens(t *testing.T) {
	var buf bytes.Buffer
	a := newTestApp(t)
	a.logger = slog.New(slog.NewTextHandler(&buf, nil))
	h := a.routes()
	const token = "super-secret-token-value"
	for _, path := range []string{
		"/login/link/" + token,
		"/verify/" + token,
		"/verify/done",
		"/s/" + token,
		"/s/" + token + "/quick",
		"/join/" + token,
	} {
		buf.Reset()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		h.ServeHTTP(httptest.NewRecorder(), req)
		logged := buf.String()
		if strings.Contains(logged, token) {
			t.Errorf("%s logged the token: %s", path, logged)
		}
	}
	buf.Reset()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/login/link/"+token, nil))
	if !strings.Contains(buf.String(), "/login/link/{token}") {
		t.Fatalf("log: %s", buf.String())
	}
	buf.Reset()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/verify/done", nil))
	if strings.Contains(buf.String(), "/verify/{token}") || !strings.Contains(buf.String(), "/verify/done") {
		t.Fatalf("verify done log: %s", buf.String())
	}
}

func TestValidateConfig(t *testing.T) {
	ok := config{Env: "dev", BaseURL: "http://localhost:8080"}
	if err := ok.validate(); err != nil {
		t.Fatal(err)
	}
	prod := config{Env: "prod", BaseURL: "https://trackanything.io", baseURLSet: true, TrustedIPHeader: "CF-Connecting-IP"}
	if err := prod.validate(); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []config{
		{Env: "production", BaseURL: "https://trackanything.io", baseURLSet: true},
		{Env: "prod", BaseURL: "http://localhost:8080"},
		{Env: "prod", BaseURL: "https://trackanything.io"},
		{Env: "prod", BaseURL: "http://trackanything.io", baseURLSet: true},
		{Env: "prod", BaseURL: "https://", baseURLSet: true},
		{Env: "dev", TrustedIPHeader: "X-Real-IP"},
	} {
		if err := cfg.validate(); err == nil {
			t.Errorf("accepted %+v", cfg)
		}
	}
}

func TestLoadConfigReadsTrustedProxyHeader(t *testing.T) {
	t.Setenv("ENV", "dev")
	t.Setenv("BASE_URL", "")
	t.Setenv("TRUSTED_IP_HEADER", "CF-Connecting-IP")
	cfg := loadConfig()
	if cfg.TrustedIPHeader != "CF-Connecting-IP" || cfg.baseURLSet {
		t.Fatalf("%+v", cfg)
	}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENV", "prod")
	t.Setenv("BASE_URL", "https://trackanything.io")
	cfg = loadConfig()
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownPathIs404(t *testing.T) {
	h := newTestApp(t).routes()
	if code, _ := get(t, h, "/nope", nil); code != http.StatusNotFound {
		t.Fatalf("status %d", code)
	}
}
