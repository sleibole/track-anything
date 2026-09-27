package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimiter allows up to limit events per key in each fixed window. In memory, so it
// resets on restart, which is fine for slowing down password guessing and email spam.
type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	hits   map[string]*windowCount
}

type windowCount struct {
	start time.Time
	count int
}

func newRateLimiter(limit int, window time.Duration, now func() time.Time) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, now: now, hits: map[string]*windowCount{}}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	t := l.now()
	if len(l.hits) > 10_000 {
		for k, w := range l.hits {
			if t.Sub(w.start) >= l.window {
				delete(l.hits, k)
			}
		}
	}

	w, ok := l.hits[key]
	if !ok || t.Sub(w.start) >= l.window {
		l.hits[key] = &windowCount{start: t, count: 1}
		return true
	}
	if w.count >= l.limit {
		return false
	}
	w.count++
	return true
}

// clientIP trusts the proxy headers because in production the app is only reachable
// through Cloudflare and the Fly.io ingress (see docs/development/ARCHITECTURE.md, Deployment).
func clientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		if ip := strings.TrimSpace(first); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
