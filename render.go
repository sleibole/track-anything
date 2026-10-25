package main

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
)

// views holds one parsed template set per page: layout + partials + the page itself.
// Each page defines a "content" block that the layout renders.
type views struct {
	pages map[string]*template.Template
}

func loadViews(fsys fs.FS) (*views, error) {
	pageFiles, err := fs.Glob(fsys, "templates/*.html")
	if err != nil {
		return nil, err
	}
	partialFiles, err := fs.Glob(fsys, "templates/partials/*.html")
	if err != nil {
		return nil, err
	}

	v := &views{pages: map[string]*template.Template{}}
	for _, page := range pageFiles {
		name := path.Base(page)
		if name == "layout.html" {
			continue
		}
		files := append([]string{"templates/layout.html"}, partialFiles...)
		files = append(files, page)
		t, err := template.New(name).Funcs(templateFuncs).ParseFS(fsys, files...)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		v.pages[name] = t
	}
	return v, nil
}

// view is what every template receives: the logged-in user (nil when logged out)
// for the layout, and the page's own data.
type view struct {
	User             *User
	Data             any
	TurnstileSiteKey string
	SEO              seoTags
}

// seoTags is set only for the logged-out production home page.
// Other documents stay noindex and do not advertise a canonical URL.
type seoTags struct {
	Indexable   bool
	Title       string
	Canonical   string
	Description string
	Image       string
}

// render writes a full page, or a title plus the content block for HTMX requests.
// The title is lifted out of the fragment by HTMX. The content block is swapped
// into #content, so the header and the rest of the document stay.
func (a *app) render(w http.ResponseWriter, r *http.Request, status int, page string, data any) {
	block := "layout"
	if r.Header.Get("HX-Request") == "true" {
		block = "partial"
	}
	a.renderBlock(w, r, status, page, block, data)
}

// renderBlock writes one named template from a page's set, e.g. a partial for an HTMX swap.
func (a *app) renderBlock(w http.ResponseWriter, r *http.Request, status int, page, block string, data any) {
	t, ok := a.views.pages[page]
	if !ok {
		a.serverError(w, r, fmt.Errorf("unknown page %q", page))
		return
	}

	// Render to a buffer first so a template error doesn't send a half-written page.
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, block, view{
		User:             currentUser(r),
		Data:             data,
		TurnstileSiteKey: a.cfg.TurnstileSiteKey,
		SEO:              a.seoTags(r),
	}); err != nil {
		a.serverError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	// A full document and a fragment share a URL, and logged-in and logged-out
	// home share "/". private, no-store already refuses a shared cache; Vary
	// is the contract if one stores the response anyway.
	w.Header().Set("Vary", "HX-Request, Cookie")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (a *app) serverError(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.Error("server error", "method", r.Method, "path", logPath(r.URL.Path), "err", err)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}
