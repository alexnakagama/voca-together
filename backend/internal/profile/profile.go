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

// Profile is a user's saved profile, as its owner sees it. DisplayName and
// Bio are the text meant for other members; nothing else about the user
// belongs here.
type Profile struct {
	// PublicID names the profile to other members (decision 031). It is
	// assigned by the database when the profile is created and never changes.
	PublicID    string
	DisplayName string
	Bio         string // "" when the user wrote none
	CreatedAt   time.Time
	UpdatedAt   time.Time // last change of DisplayName or Bio
}

// PublicProfile is what any signed-in member may read of another's profile,
// found by its public identifier. UserID is the owner's internal id, for the
// caller to read the owner's other public data with; it must never leave the
// server.
type PublicProfile struct {
	UserID      string
	PublicID    string
	DisplayName string
	Bio         string // "" when the user wrote none
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

// Public returns the profile that publicID names, or ErrNotFound. A value
// that is not a well-formed identifier (ParsePublicID) names nothing and is
// answered without a query, exactly as an unknown one is. A member who has
// saved no profile has no public identifier, so nothing reaches them.
//
// Unlike Get, the caller is not the owner: who may read is the caller's
// decision.
func (s *Service) Public(ctx context.Context, publicID string) (PublicProfile, error) {
	id, ok := ParsePublicID(publicID)
	if !ok {
		return PublicProfile{}, ErrNotFound
	}
	if err := ctx.Err(); err != nil {
		return PublicProfile{}, fmt.Errorf("profile: public: %w", err)
	}
	p, found, err := findPublicProfile(ctx, s.pool, id)
	if err != nil {
		return PublicProfile{}, fmt.Errorf("profile: public: %w", err)
	}
	if !found {
		return PublicProfile{}, ErrNotFound
	}
	return p, nil
}

// PublicByUsers returns the public profiles of the users userIDs, in no
// particular order: one for each of them that has saved a profile, and
// nothing for one that hasn't, or that no longer exists. No ids is no query.
//
// Like Public, the caller is not the owner and decides who may read. Unlike
// it, the keys are internal user ids, which the caller got from the server's
// own data and never from a request.
func (s *Service) PublicByUsers(ctx context.Context, userIDs []string) ([]PublicProfile, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("profile: public by users: %w", err)
	}
	ps, err := findPublicProfilesByUsers(ctx, s.pool, userIDs)
	if err != nil {
		return nil, fmt.Errorf("profile: public by users: %w", err)
	}
	return ps, nil
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
