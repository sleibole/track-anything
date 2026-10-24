package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxRequestBody is the largest body a handler may see. Current forms,
// including a long reorder list, fit under this. There is no file upload.
const maxRequestBody = 256 << 10

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

func (a *app) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("Referrer-Policy", "same-origin")
		// 180 days. The edge still has to redirect HTTP to HTTPS.
		// No subdomains and no preload: this host only.
		if a.cfg.Env == "prod" {
			h.Set("Strict-Transport-Security", "max-age=15552000")
		}
		next.ServeHTTP(w, r)
	})
}

// limitBody rejects a body larger than maxRequestBody before the handler
// runs. It does not parse the body. A handler that ignores the form, such
// as quick-log, must not see an oversized request at all.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if r.ContentLength > maxRequestBody {
			w.Header().Set("Connection", "close")
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				w.Header().Set("Connection", "close")
				http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
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
