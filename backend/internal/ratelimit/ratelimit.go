// Package ratelimit provides an in-process, per-key token bucket limiter for
// abuse protection (decision 018). It holds state in memory only, so limits
// apply per process: with several instances each one enforces its own.
package ratelimit

import (
	"log/slog"
	"sync"
	"time"
)

// sweepInterval is how often fully refilled keys are dropped and rejection
// counts are logged. Both happen lazily, inside Allow, so a Limiter needs no
// goroutine and nothing to stop on shutdown.
const sweepInterval = time.Minute

// Limiter allows each key burst requests at once, then one more per every.
// It is safe for concurrent use. A nil *Limiter allows everything.
//
// At most maxKeys keys are tracked. A new key arriving at the cap is allowed
// without being tracked, and the event is logged: limiters degrade open,
// because failing closed would let anyone who can mint keys (attacker-chosen
// emails, IPv6 prefixes) lock everyone out. The hard resource bounds
// elsewhere (argon2 slots, DB pool, request deadline) keep the process safe.
//
// Keys are never logged; logs carry only the limiter's name and counts.
type Limiter[K comparable] struct {
	name    string
	burst   float64
	every   time.Duration
	maxKeys int
	logger  *slog.Logger
	now     func() time.Time

	mu        sync.Mutex
	buckets   map[K]bucket
	lastSweep time.Time
	rejected  int // since the last report
	untracked int // since the last report
}

type bucket struct {
	tokens float64
	last   time.Time
}

// New returns a Limiter. name identifies it in logs; burst and every must be
// positive.
func New[K comparable](name string, burst int, every time.Duration, maxKeys int, logger *slog.Logger) *Limiter[K] {
	if burst < 1 || every <= 0 || maxKeys < 1 {
		panic("ratelimit: burst, every and maxKeys must be positive")
	}
	return &Limiter[K]{
		name:      name,
		burst:     float64(burst),
		every:     every,
		maxKeys:   maxKeys,
		logger:    logger,
		now:       time.Now,
		buckets:   make(map[K]bucket),
		lastSweep: time.Now(),
	}
}

// Allow takes a token from key's bucket. If none is left it returns false and
// how long until one will be; denied requests take nothing.
func (l *Limiter[K]) Allow(key K) (ok bool, retryAfter time.Duration) {
	if l == nil {
		return true, 0
	}
	l.mu.Lock()
	now := l.now()
	report := l.sweepIfDue(now)
	ok, retryAfter = l.take(key, now)
	l.mu.Unlock()

	report.log(l.logger, l.name)
	return ok, retryAfter
}

func (l *Limiter[K]) take(key K, now time.Time) (bool, time.Duration) {
	b, tracked := l.buckets[key]
	if !tracked {
		if len(l.buckets) >= l.maxKeys {
			l.untracked++
			return true, 0
		}
		b = bucket{tokens: l.burst, last: now}
	} else {
		b = l.refill(b, now)
	}

	if b.tokens >= 1 {
		b.tokens--
		l.buckets[key] = b
		return true, 0
	}
	l.buckets[key] = b
	l.rejected++
	return false, time.Duration((1 - b.tokens) * float64(l.every))
}

// refill adds the tokens earned since b.last, up to burst. A clock that went
// backwards adds nothing.
func (l *Limiter[K]) refill(b bucket, now time.Time) bucket {
	if elapsed := now.Sub(b.last); elapsed > 0 {
		b.tokens = min(l.burst, b.tokens+float64(elapsed)/float64(l.every))
		b.last = now
	}
	return b
}

// sweepIfDue drops keys whose bucket is full again, which is lossless (a full
// bucket behaves exactly like a missing one), and collects the counts to
// report. It runs at most once per sweepInterval.
func (l *Limiter[K]) sweepIfDue(now time.Time) report {
	if now.Sub(l.lastSweep) < sweepInterval {
		return report{}
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if l.refill(b, now).tokens >= l.burst {
			delete(l.buckets, k)
		}
	}
	r := report{rejected: l.rejected, untracked: l.untracked, keys: len(l.buckets)}
	l.rejected, l.untracked = 0, 0
	return r
}

// report is what one sweep logs, outside the lock.
type report struct {
	rejected, untracked, keys int
}

func (r report) log(logger *slog.Logger, name string) {
	if logger == nil {
		return
	}
	if r.rejected > 0 {
		logger.Info("ratelimit: rejected", "limiter", name, "count", r.rejected)
	}
	if r.untracked > 0 {
		logger.Warn("ratelimit: key cap reached, new keys not limited",
			"limiter", name, "untracked", r.untracked, "keys", r.keys)
	}
}
