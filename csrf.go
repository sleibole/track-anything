package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"html/template"
	"net/http"
	"reflect"
	"strings"
)

const (
	csrfCookie    = "csrf"
	csrfFieldName = "csrf_token"
	csrfHeader    = "X-CSRF-Token"
	csrfTokenLen  = 43 // base64.RawURLEncoding of 32 bytes
)

// csrfProtect rejects state-changing requests that do not present the CSRF
// secret for this browser context. Safe methods are always allowed.
// CrossOriginProtection runs outside this middleware and is a separate check.
func (a *app) csrfProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skipCSRF(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		token, err := a.currentCSRF(w, r)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if csrfFrom(r) == "" {
			r = r.WithContext(context.WithValue(r.Context(), csrfKey, token))
		}
		if safeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if token == "" || !requestHasCSRF(r, token) {
			a.rejectCSRF(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func skipCSRF(path string) bool {
	return strings.HasPrefix(path, "/static/") || path == "/healthz"
}

func safeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// currentCSRF is the secret this request must present.
// A signed-in session, including a guest, uses the secret stored on that session.
// Everyone else uses the anonymous csrf cookie. A missing cookie is issued on a
// safe request. An unsafe request cannot mint a secret for itself.
func (a *app) currentCSRF(w http.ResponseWriter, r *http.Request) (string, error) {
	if secret := csrfFrom(r); secret != "" {
		return secret, nil
	}
	if c, err := r.Cookie(csrfCookie); err == nil && validCSRFToken(c.Value) {
		return c.Value, nil
	}
	if !safeMethod(r.Method) {
		return "", nil
	}
	token, err := newToken()
	if err != nil {
		return "", err
	}
	a.setCSRFCookie(w, token)
	return token, nil
}

func (a *app) setCSRFCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionLifetime.Seconds()),
		Expires:  a.now().Add(sessionLifetime),
		HttpOnly: true,
		Secure:   a.cfg.Env != "dev",
		SameSite: http.SameSiteLaxMode,
	})
}

func validCSRFToken(token string) bool {
	if len(token) != csrfTokenLen {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(raw) == 32
}

func csrfFrom(r *http.Request) string {
	s, _ := r.Context().Value(csrfKey).(string)
	return s
}

// requestHasCSRF reports whether the header or the form field matches expected.
// Either one is enough: HTMX sends the header, and a form without JavaScript
// sends the field. A cross-site form cannot set the header.
func requestHasCSRF(r *http.Request, expected string) bool {
	if tokensEqual(r.Header.Get(csrfHeader), expected) {
		return true
	}
	if err := r.ParseForm(); err != nil {
		return false
	}
	return tokensEqual(r.PostFormValue(csrfFieldName), expected)
}

// tokensEqual compares SHA-256 digests so the check does not leak the secret
// through the length of the presented value.
func tokensEqual(got, want string) bool {
	if got == "" || want == "" || len(got) > 256 || len(want) > 256 {
		return false
	}
	sumGot := sha256.Sum256([]byte(got))
	sumWant := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(sumGot[:], sumWant[:]) == 1
}

func (a *app) rejectCSRF(w http.ResponseWriter, r *http.Request) {
	a.logger.Warn("csrf rejected", "method", r.Method, "path", logPath(r.URL.Path))
	http.Error(w, "Forbidden", http.StatusForbidden)
}

// csrfField renders the hidden input. v is the page view, a tracker card, an
// entry, or the token string itself. The markup is fixed; only the escaped
// token is inserted.
func csrfField(v any) template.HTML {
	return template.HTML(`<input type="hidden" name="` + csrfFieldName + `" value="` + template.HTMLEscapeString(csrfTokenValue(v)) + `">`)
}

func csrfTokenValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	rv := reflect.ValueOf(v)
	rv, ok := deref(rv)
	if !ok || rv.Kind() != reflect.Struct {
		return ""
	}
	f := rv.FieldByName("CSRFToken")
	if f.IsValid() && f.Kind() == reflect.String {
		return f.String()
	}
	return ""
}

// stampCSRF copies token onto every CSRFToken field in data so a nested
// template can render the field without each handler passing the secret.
func stampCSRF(data any, token string) any {
	if data == nil || token == "" {
		return data
	}
	v := reflect.ValueOf(data)
	if !v.IsValid() {
		return data
	}
	if v.Kind() != reflect.Pointer {
		ptr := reflect.New(v.Type())
		ptr.Elem().Set(v)
		stampCSRFValue(ptr, token, 0)
		return ptr.Elem().Interface()
	}
	stampCSRFValue(v, token, 0)
	return data
}

func stampCSRFValue(v reflect.Value, token string, depth int) {
	if depth > 12 {
		return
	}
	v, ok := deref(v)
	if !ok {
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		// Named types from other packages, such as time.Time, have nothing to stamp.
		if !appStruct(t) {
			return
		}
		if f := v.FieldByName("CSRFToken"); f.IsValid() && f.CanSet() && f.Kind() == reflect.String {
			f.SetString(token)
		}
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).PkgPath != "" {
				continue
			}
			stampCSRFValue(v.Field(i), token, depth+1)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			stampCSRFValue(v.Index(i), token, depth+1)
		}
	}
}

func appStruct(t reflect.Type) bool {
	if t.Name() == "" {
		return true
	}
	path := t.PkgPath()
	return path == "" || path == reflect.TypeOf(Session{}).PkgPath()
}

func deref(v reflect.Value) (reflect.Value, bool) {
	for range 8 {
		if !v.IsValid() {
			return v, false
		}
		switch v.Kind() {
		case reflect.Interface, reflect.Pointer:
			if v.IsNil() {
				return v, false
			}
			v = v.Elem()
		default:
			return v, true
		}
	}
	return v, false
}
