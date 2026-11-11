package ratelimit

import (
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestLimiter(t *testing.T) {
	t.Parallel()
	c := &clock{t: time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)}
	l := New(3, time.Minute, c.now)

	for i := range 3 {
		if ok, _ := l.Allow("ada"); !ok {
			t.Fatalf("event %d refused, want the first 3 allowed", i+1)
		}
		c.t = c.t.Add(10 * time.Second)
	}
	ok, retry := l.Allow("ada")
	if ok || retry != 30*time.Second {
		t.Errorf("4th event at 0:30 = %v, retry after %v; want refused, 30s until the window ends", ok, retry)
	}
	if ok, _ := l.Allow("grace"); !ok {
		t.Error("another key was refused")
	}

	c.t = c.t.Add(30 * time.Second)
	if ok, _ := l.Allow("ada"); !ok {
		t.Error("refused once the window ended")
	}
}

func TestSweep(t *testing.T) {
	t.Parallel()
	c := &clock{t: time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)}
	l := New(1, time.Minute, c.now)
	for _, key := range []string{"a", "b", "c"} {
		l.Allow(key)
	}
	c.t = c.t.Add(2 * time.Minute)
	l.Allow("d")
	if len(l.windows) != 1 {
		t.Errorf("%d windows kept after the others ended, want 1", len(l.windows))
	}
}
