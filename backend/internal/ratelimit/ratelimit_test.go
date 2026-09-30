package ratelimit

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a manually advanced clock, safe for concurrent reads.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTestLimiter(burst int, every time.Duration, maxKeys int) (*Limiter[string], *fakeClock, *bytes.Buffer) {
	logs := &bytes.Buffer{}
	l := New[string]("test", burst, every, maxKeys, slog.New(slog.NewTextHandler(logs, nil)))
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	l.now = clock.now
	l.lastSweep = clock.now()
	return l, clock, logs
}

func TestAllowsBurstThenDenies(t *testing.T) {
	l, _, _ := newTestLimiter(3, time.Minute, 100)
	for i := range 3 {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d denied within burst", i+1)
		}
	}
	ok, retry := l.Allow("a")
	if ok {
		t.Fatal("request after burst allowed")
	}
	if retry != time.Minute {
		t.Errorf("retryAfter = %v, want 1m", retry)
	}
}

func TestRefillsOverTime(t *testing.T) {
	l, clock, _ := newTestLimiter(2, 10*time.Second, 100)
	l.Allow("a")
	l.Allow("a")

	clock.advance(4 * time.Second)
	ok, retry := l.Allow("a")
	if ok {
		t.Fatal("allowed before a token refilled")
	}
	if retry != 6*time.Second {
		t.Errorf("retryAfter = %v, want 6s", retry)
	}

	clock.advance(6 * time.Second)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("denied after a token refilled")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("only one token should have refilled")
	}

	// Refill never exceeds the burst, however long the key was idle.
	clock.advance(time.Hour)
	for i := range 2 {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d denied after a full refill", i+1)
		}
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("refill exceeded the burst")
	}
}

func TestDeniedRequestsDoNotConsume(t *testing.T) {
	l, clock, _ := newTestLimiter(1, 10*time.Second, 100)
	l.Allow("a")
	for range 5 {
		l.Allow("a")
	}
	clock.advance(10 * time.Second)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("denied requests delayed the refill")
	}
}

func TestClockGoingBackwardsDoesNotAddTokens(t *testing.T) {
	l, clock, _ := newTestLimiter(1, 10*time.Second, 100)
	l.Allow("a")
	clock.advance(-time.Hour)
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a backwards clock step refilled the bucket")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	l, _, _ := newTestLimiter(1, time.Minute, 100)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("a denied")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("b denied after a used its token")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a allowed twice")
	}
}

func TestSweepRemovesOnlyFullyRefilledKeys(t *testing.T) {
	l, clock, _ := newTestLimiter(2, time.Minute, 100)
	l.Allow("old")
	clock.advance(90 * time.Second) // "old" is full again after 60s
	l.Allow("recent")
	l.Allow("recent")

	clock.advance(sweepInterval)
	l.Allow("trigger")

	l.mu.Lock()
	_, hasOld := l.buckets["old"]
	_, hasRecent := l.buckets["recent"]
	l.mu.Unlock()
	if hasOld {
		t.Error("fully refilled key was not swept")
	}
	if !hasRecent {
		t.Fatal("partially used key was swept")
	}
	// The kept key still has its state: one token refilled in 60s, not two.
	l.Allow("recent")
	if ok, _ := l.Allow("recent"); ok {
		t.Error("sweep reset a partially used key")
	}
}

func TestKeyCapAllowsUntrackedAndWarnsOnce(t *testing.T) {
	l, clock, logs := newTestLimiter(1, time.Hour, 2)
	l.Allow("a")
	l.Allow("b")

	// At the cap: new keys pass without being tracked (limiters degrade
	// open), tracked keys stay limited.
	for range 3 {
		if ok, _ := l.Allow("c"); !ok {
			t.Fatal("new key at the cap was denied")
		}
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("tracked key lost its limit at the cap")
	}
	l.mu.Lock()
	n := len(l.buckets)
	l.mu.Unlock()
	if n != 2 {
		t.Fatalf("tracked keys = %d, want 2", n)
	}

	clock.advance(sweepInterval)
	l.Allow("a")
	out := logs.String()
	if strings.Count(out, "ratelimit: key cap reached") != 1 {
		t.Errorf("want one cap warning, logs:\n%s", out)
	}
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "untracked=3") {
		t.Errorf("cap warning lacks level or count:\n%s", out)
	}
}

func TestRejectionsAreLoggedAggregatedWithoutKeys(t *testing.T) {
	l, clock, logs := newTestLimiter(1, time.Hour, 100)
	l.Allow("secret-key@example.com")
	for range 50 {
		l.Allow("secret-key@example.com")
	}
	if logs.Len() != 0 {
		t.Fatalf("logged before the report interval:\n%s", logs)
	}
	clock.advance(sweepInterval)
	l.Allow("other")

	out := logs.String()
	if strings.Count(out, "ratelimit: rejected") != 1 {
		t.Fatalf("want one aggregated line, logs:\n%s", out)
	}
	if !strings.Contains(out, "limiter=test") || !strings.Contains(out, "count=50") {
		t.Errorf("report lacks limiter or count:\n%s", out)
	}
	if strings.Contains(out, "secret-key") {
		t.Errorf("key leaked into logs:\n%s", out)
	}

	// Counters reset after each report: a quiet interval logs nothing.
	logs.Reset()
	clock.advance(sweepInterval)
	l.Allow("other2")
	if logs.Len() != 0 {
		t.Errorf("quiet interval logged:\n%s", logs)
	}
}

func TestNilLimiterAllowsEverything(t *testing.T) {
	var l *Limiter[string]
	for range 100 {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatal("nil limiter denied")
		}
	}
}

func TestConcurrentAllowOnOneKeyGrantsExactlyBurst(t *testing.T) {
	l, _, _ := newTestLimiter(10, time.Hour, 100)
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if ok, _ := l.Allow("a"); ok {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	if got := allowed.Load(); got != 10 {
		t.Fatalf("allowed = %d, want 10", got)
	}
}

func TestConcurrentMixedKeys(t *testing.T) {
	l, clock, _ := newTestLimiter(2, time.Second, 50)
	var wg sync.WaitGroup
	for g := range 20 {
		wg.Go(func() {
			for i := range 200 {
				l.Allow(string(rune('a' + (g+i)%60)))
				if i%50 == 0 {
					clock.advance(sweepInterval)
				}
			}
		})
	}
	wg.Wait()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buckets) > 50 {
		t.Fatalf("tracked %d keys, cap is 50", len(l.buckets))
	}
}
