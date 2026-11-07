// Package ratelimit is a small fixed-window counter, used to stop someone
// guessing passwords against /v1/auth/login.
//
// Counters live in memory, so each API instance limits on its own. That is
// deliberate: it needs no Redis and no database round trip on the login path,
// and with a handful of instances an attacker still only multiplies their
// budget by the instance count. Move it to Postgres or Redis if that stops
// being good enough.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter allows a fixed number of attempts per key within a window.
// A zero or negative limit disables limiting entirely.
type Limiter struct {
	limit  int
	window time.Duration

	// Now lets tests advance the clock instead of sleeping.
	Now func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	count int
	start time.Time
}

// pruneAbove is the number of tracked keys past which Allow clears out expired
// ones, so a stream of distinct emails or IPs can't grow the map for ever.
const pruneAbove = 4096

func New(limit int, window time.Duration) *Limiter {
	return &Limiter{limit: limit, window: window, Now: time.Now, buckets: map[string]*bucket{}}
}

// Allow records one attempt against key and reports whether it is within the
// limit. When it is not, it also returns how long until the window resets, for
// a Retry-After header or a message to the user.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	if l.limit <= 0 || l.window <= 0 {
		return true, 0
	}
	now := l.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.buckets) > pruneAbove {
		for k, b := range l.buckets {
			if now.Sub(b.start) >= l.window {
				delete(l.buckets, k)
			}
		}
	}

	b, ok := l.buckets[key]
	if !ok || now.Sub(b.start) >= l.window {
		l.buckets[key] = &bucket{count: 1, start: now}
		return true, 0
	}
	b.count++
	if b.count > l.limit {
		return false, l.window - now.Sub(b.start)
	}
	return true, 0
}
