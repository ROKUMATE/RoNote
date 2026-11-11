// Package ratelimit counts events per key in fixed windows, in memory: a
// limit holds per process (docs/adr/0010-sign-in-and-sessions.md).
package ratelimit

import (
	"sync"
	"time"
)

// Limiter allows each key at most limit events per window. A key's window
// starts at its first event and the count starts over once it ends.
type Limiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	windows map[string]*window
	swept   time.Time
}

type window struct {
	start time.Time
	n     int
}

// New returns a Limiter reading the time from now, normally time.Now.
func New(limit int, every time.Duration, now func() time.Time) *Limiter {
	return &Limiter{limit: limit, window: every, now: now, windows: map[string]*window{}, swept: now()}
}

// Allow counts one event for key and reports whether it is within the
// limit; when it isn't, retryAfter is how long until the key's window ends.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	w, found := l.windows[key]
	if !found || now.Sub(w.start) >= l.window {
		w = &window{start: now}
		l.windows[key] = w
	}
	if w.n >= l.limit {
		return false, w.start.Add(l.window).Sub(now)
	}
	w.n++
	return true, 0
}

// sweep drops ended windows, once per window, so keys seen once don't stay.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.swept) < l.window {
		return
	}
	for key, w := range l.windows {
		if now.Sub(w.start) >= l.window {
			delete(l.windows, key)
		}
	}
	l.swept = now
}
