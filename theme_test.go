package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestBrandThemeStylesheet(t *testing.T) {
	h := newTestApp(t).routes()
	_, body := get(t, h, "/", nil)
	pico := strings.Index(body, `/static/pico.min.css`)
	theme := strings.Index(body, `/static/theme.css`)
	app := strings.Index(body, `/static/app.css`)
	if pico < 0 || theme < 0 || app < 0 || pico > theme || theme > app {
		t.Fatalf("stylesheet order pico=%d theme=%d app=%d", pico, theme, app)
	}

	code, css := get(t, h, "/static/theme.css", nil)
	if code != http.StatusOK {
		t.Fatalf("theme.css status %d", code)
	}
	lower := strings.ToLower(css)
	for _, want := range []string{
		"--pico-primary",
		"#0f766e",
		"#115e59",
		"#fafaf7",
		"#1e293b",
		"#5eead4",
		"prefers-color-scheme: dark",
		`[data-theme="dark"]`,
	} {
		if !strings.Contains(lower, strings.ToLower(want)) {
			t.Errorf("theme.css missing %s", want)
		}
	}
	if strings.Contains(lower, "#0172ad") {
		t.Error("theme.css still uses Pico blue")
	}
}
