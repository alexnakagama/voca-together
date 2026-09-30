package auth

import (
	"crypto/sha256"
	"log/slog"
	"time"

	"vocatogether/backend/internal/ratelimit"
)

// Per-account limits (decision 018), keyed by the normalized email address
// whether or not it has an account, so a limit is reached at the same attempt
// for every address and a 429 reveals nothing. Each check runs after input
// validation and before any database or argon2 work.
const (
	// loginBurst logins at once, then one per loginEvery: stops guessing one
	// account's password from many IPs; a real user never needs 10 quick tries.
	loginBurst = 10
	loginEvery = time.Minute
	// mailBurst emails at once, then one per mailEvery, shared by every flow
	// that mails the address (register, resend verification, forgot
	// password): at most about 29 emails a day reach any mailbox from us.
	mailBurst = 5
	mailEvery = time.Hour

	// maxTrackedAccounts caps each limiter's memory (about 100 bytes per key).
	maxTrackedAccounts = 100_000
)

// AccountLimits are the per-account limiters. Keys are SHA-256 hashes of the
// normalized address: fixed-size, and no addresses are kept in memory. A nil
// limiter allows everything, so the zero value disables limiting (tests).
type AccountLimits struct {
	Login *ratelimit.Limiter[[32]byte]
	Mail  *ratelimit.Limiter[[32]byte]
}

// NewAccountLimits returns the production per-account limits.
func NewAccountLimits(logger *slog.Logger) AccountLimits {
	return AccountLimits{
		Login: ratelimit.New[[32]byte]("account_login", loginBurst, loginEvery, maxTrackedAccounts, logger),
		Mail:  ratelimit.New[[32]byte]("account_mail", mailBurst, mailEvery, maxTrackedAccounts, logger),
	}
}

// allowAccount takes a token for the normalized address addr from l, or
// returns a *RateLimitedError. Every attempt counts, whatever its outcome
// later, so nothing about the outcome shows in the limit.
func allowAccount(l *ratelimit.Limiter[[32]byte], addr string) error {
	if ok, retryAfter := l.Allow(sha256.Sum256([]byte(addr))); !ok {
		return &RateLimitedError{RetryAfter: retryAfter}
	}
	return nil
}
