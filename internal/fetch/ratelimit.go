package fetch

import (
	"context"
	"sync"
	"time"
)

// hostLimiter spaces requests to the same host at least interval apart.
type hostLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     map[string]time.Time // host -> earliest start of its next request
}

func newHostLimiter(interval time.Duration) *hostLimiter {
	return &hostLimiter{interval: interval, next: map[string]time.Time{}}
}

// wait blocks until a request to host may start, or ctx ends. Each call takes
// the next free slot, so concurrent callers queue up one interval apart.
func (l *hostLimiter) wait(ctx context.Context, host string) error {
	l.mu.Lock()
	now := time.Now()
	start := l.next[host]
	if start.Before(now) {
		start = now
	}
	l.next[host] = start.Add(l.interval)
	if len(l.next) > 1024 {
		for h, t := range l.next {
			if t.Before(now) {
				delete(l.next, h)
			}
		}
	}
	l.mu.Unlock()

	delay := start.Sub(now)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
