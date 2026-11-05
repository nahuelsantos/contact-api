package guard

import (
	"sync"
	"time"

	"github.com/nahuelsantos/contact-api/internal/config"
	"golang.org/x/time/rate"
)

const sweepInterval = time.Minute

type bucket struct {
	lim      *rate.Limiter
	rate     config.Rate
	lastSeen time.Time
}

// Limiter is a keyed token-bucket rate limiter, safe for concurrent use.
type Limiter struct {
	now       func() time.Time
	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

// NewLimiter returns a Limiter reading time from now.
func NewLimiter(now func() time.Time) *Limiter {
	return &Limiter{now: now, buckets: map[string]*bucket{}, lastSweep: now()}
}

// Allow consumes one token for key under rate r. When refused it returns how
// long to wait for the next token; a refusal consumes nothing. A rate with a
// non-positive Count or Per never limits.
func (l *Limiter) Allow(key string, r config.Rate) (bool, time.Duration) {
	per := time.Duration(r.Per)
	if r.Count <= 0 || per <= 0 {
		return true, 0
	}
	interval := per / time.Duration(r.Count)
	if interval <= 0 {
		return true, 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweep(now)

	b, ok := l.buckets[key]
	if !ok || b.rate != r {
		b = &bucket{lim: rate.NewLimiter(rate.Every(interval), r.Count), rate: r}
		l.buckets[key] = b
	}
	b.lastSeen = now

	res := b.lim.ReserveN(now, 1)
	if !res.OK() {
		return false, per
	}
	if delay := res.DelayFrom(now); delay > 0 {
		res.CancelAt(now)
		return false, delay
	}
	return true, 0
}

// Len returns the number of tracked keys.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// sweep drops keys idle for more than twice their window, at most once per
// sweepInterval. The caller holds l.mu.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < sweepInterval {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if now.Sub(b.lastSeen) > 2*time.Duration(b.rate.Per) {
			delete(l.buckets, k)
		}
	}
}
