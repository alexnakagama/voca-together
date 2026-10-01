package auth

import (
	"context"
	"errors"
	"fmt"

	"vocatogether/backend/internal/googleid"
)

// SignInWithGoogle exchanges a Google ID token for a new session of the
// VocaTogether account linked to its Google identity, creating a
// passwordless account first if none is linked and the token's email may
// create one (decision 020). Google only establishes identity: the result is
// an ordinary session, exactly as Login returns it, and the ID token is never
// a credential itself.
//
// The token is verified first, and nothing but its verified claims is used.
// A linked identity signs in by subject alone, whatever its token's email now
// says. An unknown identity needs an email Google verified that passes
// NormalizeEmail (else ErrGoogleEmailUnusable) and that belongs to no account
// (else ErrAccountExists; nothing is ever linked automatically).
//
// Every unusable token (rejected by the verifier, already used, or Google
// sign-in not configured) returns ErrInvalidGoogleToken, and unavailable
// Google keys return ErrGoogleUnavailable; neither writes anything. An empty
// token returns a *ValidationError, and a spent per-subject limit a
// *RateLimitedError, before any database work. Once a verified token reaches
// the database it is spent, whatever the outcome; the store owns that
// transaction and its only retry. Credentials are returned only after the
// session is committed.
func (s *Service) SignInWithGoogle(ctx context.Context, rawIDToken, userAgent string) (Credentials, error) {
	if rawIDToken == "" {
		return Credentials{}, validationError(ErrIDTokenRequired)
	}
	if s.google == nil {
		s.logGoogleFailed(ctx, "not_configured", "")
		return Credentials{}, ErrInvalidGoogleToken
	}
	claims, err := s.google.Verify(ctx, rawIDToken)
	if err != nil {
		return Credentials{}, s.googleVerifyError(ctx, err)
	}
	// The verifier always sets it; without it the token use would expire at
	// once and the token could be replayed.
	if claims.AcceptedUntil().IsZero() {
		return Credentials{}, errors.New("auth: google sign-in: verifier returned no expiry")
	}
	// Keyed by subject, not email: Google sign-in has its own bucket, and a
	// new identity's email is the token holder's choice. Subjects never
	// contain "@", so no key matches an email's.
	if err := allowAccount(s.limits.Login, "google:"+claims.Subject()); err != nil {
		return Credentials{}, err
	}
	if err := ctx.Err(); err != nil {
		return Credentials{}, fmt.Errorf("auth: google sign-in: %w", err)
	}

	addr, ineligible := googleAccountEmail(claims)
	access, refresh := NewToken(AccessTokenPrefix), NewToken(RefreshTokenPrefix)
	r, err := googleSignIn(ctx, s.pool, googleSignInInput{
		tokenHash:     HashToken(rawIDToken),
		acceptedUntil: claims.AcceptedUntil(),
		subject:       claims.Subject(),
		eligible:      ineligible == "",
		email:         addr,
		accessHash:    access.Hash,
		refreshHash:   refresh.Hash,
		userAgent:     normalizeUserAgent(userAgent),
	})
	if err != nil {
		return Credentials{}, err // already "auth: google sign-in: …"
	}

	switch r.outcome {
	case googleSignedIn, googleCreated:
		s.logger.InfoContext(ctx, "auth: google sign-in succeeded",
			"user_id", r.userID, "session_id", r.sessionID, "new_account", r.outcome == googleCreated)
		return Credentials{AccessToken: access, RefreshToken: refresh, ExpiresIn: accessTokenTTL}, nil
	case googleReplayed:
		s.logGoogleFailed(ctx, "replayed", "")
		return Credentials{}, ErrInvalidGoogleToken
	case googleIneligible:
		s.logGoogleFailed(ctx, ineligible, "")
		return Credentials{}, ErrGoogleEmailUnusable
	case googleAccountExists:
		s.logGoogleFailed(ctx, "account_exists", r.userID)
		return Credentials{}, ErrAccountExists
	}
	return Credentials{}, fmt.Errorf("auth: google sign-in: unexpected outcome %d", r.outcome)
}

// googleVerifyError maps a verifier error. Rejections and unavailability
// carry fixed reasons, safe to log; anything else is the context's error.
func (s *Service) googleVerifyError(ctx context.Context, err error) error {
	var invalid *googleid.InvalidTokenError
	var unavailable *googleid.UnavailableError
	switch {
	case errors.As(err, &invalid):
		s.logGoogleFailed(ctx, string(invalid.Reason), "")
		return ErrInvalidGoogleToken
	case errors.Is(err, googleid.ErrInvalidToken):
		s.logGoogleFailed(ctx, "invalid_token", "")
		return ErrInvalidGoogleToken
	case errors.As(err, &unavailable):
		s.logger.WarnContext(ctx, "auth: google keys unavailable", "reason", unavailable.Reason)
		return ErrGoogleUnavailable
	case errors.Is(err, googleid.ErrUnavailable):
		s.logger.WarnContext(ctx, "auth: google keys unavailable")
		return ErrGoogleUnavailable
	}
	return fmt.Errorf("auth: google sign-in: %w", err)
}

// googleAccountEmail returns the normalized address a new account for
// claims would get, or why it can't get one (a reason for the log). Only an
// unknown identity uses it; a linked one signs in whatever its email.
func googleAccountEmail(claims googleid.Claims) (addr, ineligible string) {
	switch {
	case claims.Email() == "":
		return "", "email_missing"
	case !claims.EmailVerified():
		return "", "email_unverified"
	}
	addr, err := NormalizeEmail(claims.Email())
	if err != nil {
		return "", "email_invalid"
	}
	return addr, ""
}

// logGoogleFailed records a failed Google sign-in for auditing. userID is
// set only for account_exists (the existing account); the email, subject
// and token are never logged.
func (s *Service) logGoogleFailed(ctx context.Context, reason, userID string) {
	attrs := []any{"reason", reason}
	if userID != "" {
		attrs = append(attrs, "user_id", userID)
	}
	s.logger.InfoContext(ctx, "auth: google sign-in failed", attrs...)
}
