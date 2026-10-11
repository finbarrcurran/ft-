package oauth

import (
	"sync"
	"time"
)

// Limiter is a small in-memory sliding-window failure counter. Failures are
// recorded with Fail; Allow reports whether the key is still under the limit
// (and how long until it is). It never locks a key permanently.
type Limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
	Now    func() time.Time
}

// NewLimiter allows max failures per key within window.
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, hits: map[string][]time.Time{}}
}

func (l *Limiter) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *Limiter) prune(key string, now time.Time) []time.Time {
	h := l.hits[key]
	cut := now.Add(-l.window)
	i := 0
	for i < len(h) && h[i].Before(cut) {
		i++
	}
	h = h[i:]
	if len(h) == 0 {
		delete(l.hits, key)
	} else {
		l.hits[key] = h
	}
	return h
}

// Allow is true while the key has fewer than max failures in the window.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	h := l.prune(key, now)
	if len(h) < l.max {
		return true, 0
	}
	return false, h[0].Add(l.window).Sub(now)
}

// Fail records one failure and reports whether it just tripped the limit.
func (l *Limiter) Fail(key string) (tripped bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	h := l.prune(key, now)
	l.hits[key] = append(h, now)
	return len(h)+1 == l.max
}

// Reset forgets a key (after a successful login).
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	delete(l.hits, key)
	l.mu.Unlock()
}
