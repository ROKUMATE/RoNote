package memjobs

import (
	"sync"
	"time"
)

// Clock tells a Queue the time and wakes it when a job is due.
type Clock interface {
	Now() time.Time
	// At sends the time on the channel it returns once the clock reads t.
	// It takes a time, not a duration: a duration measured from an earlier
	// reading would be late by however far the clock moved since.
	At(t time.Time) <-chan time.Time
}

type wallClock struct{}

func (wallClock) Now() time.Time                  { return time.Now() }
func (wallClock) At(t time.Time) <-chan time.Time { return time.After(time.Until(t)) }

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

// At returns a channel that receives once Advance has moved the clock to t.
func (c *FakeClock) At(t time.Time) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if !t.After(c.now) {
		ch <- c.now
		return ch
	}
	c.timers = append(c.timers, fakeTimer{at: t, ch: ch})
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
