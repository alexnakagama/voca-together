// Package profile owns user profiles: what a member chooses to show of
// themselves (decision 027). It knows nothing about HTTP or sessions; callers
// pass the ID of the authenticated user, which is the profile's only key.
package profile

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Input is everything a user can set on their profile.
type Input struct {
	DisplayName string
	Bio         string
}

// Profile is a user's saved profile. DisplayName and Bio are the text meant
// for other members; nothing else about the user belongs here.
type Profile struct {
	DisplayName string
	Bio         string // "" when the user wrote none
	CreatedAt   time.Time
	UpdatedAt   time.Time // last change of DisplayName or Bio
}

// Service implements the profile use cases.
type Service struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewService returns the profile service.
func NewService(pool *pgxpool.Pool, logger *slog.Logger) *Service {
	return &Service{pool: pool, logger: logger}
}

// Get returns the profile of the user userID, or ErrNotFound if they haven't
// saved one. userID must be the authenticated user: it is the only thing
// that selects a profile.
func (s *Service) Get(ctx context.Context, userID string) (Profile, error) {
	if err := ctx.Err(); err != nil {
		return Profile{}, fmt.Errorf("profile: get: %w", err)
	}
	p, found, err := findProfile(ctx, s.pool, userID)
	if err != nil {
		return Profile{}, fmt.Errorf("profile: get: %w", err)
	}
	if !found {
		return Profile{}, ErrNotFound
	}
	return p, nil
}

// Save replaces the whole profile of the user userID with in, creating it if
// there is none, and returns it as stored (normalized). Invalid input returns
// a *ValidationError and writes nothing; a user that no longer exists returns
// ErrUserGone.
//
// The write is one statement, so it is atomic, and saving is idempotent:
// saving the text the profile already has writes nothing, logs nothing and
// leaves UpdatedAt alone, which makes any retry safe. Concurrent saves are
// applied one after the other and the last one wins whole (decision 027).
func (s *Service) Save(ctx context.Context, userID string, in Input) (Profile, error) {
	in, err := normalize(in)
	if err != nil {
		return Profile{}, err
	}
	if err := ctx.Err(); err != nil {
		return Profile{}, fmt.Errorf("profile: save: %w", err)
	}
	p, changed, err := upsertProfile(ctx, s.pool, userID, in)
	if err != nil {
		return Profile{}, fmt.Errorf("profile: save: %w", err)
	}
	if changed {
		// The user and nothing they wrote.
		s.logger.InfoContext(ctx, "profile: saved", "user_id", userID)
	}
	return p, nil
}
