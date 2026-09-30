package auth

import (
	"context"
	"fmt"
	"time"
)

// Identity is who an access token authenticates: its session and that
// session's user.
type Identity struct {
	UserID    string
	SessionID string
}

// User is the account data safe to show its owner: no password hash, token
// or session data.
type User struct {
	ID              string
	Email           string
	EmailVerifiedAt time.Time // always set: only verified accounts can log in
	CreatedAt       time.Time
}

// Authenticate returns the identity of the live session whose current access
// token is rawAccess (decision 016). Every unusable token (malformed, a
// refresh token, unknown, rotated out, revoked, or expired) returns
// ErrInvalidAccessToken; malformed ones before any database access.
//
// It only reads: nothing is written, not even last_used_at, and no row is
// locked. A concurrent refresh is seen either before or after it commits:
// the old token works until the rotation commits and never after.
func (s *Service) Authenticate(ctx context.Context, rawAccess string) (Identity, error) {
	if !wellFormedToken(rawAccess, AccessTokenPrefix) {
		s.logger.InfoContext(ctx, "auth: access token rejected", "reason", "malformed")
		return Identity{}, ErrInvalidAccessToken
	}
	if err := ctx.Err(); err != nil {
		return Identity{}, fmt.Errorf("auth: authenticate: %w", err)
	}

	id, found, err := findSessionByAccessToken(ctx, s.pool, HashToken(rawAccess))
	if err != nil {
		return Identity{}, fmt.Errorf("auth: authenticate: %w", err)
	}
	if !found {
		// No ids: an unknown or stale token isn't linked to an account.
		s.logger.InfoContext(ctx, "auth: access token rejected", "reason", "invalid")
		return Identity{}, ErrInvalidAccessToken
	}
	return id, nil
}

// User returns the account of an authenticated user. A user that no longer
// exists returns ErrInvalidAccessToken: deleting a user deletes its sessions,
// so the credential that authenticated the request is dead.
func (s *Service) User(ctx context.Context, userID string) (User, error) {
	u, found, err := findUserByID(ctx, s.pool, userID)
	if err != nil {
		return User{}, fmt.Errorf("auth: user: %w", err)
	}
	if !found {
		return User{}, ErrInvalidAccessToken
	}
	return u, nil
}
