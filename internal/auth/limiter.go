package auth

import (
	"sync"
	"time"
)

// limiter backs off login attempts: per client, 5 free failures, then a wait that
// doubles from 30 s up to 15 minutes; globally at most 30 failures per minute.
// Memory only, keyed by client address, never logged.
type limiter struct {
	mu          sync.Mutex
	now         func() time.Time
	clients     map[string]*clientState
	windowStart time.Time
	windowFails int
}

type clientState struct {
	failures int
	until    time.Time
	last     time.Time
}

const (
	freeFailures   = 5
	firstBackoff   = 30 * time.Second
	maxBackoff     = 15 * time.Minute
	globalPerMin   = 30
	forgetAfter    = time.Hour
	maxClientState = 10_000
)

func newLimiter(now func() time.Time) *limiter {
	return &limiter{now: now, clients: map[string]*clientState{}}
}

// allow reports whether the client may try now, and how long to wait if not.
func (l *limiter) allow(client string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.windowStart) >= time.Minute {
		l.windowStart, l.windowFails = now, 0
	}
	if l.windowFails >= globalPerMin {
		return false, l.windowStart.Add(time.Minute).Sub(now)
	}
	if c := l.clients[client]; c != nil && now.Before(c.until) {
		return false, c.until.Sub(now)
	}
	return true, 0
}

func (l *limiter) fail(client string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.windowFails++
	c := l.clients[client]
	if c == nil {
		if len(l.clients) >= maxClientState {
			l.gc(now)
		}
		c = &clientState{}
		l.clients[client] = c
	}
	c.failures++
	c.last = now
	if c.failures >= freeFailures {
		wait := firstBackoff << (c.failures - freeFailures)
		if wait > maxBackoff || wait <= 0 {
			wait = maxBackoff
		}
		c.until = now.Add(wait)
	}
}

func (l *limiter) succeed(client string) {
	l.mu.Lock()
	delete(l.clients, client)
	l.mu.Unlock()
}

func (l *limiter) gc(now time.Time) {
	for k, c := range l.clients {
		if now.Sub(c.last) > forgetAfter && now.After(c.until) {
			delete(l.clients, k)
		}
	}
}
