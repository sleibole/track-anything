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
	if !strings.Contains(body, `name="apple-mobile-web-app-capable"`) {
		t.Error("layout missing apple-mobile-web-app-capable")
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
