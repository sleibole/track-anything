package main

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"strings"
)

// publicTitle and publicDescription are the logged-out home page's own words.
// The description is the sentence under the heading, not a separate pitch.
const (
	publicTitle       = "Track Anything"
	publicDescription = "Track how often anything happens: dog meals, workouts, habits."
	publicIconPath    = "/static/icons/icon-512.png"
)

// indexable reports whether this response may appear in search results.
// The only such page is the logged-out home page in production.
// A query string does not make another public page: canonical always drops it.
// Share links are reachable without an account and stay private.
func (a *app) indexable(r *http.Request) bool {
	if a.cfg.Env != "prod" || currentUser(r) != nil {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	return r.URL.Path == "/"
}

func (a *app) absoluteURL(p string) string {
	return strings.TrimRight(a.cfg.BaseURL, "/") + p
}

func (a *app) seoTags(r *http.Request) seoTags {
	if !a.indexable(r) {
		return seoTags{}
	}
	return seoTags{
		Indexable:   true,
		Title:       publicTitle,
		Canonical:   a.absoluteURL("/"),
		Description: publicDescription,
		Image:       a.absoluteURL(publicIconPath),
	}
}

// robotsHeader marks responses noindex unless they are the production home
// page, or a production robots.txt or sitemap. Static files are left alone so
// a crawler can fetch the sharing icon. This is not access control.
func (a *app) robotsHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.robotsNoindex(r) {
			w.Header().Set("X-Robots-Tag", "noindex")
		}
		next.ServeHTTP(w, r)
	})
}

func (a *app) robotsNoindex(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/static/") {
		return false
	}
	if a.cfg.Env == "prod" && (r.URL.Path == "/robots.txt" || r.URL.Path == "/sitemap.xml") {
		return false
	}
	return !a.indexable(r)
}

func (a *app) handleRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if a.cfg.Env != "prod" {
		io.WriteString(w, "User-agent: *\nDisallow: /\n")
		return
	}
	io.WriteString(w, "User-agent: *\nAllow: /\n\nSitemap: "+a.absoluteURL("/sitemap.xml")+"\n")
}

func (a *app) handleSitemap(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	if a.cfg.Env == "prod" {
		buf.WriteString("<url><loc>")
		xml.EscapeText(&buf, []byte(a.absoluteURL("/")))
		buf.WriteString("</loc></url>\n")
	}
	buf.WriteString("</urlset>\n")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(buf.Bytes())
}

// dispatch serves mux, replaces its plain-text 404 with the message page,
// and permanently redirects a trailing slash on an exact route.
// It does not register a catch-all pattern, so a POST-only path still
// answers 405 and Go's own path cleaning still answers 307.
func (a *app) dispatch(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if dest, ok := trailingSlashTarget(mux, r); ok {
			http.Redirect(w, r, dest, http.StatusMovedPermanently)
			return
		}
		_, pattern := mux.Handler(r)
		if pattern == "" {
			probe := httptest.NewRecorder()
			mux.ServeHTTP(probe, r)
			if probe.Code == http.StatusNotFound {
				a.notFound(w, r)
				return
			}
			copyRecorded(w, probe)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// trailingSlashTarget is the path without its trailing slash when that path
// is its own route. A subtree such as /static/ is not redirected, or /static
// and /static/ would send the browser back and forth.
func trailingSlashTarget(mux *http.ServeMux, r *http.Request) (string, bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return "", false
	}
	p := r.URL.Path
	if len(p) <= 1 || !strings.HasSuffix(p, "/") {
		return "", false
	}
	trimmed := path.Clean(strings.TrimRight(p, "/"))
	if !strings.HasPrefix(trimmed, "/") || trimmed == p {
		return "", false
	}
	probe := r.Clone(r.Context())
	u := *r.URL
	u.Path = trimmed
	u.RawPath = ""
	probe.URL = &u
	_, pattern := mux.Handler(probe)
	if pattern == "" || strings.HasSuffix(pattern, "/") {
		return "", false
	}
	return (&url.URL{Path: trimmed, RawQuery: r.URL.RawQuery}).String(), true
}

func copyRecorded(w http.ResponseWriter, rec *httptest.ResponseRecorder) {
	if rec.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	for k, vs := range rec.Header() {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(rec.Code)
	w.Write(rec.Body.Bytes())
}
