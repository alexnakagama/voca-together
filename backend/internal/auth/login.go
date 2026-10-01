package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Session lifetimes (decision 002). Refresh slides refreshTokenTTL forward on
// each use, but never past the session's absolute sessionMaxLifetime.
const (
	accessTokenTTL     = 15 * time.Minute
	refreshTokenTTL    = 30 * 24 * time.Hour
	sessionMaxLifetime = 90 * 24 * time.Hour
	// minAccessTokenLifetime is the least time a refreshed access token must
	// have before the session's absolute expiry; with less left, refresh is
	// refused instead of issuing a token that is useless on arrival.
	minAccessTokenLifetime = time.Minute
)

// Credentials holds a session's current tokens, as issued by login or
// refresh. The tokens are secrets for the response body only; Token redacts
// itself everywhere else. ExpiresIn is the access token's remaining lifetime:
// accessTokenTTL, or less near the session's absolute expiry.
type Credentials struct {
	AccessToken  Token
	RefreshToken Token
	ExpiresIn    time.Duration
}

// Login checks an email and password and, for a verified account, creates a
// new session (one per login/device) and returns its credentials.
//
// Unknown email, wrong password, an unusable stored hash and an account
// without a password (created with Google) all return ErrInvalidCredentials
// after the same argon2 work (the dummy hash stands in for a missing one), so
// neither the result nor its timing reveals which addresses have accounts or
// how they sign in. ErrEmailNotVerified is returned only once the
// password is correct. Invalid input returns a *ValidationError, and a
// spent per-account limit a *RateLimitedError, before any hashing or
// database access. Credentials are returned only after the
// session is committed.
func (s *Service) Login(ctx context.Context, emailInput, password, userAgent string) (Credentials, error) {
	addr, emailErr := NormalizeEmail(emailInput)
	var passwordErr error
	if password == "" {
		passwordErr = ErrPasswordRequired
	}
	if err := validationError(emailErr, passwordErr); err != nil {
		return Credentials{}, err
	}
	// Before any database or argon2 work, and for unknown addresses too.
	if err := allowAccount(s.limits.Login, addr); err != nil {
		return Credentials{}, err
	}
	if err := ctx.Err(); err != nil {
		return Credentials{}, fmt.Errorf("auth: login: %w", err)
	}

	u, found, err := findUserByEmail(ctx, s.pool, addr)
	if err != nil {
		return Credentials{}, fmt.Errorf("auth: login: %w", err)
	}
	hasPassword := found && u.passwordHash != ""
	stored := s.dummyHash
	if hasPassword {
		stored = u.passwordHash
	}
	ok, needsRehash, err := s.verifyPassword(ctx, stored, password)
	if errors.Is(err, ErrMalformedHash) {
		// Corrupt data, not a client error. The client gets the usual answer,
		// so it can't probe for broken accounts.
		s.logger.ErrorContext(ctx, "auth: stored password hash malformed", "user_id", u.id)
		ok, err = false, nil
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("auth: login: %w", err)
	}
	if found && !hasPassword {
		s.logLoginFailed(ctx, "no_password", u.id)
		return Credentials{}, ErrInvalidCredentials
	}
	if !found || !ok {
		s.logLoginFailed(ctx, "invalid_credentials", u.id)
		return Credentials{}, ErrInvalidCredentials
	}
	if !u.verified {
		s.logLoginFailed(ctx, "email_not_verified", u.id)
		return Credentials{}, ErrEmailNotVerified
	}

	access, refresh := NewToken(AccessTokenPrefix), NewToken(RefreshTokenPrefix)
	sessionID, err := createSession(ctx, s.pool, u.id, u.passwordHash, access.Hash, refresh.Hash, normalizeUserAgent(userAgent))
	if errors.Is(err, errPasswordChanged) {
		s.logLoginFailed(ctx, "password_changed", u.id)
		return Credentials{}, ErrInvalidCredentials
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("auth: login: %w", err)
	}

	if needsRehash {
		s.rehashPassword(ctx, u, password)
	}
	s.logger.InfoContext(ctx, "auth: login succeeded", "user_id", u.id, "session_id", sessionID)
	return Credentials{AccessToken: access, RefreshToken: refresh, ExpiresIn: accessTokenTTL}, nil
}

// logLoginFailed records a failed login for auditing (e.g. spotting
// credential stuffing). userID is empty for unknown emails; the email itself
// is never logged.
func (s *Service) logLoginFailed(ctx context.Context, reason, userID string) {
	attrs := []any{"reason", reason}
	if userID != "" {
		attrs = append(attrs, "user_id", userID)
	}
	s.logger.InfoContext(ctx, "auth: login failed", attrs...)
}

// rehashPassword upgrades a verified password's hash to the current argon2
// parameters (decision 003). It runs after the session is committed and is
// best effort: on failure the old hash still works and the next login
// retries. The compare-and-swap never overwrites a password changed since.
func (s *Service) rehashPassword(ctx context.Context, u loginUser, password string) {
	newHash, err := s.hashPassword(ctx, password)
	if err == nil {
		_, err = updatePasswordHash(ctx, s.pool, u.id, u.passwordHash, newHash)
	}
	if err != nil {
		s.logger.WarnContext(ctx, "auth: password rehash failed", "user_id", u.id, "err", err)
	}
}

// maxUserAgentBytes bounds the stored User-Agent (matches the
// sessions_user_agent_length CHECK). Real browsers stay well below it.
const maxUserAgentBytes = 256

// normalizeUserAgent prepares the client's User-Agent header for storage, or
// returns nil if nothing is left. It is client-supplied text, kept only so a
// user can recognize their sessions: never trusted, never used for decisions,
// and never logged. Invalid UTF-8 and control characters are removed
// (PostgreSQL text rejects NUL), surrounding space trimmed, and the result
// truncated to maxUserAgentBytes on a character boundary.
func normalizeUserAgent(ua string) *string {
	ua = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(ua, ""))
	ua = strings.TrimSpace(ua)
	if len(ua) > maxUserAgentBytes {
		cut := maxUserAgentBytes
		for !utf8.RuneStart(ua[cut]) {
			cut--
		}
		ua = strings.TrimSpace(ua[:cut])
	}
	if ua == "" {
		return nil
	}
	return &ua
}
