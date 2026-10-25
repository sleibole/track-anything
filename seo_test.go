package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func prodApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp(t)
	a.cfg.Env = "prod"
	a.cfg.BaseURL = "https://trackanything.io"
	return a
}

func TestDevSiteIsNotIndexable(t *testing.T) {
	h := newTestApp(t).routes()
	code, body := get(t, h, "/", nil)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("robots %q", rec.Header().Get("X-Robots-Tag"))
	}
	if !strings.Contains(body, `<meta name="robots" content="noindex" data-page-meta>`) {
		t.Fatal("dev home missing robots meta")
	}
	if strings.Contains(body, `rel="canonical"`) || strings.Contains(body, `property="og:`) {
		t.Fatal("dev home published canonical or social tags")
	}
	if strings.Contains(body, "trackanything.io") {
		t.Fatal("dev home named the production host")
	}

	robotsCode, robots := get(t, h, "/robots.txt", nil)
	if robotsCode != http.StatusOK || robots != "User-agent: *\nDisallow: /\n" {
		t.Fatalf("robots.txt %d %q", robotsCode, robots)
	}
	if strings.Contains(robots, "Sitemap:") || strings.Contains(robots, "trackanything.io") {
		t.Fatalf("dev robots.txt %q", robots)
	}
	siteCode, site := get(t, h, "/sitemap.xml", nil)
	if siteCode != http.StatusOK || strings.Contains(site, "<loc>") || strings.Contains(site, "trackanything.io") || strings.Contains(site, "example.test") {
		t.Fatalf("sitemap %d %s", siteCode, site)
	}
	if rec.Header().Get("Vary") != "HX-Request, Cookie" {
		t.Fatalf("vary %q", rec.Header().Get("Vary"))
	}
}

func TestProductionHomeMetadata(t *testing.T) {
	h := prodApp(t).routes()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?utm=newsletter", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("X-Robots-Tag") != "" {
		t.Fatalf("robots %q", rec.Header().Get("X-Robots-Tag"))
	}
	if rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("Vary") != "HX-Request, Cookie" {
		t.Fatalf("cache %q vary %q", rec.Header().Get("Cache-Control"), rec.Header().Get("Vary"))
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<title>Track Anything</title>",
		`<link rel="canonical" href="https://trackanything.io/" data-page-meta>`,
		`<meta name="description" content="Track how often anything happens: dog meals, workouts, habits." data-page-meta>`,
		`<meta property="og:title" content="Track Anything" data-page-meta>`,
		`<meta property="og:url" content="https://trackanything.io/" data-page-meta>`,
		`<meta property="og:image" content="https://trackanything.io/static/icons/icon-512.png" data-page-meta>`,
		`<meta property="og:image:alt" content="Track Anything icon" data-page-meta>`,
		`<meta property="og:image:width" content="512" data-page-meta>`,
		`<meta name="twitter:card" content="summary" data-page-meta>`,
		`<html lang="en">`,
		`name="viewport"`,
		"<h1>Track Anything</h1>",
		`href="/signup"`,
		`href="/login"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(body, `name="robots"`) || strings.Contains(body, "utm=newsletter") || strings.Contains(body, `name="keywords"`) {
		t.Fatalf("home leaked robots, the query, or keywords:\n%s", body)
	}

	partial := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("HX-Request", "true")
	h.ServeHTTP(partial, req)
	frag := partial.Body.String()
	if partial.Code != http.StatusOK || strings.Contains(frag, "<!doctype") || strings.Contains(frag, "<html") {
		t.Fatalf("partial %d %s", partial.Code, frag)
	}
	if !strings.Contains(frag, "<title>Track Anything</title>") || !strings.Contains(frag, `<template data-page-meta>`) || !strings.Contains(frag, `rel="canonical" href="https://trackanything.io/"`) {
		t.Fatal("partial lost the title or the discovery tags")
	}
	if partial.Header().Get("X-Robots-Tag") != "" || partial.Header().Get("Vary") != "HX-Request, Cookie" {
		t.Fatalf("partial robots %q vary %q", partial.Header().Get("X-Robots-Tag"), partial.Header().Get("Vary"))
	}

	icon := httptest.NewRecorder()
	h.ServeHTTP(icon, httptest.NewRequest(http.MethodGet, "/static/icons/icon-512.png", nil))
	if icon.Code != http.StatusOK || icon.Header().Get("X-Robots-Tag") != "" {
		t.Fatalf("icon %d robots %q", icon.Code, icon.Header().Get("X-Robots-Tag"))
	}
}

func TestProductionPrivatePagesAreNoindex(t *testing.T) {
	ts, owner, _, link := shareLink(t)
	ts.app.cfg.Env = "prod"
	ts.app.cfg.BaseURL = "https://trackanything.io"

	home := owner.get("/")
	if home.status != http.StatusOK || !strings.Contains(home.body, "<title>Your trackers · Track Anything</title>") {
		t.Fatalf("dashboard %d %s", home.status, home.body)
	}
	if !strings.Contains(home.body, `<meta name="robots" content="noindex" data-page-meta>`) || strings.Contains(home.body, `rel="canonical"`) || strings.Contains(home.body, `property="og:`) {
		t.Fatal("logged-in home is indexable or has social tags")
	}

	anon := ts.browser(t)
	for _, path := range []string{"/login", "/signup", link} {
		res := anon.get(path)
		if res.status != http.StatusOK || !strings.Contains(res.body, `<meta name="robots" content="noindex" data-page-meta>`) || strings.Contains(res.body, `rel="canonical"`) || strings.Contains(res.body, `property="og:`) {
			t.Errorf("%s indexable: %d", path, res.status)
		}
	}

	shareReq, err := http.NewRequest(http.MethodGet, ts.srv.URL+link, nil)
	if err != nil {
		t.Fatal(err)
	}
	shareResp, err := anon.c.Do(shareReq)
	if err != nil {
		t.Fatal(err)
	}
	shareResp.Body.Close()
	if shareResp.Header.Get("X-Robots-Tag") != "noindex" || shareResp.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("share headers %v", shareResp.Header)
	}

	h := ts.app.routes()
	settings := httptest.NewRecorder()
	h.ServeHTTP(settings, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if settings.Code != http.StatusSeeOther || settings.Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("settings %d robots %q", settings.Code, settings.Header().Get("X-Robots-Tag"))
	}
	health := httptest.NewRecorder()
	h.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || health.Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("healthz %d %q", health.Code, health.Header().Get("X-Robots-Tag"))
	}
}

func TestProductionRobotsAndSitemap(t *testing.T) {
	h := prodApp(t).routes()
	code, robots := get(t, h, "/robots.txt", nil)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if robots != "User-agent: *\nAllow: /\n\nSitemap: https://trackanything.io/sitemap.xml\n" {
		t.Fatalf("robots.txt %q", robots)
	}
	for _, hidden := range []string{"/login", "/signup", "/settings", "/s/", "/trackers", "/households", "/join", "Disallow"} {
		if strings.Contains(robots, hidden) {
			t.Errorf("robots.txt lists %s", hidden)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/robots.txt", nil))
	if rec.Header().Get("X-Robots-Tag") != "" || rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("robots headers %v", rec.Header())
	}

	siteCode, site := get(t, h, "/sitemap.xml", nil)
	if siteCode != http.StatusOK {
		t.Fatalf("sitemap %d", siteCode)
	}
	if !strings.Contains(site, "<loc>https://trackanything.io/</loc>") || strings.Contains(site, "<lastmod>") {
		t.Fatalf("sitemap %s", site)
	}
	for _, hidden := range []string{"/login", "/signup", "/s/", "/charts", "/settings", "utm="} {
		if strings.Contains(site, hidden) {
			t.Errorf("sitemap lists %s", hidden)
		}
	}
	if strings.Count(site, "<loc>") != 1 {
		t.Fatalf("sitemap locs %s", site)
	}
}

func TestTrailingSlashAndUnknownPaths(t *testing.T) {
	h := newTestApp(t).routes()
	slash := httptest.NewRecorder()
	h.ServeHTTP(slash, httptest.NewRequest(http.MethodGet, "/login/?next=%2Fcharts", nil))
	if slash.Code != http.StatusMovedPermanently || slash.Header().Get("Location") != "/login?next=%2Fcharts" {
		t.Fatalf("login slash %d %q", slash.Code, slash.Header().Get("Location"))
	}
	if slash.Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("redirect robots %q", slash.Header().Get("X-Robots-Tag"))
	}

	missing := httptest.NewRecorder()
	h.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/nope/", nil))
	if missing.Code != http.StatusNotFound || missing.Header().Get("Location") != "" {
		t.Fatalf("unknown slash %d %q", missing.Code, missing.Header().Get("Location"))
	}
	if !strings.Contains(missing.Body.String(), "<!doctype html>") || !strings.Contains(missing.Body.String(), "<h1>Not found</h1>") || !strings.Contains(missing.Body.String(), `href="/"`) {
		t.Fatalf("404 body %s", missing.Body.String())
	}

	method := httptest.NewRecorder()
	h.ServeHTTP(method, httptest.NewRequest(http.MethodGet, "/households", nil))
	if method.Code != http.StatusMethodNotAllowed || !strings.Contains(method.Header().Get("Allow"), "POST") {
		t.Fatalf("households %d allow %q", method.Code, method.Header().Get("Allow"))
	}

	static := httptest.NewRecorder()
	h.ServeHTTP(static, httptest.NewRequest(http.MethodGet, "/static/", nil))
	if static.Code == http.StatusMovedPermanently {
		t.Fatalf("static subtree redirected to %q", static.Header().Get("Location"))
	}
}
