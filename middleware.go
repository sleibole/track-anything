package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (a *app) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		a.logger.Info("request",
			"method", r.Method,
			"path", logPath(r.URL.Path),
			"status", rec.status,
			"duration", time.Since(start),
		)
	})
}

// logPath is the request path written to logs. Bearer tokens in the URL are
// replaced with the route placeholder so a log line cannot be used to log in,
// confirm an email, or open a share or invite link.
func logPath(path string) string {
	switch {
	case strings.HasPrefix(path, "/login/link/") && len(path) > len("/login/link/"):
		return "/login/link/{token}"
	case strings.HasPrefix(path, "/verify/") && path != "/verify/done" && !strings.HasPrefix(path, "/verify/done/"):
		return "/verify/{token}"
	case strings.HasPrefix(path, "/join/") && len(path) > len("/join/"):
		return "/join/{token}"
	case strings.HasPrefix(path, "/s/"):
		rest := strings.TrimPrefix(path, "/s/")
		if rest == "" {
			return path
		}
		_, suffix, ok := strings.Cut(rest, "/")
		if !ok {
			return "/s/{token}"
		}
		return "/s/{token}/" + suffix
	default:
		return path
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func (a *app) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				w.Header().Set("Connection", "close")
				a.serverError(w, r, fmt.Errorf("panic: %v", err))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
