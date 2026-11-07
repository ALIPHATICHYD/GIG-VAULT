package ratelimit

import (
	"sync"
	"testing"
	"time"
)

// A fake clock, so these tests are instant and deterministic rather than
// sleeping for a minute.
type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

func newTestLimiter(limit int, window time.Duration) (*Limiter, *clock) {
	c := &clock{t: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)}
	l := New(limit, window)
	l.Now = c.now
	return l, c
}

func TestAllowsUpToTheLimitThenDenies(t *testing.T) {
	l, _ := newTestLimiter(5, time.Minute)

	for i := 1; i <= 5; i++ {
		ok, retry := l.Allow("email:tolu@example.com")
		if !ok {
			t.Fatalf("attempt %d was denied, want allowed", i)
		}
		if retry != 0 {
			t.Errorf("attempt %d returned retry-after %v, want 0", i, retry)
		}
	}

	ok, retry := l.Allow("email:tolu@example.com")
	if ok {
		t.Error("6th attempt was allowed, want denied")
	}
	if retry <= 0 || retry > time.Minute {
		t.Errorf("retry-after = %v, want between 0 and 1m", retry)
	}
}

func TestWindowResets(t *testing.T) {
	l, c := newTestLimiter(5, time.Minute)
	for i := 0; i < 5; i++ {
		l.Allow("ip:10.0.0.1")
	}
	if ok, _ := l.Allow("ip:10.0.0.1"); ok {
		t.Fatal("6th attempt allowed before the window reset")
	}

	// Still inside the window.
	c.add(59 * time.Second)
	if ok, _ := l.Allow("ip:10.0.0.1"); ok {
		t.Error("attempt at 59s was allowed; the window is a minute")
	}

	// Past it.
	c.add(2 * time.Second)
	if ok, _ := l.Allow("ip:10.0.0.1"); !ok {
		t.Error("attempt after the window was denied, want allowed")
	}
}

// One attacker hammering one account must not lock out everyone else.
func TestKeysAreIndependent(t *testing.T) {
	l, _ := newTestLimiter(5, time.Minute)
	for i := 0; i < 6; i++ {
		l.Allow("email:victim@example.com")
	}
	if ok, _ := l.Allow("email:victim@example.com"); ok {
		t.Fatal("the hammered key should be denied")
	}
	if ok, _ := l.Allow("email:someone-else@example.com"); !ok {
		t.Error("a different key was denied; keys must be counted separately")
	}
}

func TestRetryAfterShrinksAsTheWindowElapses(t *testing.T) {
	l, c := newTestLimiter(1, time.Minute)
	l.Allow("k")

	_, first := l.Allow("k")
	c.add(30 * time.Second)
	_, later := l.Allow("k")

	if later >= first {
		t.Errorf("retry-after did not shrink: %v then %v", first, later)
	}
	if later <= 0 {
		t.Errorf("retry-after = %v, want positive", later)
	}
}

func TestZeroLimitDisablesLimiting(t *testing.T) {
	for _, l := range []*Limiter{New(0, time.Minute), New(5, 0)} {
		for i := 0; i < 50; i++ {
			if ok, _ := l.Allow("k"); !ok {
				t.Fatalf("attempt %d denied; a zero limit or window must disable limiting", i)
			}
		}
	}
}

// The limiter is shared by every in-flight login, so it has to be safe to call
// from many goroutines. The count assertion catches a lost increment on its
// own; `go test -race` catches more, where cgo is available.
func TestConcurrentUseIsSafe(t *testing.T) {
	l := New(100, time.Minute)
	var wg sync.WaitGroup
	allowed := make([]bool, 200)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			allowed[i], _ = l.Allow("shared")
		}(i)
	}
	wg.Wait()

	n := 0
	for _, ok := range allowed {
		if ok {
			n++
		}
	}
	if n != 100 {
		t.Errorf("%d of 200 concurrent attempts allowed, want exactly 100", n)
	}
}

func TestExpiredKeysArePruned(t *testing.T) {
	l, c := newTestLimiter(5, time.Minute)
	for i := 0; i < pruneAbove+10; i++ {
		l.Allow(string(rune(i%1000)) + "-" + time.Duration(i).String())
	}
	before := len(l.buckets)
	if before <= pruneAbove {
		t.Skipf("only %d keys tracked, pruning not triggered", before)
	}

	// Everything is now stale, so the next call clears it out.
	c.add(2 * time.Minute)
	l.Allow("fresh")
	if len(l.buckets) >= before {
		t.Errorf("map held %d keys before and %d after; expired keys should be pruned", before, len(l.buckets))
	}
}
