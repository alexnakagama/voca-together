package auth

import (
	"context"
	"fmt"
)

// Refresh exchanges a refresh token for new credentials of the same session,
// rotating both tokens (decisions 002 and 014). The presented refresh token
// and the session's previous access token stop working.
//
// Every unusable token (malformed, unknown, expired, revoked, or reused)
// returns ErrInvalidRefreshToken. Reuse, presenting a token that was already
// rotated out, also revokes the session, so the current tokens stop working
// too: one of the two holders is not the legitimate client. An empty token
// returns a *ValidationError. Malformed tokens are rejected before any
// database access. Credentials are returned only after the rotation is
// committed.
func (s *Service) Refresh(ctx context.Context, rawRefresh string) (Credentials, error) {
	if rawRefresh == "" {
		return Credentials{}, validationError(ErrRefreshTokenRequired)
	}
	if !wellFormedToken(rawRefresh, RefreshTokenPrefix) {
		s.logRefreshFailed(ctx, "malformed", rotation{})
		return Credentials{}, ErrInvalidRefreshToken
	}
	if err := ctx.Err(); err != nil {
		return Credentials{}, fmt.Errorf("auth: refresh: %w", err)
	}

	access, refresh := NewToken(AccessTokenPrefix), NewToken(RefreshTokenPrefix)
	r, err := rotateRefreshToken(ctx, s.pool, HashToken(rawRefresh), access.Hash, refresh.Hash)
	if err != nil {
		return Credentials{}, fmt.Errorf("auth: refresh: %w", err)
	}

	switch r.outcome {
	case refreshRotated:
		s.logger.InfoContext(ctx, "auth: refresh succeeded", "user_id", r.userID, "session_id", r.sessionID)
		return Credentials{AccessToken: access, RefreshToken: refresh, ExpiresIn: r.expiresIn}, nil
	case refreshReused:
		s.logger.WarnContext(ctx, "auth: refresh token reuse detected, session revoked",
			"user_id", r.userID, "session_id", r.sessionID)
		return Credentials{}, ErrInvalidRefreshToken
	default:
		s.logRefreshFailed(ctx, refreshReason[r.outcome], r)
		return Credentials{}, ErrInvalidRefreshToken
	}
}

// logRefreshFailed records a rejected refresh with the session it matched,
// if any. Tokens and their hashes are never logged.
func (s *Service) logRefreshFailed(ctx context.Context, reason string, r rotation) {
	attrs := []any{"reason", reason}
	if r.sessionID != "" {
		attrs = append(attrs, "user_id", r.userID, "session_id", r.sessionID)
	}
	s.logger.InfoContext(ctx, "auth: refresh failed", attrs...)
}
