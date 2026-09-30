package auth

import (
	"context"
	"fmt"
)

// Logout revokes the session whose current access token is rawAccess, so
// both its access and refresh tokens stop working (decision 015). The user's
// other sessions are untouched. An expired access token still logs out.
//
// It is idempotent: a well-formed token returns nil whether its session was
// revoked now, was already revoked or expired, or is unknown (including an
// access token already rotated out by refresh), so the result reveals nothing
// about the token. Only a token that isn't a well-formed access token returns
// ErrInvalidAccessToken, before any database access.
func (s *Service) Logout(ctx context.Context, rawAccess string) error {
	if !wellFormedToken(rawAccess, AccessTokenPrefix) {
		s.logger.InfoContext(ctx, "auth: logout failed", "reason", "malformed")
		return ErrInvalidAccessToken
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("auth: logout: %w", err)
	}

	sessionID, userID, revoked, err := revokeSessionByAccessToken(ctx, s.pool, HashToken(rawAccess))
	if err != nil {
		return fmt.Errorf("auth: logout: %w", err)
	}
	if revoked {
		s.logger.InfoContext(ctx, "auth: logout succeeded", "user_id", userID, "session_id", sessionID)
	} else {
		// No ids: an unknown or stale token isn't linked to an account.
		s.logger.InfoContext(ctx, "auth: logout had no effect")
	}
	return nil
}
