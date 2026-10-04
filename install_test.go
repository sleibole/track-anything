package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
)

func TestManifest(t *testing.T) {
	h := newTestApp(t).routes()
	req := httptest.NewRequest(http.MethodGet, "/static/manifest.webmanifest", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Errorf("content type %q", ct)
	}

	var m struct {
		Name     string `json:"name"`
		StartURL string `json:"start_url"`
		Display  string `json:"display"`
		Icons    []struct {
			Src   string `json:"src"`
			Sizes string `json:"sizes"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	if m.Name == "" || m.StartURL != "/" || m.Display != "standalone" {
		t.Errorf("manifest = %+v", m)
	}

	sizes := map[string]bool{}
	for _, ic := range m.Icons {
		sizes[ic.Sizes] = true
		if code, _ := get(t, h, ic.Src, nil); code != http.StatusOK {
			t.Errorf("manifest icon %s: status %d", ic.Src, code)
		}
	}
	for _, want := range []string{"192x192", "512x512"} {
		if !sizes[want] {
			t.Errorf("manifest missing %s icon", want)
		}
	}
}

func TestLayoutLinksInstallAssets(t *testing.T) {
	h := newTestApp(t).routes()
	_, body := get(t, h, "/", nil)
	for _, href := range []string{
		"/static/manifest.webmanifest",
		"/static/icon.svg",
		"/static/apple-touch-icon.png",
	} {
		if !strings.Contains(body, `href="`+href+`"`) {
			t.Errorf("layout does not link %s", href)
		}
		if code, _ := get(t, h, href, nil); code != http.StatusOK {
			t.Errorf("%s: status %d", href, code)
		}
	}
	for _, meta := range []string{`name="mobile-web-app-capable"`, `name="apple-mobile-web-app-capable"`} {
		if !strings.Contains(body, meta) {
			t.Errorf("layout missing %s", meta)
		}
	}
}

func TestHomeScreenOffer(t *testing.T) {
	ts := newTestServer(t)
	home := ts.browser(t).get("/")
	for _, want := range []string{
		`data-install-hint`,
		`data-install-ios`,
		"Tap <strong>Share</strong>, then <strong>Add to Home Screen</strong>",
		`data-install-menu`,
		"Install app",
		`data-install-browser`,
		"open this page in Safari or Chrome",
		`data-install-action`,
		`data-install-dismiss`,
	} {
		if !strings.Contains(home.body, want) {
			t.Errorf("logged-out home missing %q", want)
		}
	}

	dash := ts.browser(t).signup("install@example.com", "").body
	if !strings.Contains(dash, "Your trackers") || !strings.Contains(dash, `data-install-hint`) {
		t.Fatal("dashboard missing the home-screen offer")
	}

	loggedIn := ts.browser(t)
	loggedIn.signup("install-settings@example.com", "")
	for _, page := range []struct {
		name string
		path string
		body string
	}{
		{"login", "/login", ts.browser(t).get("/login").body},
		{"settings", "/settings", loggedIn.get("/settings").body},
	} {
		if strings.Contains(page.body, `data-install-hint`) {
			t.Errorf("%s includes the home-screen offer", page.name)
		}
	}

	script := ts.browser(t).get("/static/app.js").body
	for _, want := range []string{
		"beforeinstallprompt",
		"install-dismissed",
		"display-mode: standalone",
		"data-install-dismiss",
		"data-install-action",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("app.js missing %q", want)
		}
	}
}

func TestEveryIconRenders(t *testing.T) {
	files, err := fs.Glob(iconFS, "icons/*.svg")
	if err != nil || len(files) == 0 {
		t.Fatalf("no icons found: %v", err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(path.Base(f), ".svg")
		html, err := icon(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		s := string(html)
		for _, want := range []string{"<svg ", `aria-hidden="true"`, `class="icon"`, `stroke="currentColor"`} {
			if !strings.Contains(s, want) {
				t.Errorf("%s: missing %s", name, want)
			}
		}
		if strings.Contains(s, "<!--") {
			t.Errorf("%s: still has Tabler's metadata comment", name)
		}
	}
}

func TestUnknownIconIsAnError(t *testing.T) {
	if _, err := icon("no-such-icon"); err == nil {
		t.Fatal("expected an error for a missing icon")
	}
}

func TestLayoutInlinesBrandIcon(t *testing.T) {
	h := newTestApp(t).routes()
	_, body := get(t, h, "/", nil)
	if !strings.Contains(body, `<svg aria-hidden="true" class="icon"`) {
		t.Error("layout does not inline the brand icon")
	}
}
