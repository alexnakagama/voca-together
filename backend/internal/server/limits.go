package server

import (
	"log/slog"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"vocatogether/backend/internal/ratelimit"
)

// maxTrackedIPs caps each per-IP limiter's memory (about 100 bytes per key).
const maxTrackedIPs = 100_000

// IPLimits are the per-client-IP limiters of the public auth routes
// (decision 018). A nil limiter allows everything, so the zero value
// disables limiting (tests).
type IPLimits struct {
	Login    *ratelimit.Limiter[netip.Prefix] // login
	Register *ratelimit.Limiter[netip.Prefix] // register
	Email    *ratelimit.Limiter[netip.Prefix] // resend-verification, forgot-password
	Token    *ratelimit.Limiter[netip.Prefix] // verify-email, reset-password (JSON and form)
	Refresh  *ratelimit.Limiter[netip.Prefix] // refresh
}

// NewIPLimits returns the production per-IP limits. They are generous enough
// for many users behind one NAT (a household, a classroom, a carrier) and
// bound what a single source can make the server do: argon2 work, outbound
// email and database load.
func NewIPLimits(logger *slog.Logger) IPLimits {
	limiter := func(name string, burst int, every time.Duration) *ratelimit.Limiter[netip.Prefix] {
		return ratelimit.New[netip.Prefix](name, burst, every, maxTrackedIPs, logger)
	}
	return IPLimits{
		Login:    limiter("ip_login", 20, 3*time.Second),  // 20/min
		Register: limiter("ip_register", 10, time.Minute), // 60/h
		Email:    limiter("ip_email", 5, 2*time.Minute),   // 30/h
		Token:    limiter("ip_token", 10, 6*time.Second),  // 10/min
		Refresh:  limiter("ip_refresh", 60, time.Second),  // 60/min
	}
}

// limitByIP returns middleware that answers with reject once the client's
// bucket in l is empty. It runs before the body is read, so malformed
// requests cost a token too.
func limitByIP(l *ratelimit.Limiter[netip.Prefix], hops int,
	reject func(http.ResponseWriter, time.Duration)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ok, retryAfter := l.Allow(clientKey(r, hops)); !ok {
				reject(w, retryAfter)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// setRetryAfter sets Retry-After in whole seconds, rounded up and at least 1,
// so a client that waits as told is never refused again for being early.
func setRetryAfter(w http.ResponseWriter, d time.Duration) {
	secs := max(1, int64(math.Ceil(d.Seconds())))
	w.Header().Set("Retry-After", strconv.FormatInt(secs, 10))
}
