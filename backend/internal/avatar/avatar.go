// Package avatar owns members' profile pictures (decision 031): what an
// upload must be to be accepted, how it becomes the picture that is stored,
// and the stored pictures themselves. It knows nothing about HTTP or
// sessions and imports none of auth, server, profile and language; callers
// pass the ID of the user whose picture it is, which is its only key.
//
// Nothing a member uploads is ever kept: the stored picture is produced only
// from the decoded pixels, so no metadata, hidden data or second file format
// of the upload can survive in it.
package avatar

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// MaxUploadBytes is the largest upload that is looked at.
	MaxUploadBytes = 5 << 20
	// MaxDimension is the largest width or height, in pixels, an upload may
	// declare. It is checked before any pixel buffer is allocated.
	MaxDimension = 4096
	// Size is the width and the height, in pixels, of every stored picture.
	Size = 512

	// jpegQuality is the quality every stored picture is encoded at. It is
	// part of what makes the same upload always give the same bytes.
	jpegQuality = 85

	// decodeSlots is how many uploads are decoded at once, and
	// decodeQueueTimeout how long a request waits for its turn. A 4096x4096
	// image takes tens of megabytes while it is processed, so the cap makes
	// the worst case a constant (see normalizer.withSlot).
	decodeSlots        = 2
	decodeQueueTimeout = 5 * time.Second
)

// Service implements the profile picture use cases.
type Service struct {
	pool       *pgxpool.Pool
	logger     *slog.Logger
	normalizer *normalizer
}

// NewService returns the avatar service, with the production decode limits.
func NewService(pool *pgxpool.Pool, logger *slog.Logger) *Service {
	return &Service{pool: pool, logger: logger, normalizer: newNormalizer(decodeSlots, decodeQueueTimeout)}
}

// Get returns the picture of the user userID, a Size by Size JPEG, or
// ErrNotFound if they have none. userID is the user whose picture is read;
// the caller decides who may read it (decision 031).
func (s *Service) Get(ctx context.Context, userID string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("avatar: get: %w", err)
	}
	image, found, err := findImage(ctx, s.pool, userID)
	if err != nil {
		return nil, fmt.Errorf("avatar: get: %w", err)
	}
	if !found {
		return nil, ErrNotFound
	}
	return image, nil
}

// Exists reports whether the user userID has a picture, without reading it.
func (s *Service) Exists(ctx context.Context, userID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("avatar: exists: %w", err)
	}
	exists, err := imageExists(ctx, s.pool, userID)
	if err != nil {
		return false, fmt.Errorf("avatar: exists: %w", err)
	}
	return exists, nil
}

// Save makes the upload raw the picture of the user userID, replacing any
// earlier one, and returns the picture as stored: never raw, always what the
// server produced from its pixels (normalizer.normalize). An upload that is
// not acceptable returns a *ValidationError; ErrOverloaded means no decode
// slot came free in time; a user that no longer exists returns ErrUserGone.
// Whenever it fails, nothing was written and any earlier picture stands.
//
// Saving is idempotent by the hash of what was uploaded (decision 031): if
// the user's picture was made from these same bytes, it is returned without
// decoding, writing or logging, which makes any retry safe and nearly free.
// Otherwise the write is one statement, so it is atomic, and concurrent
// uploads are applied one after the other and the last one wins whole.
func (s *Service) Save(ctx context.Context, userID string, raw []byte) ([]byte, error) {
	// Refused uploads never reach the database or the decoder.
	f, err := inspect(raw)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("avatar: save: %w", err)
	}
	source := sha256.Sum256(raw)
	stored, found, err := findImageOfSource(ctx, s.pool, userID, source[:])
	if err != nil {
		return nil, fmt.Errorf("avatar: save: %w", err)
	}
	if found {
		return stored, nil
	}

	image, err := s.normalizer.produce(ctx, f, raw)
	if err != nil {
		var refused *ValidationError
		if errors.As(err, &refused) {
			return nil, err
		}
		return nil, fmt.Errorf("avatar: save: %w", err)
	}
	changed, err := upsertImage(ctx, s.pool, userID, image, source[:])
	if err != nil {
		return nil, fmt.Errorf("avatar: save: %w", err)
	}
	if changed {
		// The user and nothing about the image.
		s.logger.InfoContext(ctx, "avatar: saved", "user_id", userID)
	}
	return image, nil
}

// Delete removes the picture of the user userID. Having none is not an
// error, so it is idempotent: removing nothing writes nothing and logs
// nothing.
func (s *Service) Delete(ctx context.Context, userID string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("avatar: delete: %w", err)
	}
	removed, err := deleteImage(ctx, s.pool, userID)
	if err != nil {
		return fmt.Errorf("avatar: delete: %w", err)
	}
	if removed {
		s.logger.InfoContext(ctx, "avatar: removed", "user_id", userID)
	}
	return nil
}
