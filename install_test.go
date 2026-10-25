package main

import (
	"encoding/json"
	"image/color"
	"image/png"
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
	for _, link := range []string{
		`<link rel="apple-touch-startup-image" href="/static/splash/light/1170x2532.png" media="(prefers-color-scheme: light) and (device-width: 390px) and (device-height: 844px) and (-webkit-device-pixel-ratio: 3) and (orientation: portrait)">`,
		`<link rel="apple-touch-startup-image" href="/static/splash/dark/1170x2532.png" media="(prefers-color-scheme: dark) and (device-width: 390px) and (device-height: 844px) and (-webkit-device-pixel-ratio: 3) and (orientation: portrait)">`,
		`<link rel="apple-touch-startup-image" href="/static/splash/light/2532x1170.png" media="(prefers-color-scheme: light) and (device-width: 390px) and (device-height: 844px) and (-webkit-device-pixel-ratio: 3) and (orientation: landscape)">`,
		`<link rel="apple-touch-startup-image" href="/static/splash/dark/2532x1170.png" media="(prefers-color-scheme: dark) and (device-width: 390px) and (device-height: 844px) and (-webkit-device-pixel-ratio: 3) and (orientation: landscape)">`,
		`html { background-color: #fff; }`,
		`html { background-color: #13171f; }`,
	} {
		if !strings.Contains(body, link) {
			t.Errorf("layout missing %s", link)
		}
	}
}

func TestSplashImage(t *testing.T) {
	h := newTestApp(t).routes()
	cases := []struct {
		path string
		w, h int
		bg   color.RGBA
	}{
		{"/static/splash/light/1170x2532.png", 1170, 2532, splashLight},
		{"/static/splash/dark/1170x2532.png", 1170, 2532, splashDark},
		{"/static/splash/light/2532x1170.png", 2532, 1170, splashLight},
		{"/static/splash/dark/2532x1170.png", 2532, 1170, splashDark},
	}
	for _, tc := range cases {
		code, body := get(t, h, tc.path, nil)
		if code != http.StatusOK {
			t.Errorf("%s: status %d", tc.path, code)
			continue
		}
		img, err := png.Decode(strings.NewReader(body))
		if err != nil {
			t.Errorf("%s: %v", tc.path, err)
			continue
		}
		if img.Bounds().Dx() != tc.w || img.Bounds().Dy() != tc.h {
			t.Errorf("%s: size %dx%d", tc.path, img.Bounds().Dx(), img.Bounds().Dy())
		}
		got := color.RGBAModel.Convert(img.At(0, 0)).(color.RGBA)
		if got != tc.bg {
			t.Errorf("%s: corner %v, want %v", tc.path, got, tc.bg)
		}
	}
	if code, _ := get(t, h, "/static/splash/light/1x1.png", nil); code != http.StatusNotFound {
		t.Errorf("unknown splash size: status %d", code)
	}
}

func TestHomeScreenOffer(t *testing.T) {
	ts := newTestServer(t)
	home := ts.browser(t).get("/")
	for _, want := range []string{
		`data-install-footer`,
		`id="install-dialog"`,
		`Install app`,
		`Add to Home Screen`,
		`Open as Web App`,
		`Page Menu`,
		`Having trouble? Open this page in Safari and follow these steps.`,
		`data-install-panel="android"`,
		`Add to Home screen`,
		`data-install-prompt`,
		`aria-label="Close"`,
		`beforeinstallprompt`,
		`"standalone"`,
	} {
		if !strings.Contains(home.body, want) {
			t.Errorf("logged-out home missing %q", want)
		}
	}
	if strings.Contains(home.body, `data-install-hint`) || strings.Contains(home.body, `data-install-dismiss`) {
		t.Error("logged-out home still includes the old install banner")
	}

	dash := ts.browser(t).signup("install@example.com", "").body
	if !strings.Contains(dash, "Your trackers") || !strings.Contains(dash, `data-install-footer`) || !strings.Contains(dash, `id="install-dialog"`) {
		t.Fatal("dashboard missing the install control")
	}

	loggedIn := ts.browser(t)
	loggedIn.signup("install-settings@example.com", "")
	for _, page := range []struct {
		name string
		body string
	}{
		{"login", ts.browser(t).get("/login").body},
		{"settings", loggedIn.get("/settings").body},
	} {
		if !strings.Contains(page.body, `data-install-footer`) || !strings.Contains(page.body, `id="install-dialog"`) {
			t.Errorf("%s missing the install control", page.name)
		}
		if strings.Contains(page.body, `data-install-hint`) {
			t.Errorf("%s still includes the old install banner", page.name)
		}
	}

	script := ts.browser(t).get("/static/app.js").body
	for _, want := range []string{
		"beforeinstallprompt",
		"appinstalled",
		"navigator.standalone",
		`"standalone"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("app.js missing %q", want)
		}
	}
	if strings.Contains(script, "install-dismissed") || strings.Contains(script, "data-install-dismiss") || strings.Contains(script, "data-install-action") {
		t.Error("app.js still implements the old dismissible banner")
	}
	css := ts.browser(t).get("/static/app.css").body
	if !strings.Contains(css, "display-mode: standalone") {
		t.Error("app.css does not hide the footer in standalone display mode")
	}
}

func installDialogHTML(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `<dialog id="install-dialog"`)
	if start < 0 {
		t.Fatal("page has no install dialog")
	}
	end := strings.Index(body[start:], `</dialog>`)
	if end < 0 {
		t.Fatal("install dialog is not closed")
	}
	return body[start : start+end]
}

func TestInstallDialog(t *testing.T) {
	ts := newTestServer(t)
	home := ts.browser(t).get("/")
	if strings.Count(home.body, `id="install-dialog"`) != 1 {
		t.Fatalf("dialog count %d", strings.Count(home.body, `id="install-dialog"`))
	}
	for _, want := range []string{
		`data-install-footer`,
		`data-install-open`,
		`aria-haspopup="dialog"`,
		`>Install app</button>`,
		`Install Track Anything`,
		`/static/icons/icon-192.png`,
		`aria-label="Close"`,
		`Add to Home Screen`,
		`Open as Web App`,
		`Page Menu`,
		`Having trouble? Open this page in Safari and follow these steps.`,
		`data-install-panel="ios"`,
		`data-install-panel="android"`,
		`data-install-panel="desktop"`,
		`Add to Home screen`,
		`address bar`,
		`<button type="button" data-install-prompt>Install app</button>`,
		`Other ways to install`,
		`Copy link`,
		`<span data-install-url></span>`,
		`beforeinstallprompt`,
		`navigator.standalone`,
		`"fullscreen"`,
		`"minimal-ui"`,
		`"window-controls-overlay"`,
		`appinstalled`,
	} {
		if !strings.Contains(home.body, want) {
			t.Errorf("home missing %q", want)
		}
	}
	if strings.Contains(home.body, `data-install-hint`) || strings.Contains(home.body, `data-install-dismiss`) {
		t.Error("home still includes the old install banner")
	}

	h := newTestApp(t).routes()
	_, partial := get(t, h, "/", map[string]string{"HX-Request": "true"})
	if strings.Contains(partial, `id="install-dialog"`) || strings.Contains(partial, `data-install-footer`) {
		t.Error("HTMX partial includes install chrome")
	}

	if login := ts.browser(t).get("/login"); !strings.Contains(login.body, `data-install-footer`) || strings.Count(login.body, `id="install-dialog"`) != 1 {
		t.Error("login missing the install dialog")
	}
	loggedIn := ts.browser(t)
	loggedIn.signup("install-dialog@example.com", "")
	if settings := loggedIn.get("/settings"); !strings.Contains(settings.body, `data-install-footer`) || strings.Count(settings.body, `id="install-dialog"`) != 1 {
		t.Error("settings missing the install dialog")
	}
	dash := ts.browser(t).signup("install-dash@example.com", "").body
	if !strings.Contains(dash, "Your trackers") || !strings.Contains(dash, `data-install-footer`) {
		t.Error("dashboard missing the install footer")
	}

	shareTS, _, tr, link := shareLink(t)
	shared := shareTS.browser(t).get(link)
	if shared.status != http.StatusOK {
		t.Fatalf("share page status %d", shared.status)
	}
	dialog := installDialogHTML(t, shared.body)
	if strings.Contains(dialog, *tr.ShareToken) || strings.Contains(dialog, link) {
		t.Error("install dialog includes the share token")
	}

	script := ts.browser(t).get("/static/app.js").body
	for _, want := range []string{
		"beforeinstallprompt",
		"appinstalled",
		"userChoice",
		"prompting",
		`new URL("/", location.origin)`,
		"navigator.standalone",
		`"fullscreen"`,
		`"minimal-ui"`,
		`"window-controls-overlay"`,
		"iPad|iPhone|iPod",
		"MacIntel",
		"Android",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("app.js missing %q", want)
		}
	}
	if strings.Contains(script, "install-dismissed") || strings.Contains(script, "localStorage.getItem") || strings.Contains(script, "localStorage.setItem") || strings.Contains(script, "location.href") {
		t.Error("app.js stores an install dismissal or copies the current URL")
	}
	css := ts.browser(t).get("/static/app.css").body
	for _, want := range []string{
		"display-mode: fullscreen",
		"display-mode: minimal-ui",
		"display-mode: window-controls-overlay",
		"prefers-reduced-motion: reduce",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("app.css missing %q", want)
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

func TestHeaderUsesAppIcon(t *testing.T) {
	h := newTestApp(t).routes()
	_, body := get(t, h, "/", nil)
	if !strings.Contains(body, `<img class="brand-mark" src="/static/icons/icon-192.png" alt="" width="22" height="22">`) {
		t.Error("header does not use the app icon")
	}
}
