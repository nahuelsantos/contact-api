package guard_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nahuelsantos/contact-api/internal/config"
	"github.com/nahuelsantos/contact-api/internal/guard"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newClock() *fakeClock { return &fakeClock{t: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)} }

var rate5per10m = config.Rate{Count: 5, Per: config.Duration(10 * time.Minute)}

func TestLimiter_AllowsCountPerWindow(t *testing.T) {
	clk := newClock()
	l := guard.NewLimiter(clk.Now)
	for i := range 5 {
		if ok, _ := l.Allow("k", rate5per10m); !ok {
			t.Fatalf("request %d refused", i+1)
		}
	}
	ok, retry := l.Allow("k", rate5per10m)
	if ok {
		t.Fatal("6th request allowed")
	}
	if retry <= 0 {
		t.Fatalf("retryAfter = %v, want > 0", retry)
	}
	// A refused request must not consume tokens: retry stays stable.
	_, retry2 := l.Allow("k", rate5per10m)
	if retry2 != retry {
		t.Fatalf("retryAfter drifted: %v then %v", retry, retry2)
	}
}

func TestLimiter_Refills(t *testing.T) {
	clk := newClock()
	l := guard.NewLimiter(clk.Now)
	for range 5 {
		l.Allow("k", rate5per10m)
	}
	clk.Advance(2*time.Minute - time.Second)
	if ok, _ := l.Allow("k", rate5per10m); ok {
		t.Fatal("allowed before per/count elapsed")
	}
	clk.Advance(time.Second)
	if ok, _ := l.Allow("k", rate5per10m); !ok {
		t.Fatal("one more should be allowed after per/count")
	}
	if ok, _ := l.Allow("k", rate5per10m); ok {
		t.Fatal("only one token should have refilled")
	}
}

func TestLimiter_KeysIndependent(t *testing.T) {
	l := guard.NewLimiter(newClock().Now)
	for range 5 {
		l.Allow("a", rate5per10m)
	}
	if ok, _ := l.Allow("a", rate5per10m); ok {
		t.Fatal("a should be limited")
	}
	if ok, _ := l.Allow("b", rate5per10m); !ok {
		t.Fatal("b should be unaffected")
	}
}

func TestLimiter_EvictsIdleKeys(t *testing.T) {
	clk := newClock()
	l := guard.NewLimiter(clk.Now)
	l.Allow("idle", rate5per10m)
	if l.Len() != 1 {
		t.Fatalf("Len = %d, want 1", l.Len())
	}
	clk.Advance(21 * time.Minute) // > 2*per
	l.Allow("other", rate5per10m) // triggers sweep
	if l.Len() != 1 {
		t.Fatalf("Len = %d after eviction, want 1 (only other)", l.Len())
	}
}

func TestLimiter_NonPositiveRateAlwaysAllows(t *testing.T) {
	l := guard.NewLimiter(newClock().Now)
	for _, r := range []config.Rate{{}, {Count: 0, Per: config.Duration(time.Minute)}, {Count: 5, Per: 0}, {Count: -1, Per: -1}} {
		for range 20 {
			if ok, retry := l.Allow("k", r); !ok || retry != 0 {
				t.Fatalf("rate %+v: ok=%v retry=%v", r, ok, retry)
			}
		}
	}
}

func TestLimiter_ConcurrentExactlyCount(t *testing.T) {
	l := guard.NewLimiter(newClock().Now)
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := l.Allow("k", rate5per10m); ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 5 {
		t.Fatalf("allowed = %d, want exactly 5", got)
	}
}
