package memjobs

import (
	"sync"
	"time"
)

// Clock tells a Queue the time and wakes it when a retry is due.
type Clock interface {
	Now() time.Time
	// After sends the time on the channel it returns once d has passed.
	After(d time.Duration) <-chan time.Time
}

type wallClock struct{}

func (wallClock) Now() time.Time                         { return time.Now() }
func (wallClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// FakeClock is a Clock that moves only when Advance moves it.
type FakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []fakeTimer
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

// NewFakeClock returns a FakeClock that reads now.
func NewFakeClock(now time.Time) *FakeClock {
	return &FakeClock{now: now}
}

// Now returns the clock's time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After returns a channel that receives once Advance has moved the clock d.
func (c *FakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.timers = append(c.timers, fakeTimer{at: c.now.Add(d), ch: ch})
	return ch
}

// Advance moves the clock d forward and fires the timers now due.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	kept := c.timers[:0]
	for _, t := range c.timers {
		if t.at.After(c.now) {
			kept = append(kept, t)
			continue
		}
		t.ch <- c.now
	}
	c.timers = kept
}
