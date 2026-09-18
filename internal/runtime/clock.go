package runtime

import (
	"sync"
	"time"
)

type Clock interface{ Now() time.Time }

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func NewFakeClock(now time.Time) *FakeClock { return &FakeClock{now: now.UTC()} }

func (c *FakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

func (c *FakeClock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}
