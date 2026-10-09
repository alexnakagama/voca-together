// Package safety owns what one member does about another: blocking them
// (decision 033). It knows nothing about HTTP or sessions, and it names
// members only by their internal user id: the caller resolves a public
// identifier before calling and never passes one in, so none can be stored,
// returned or logged here.
package safety

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MaxBlocks is how many members one member may block at a time. It bounds
// the list of blocked members to one response without paging.
const MaxBlocks = 200

// Service implements the blocking use cases.
type Service struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewService returns the safety service.
func NewService(pool *pgxpool.Pool, logger *slog.Logger) *Service {
	return &Service{pool: pool, logger: logger}
}

// Block makes the user blockerID block the user blockedID. Blocking oneself,
// or one member more than MaxBlocks, returns a *ValidationError and stores
// nothing; a blocker that no longer exists returns ErrUserGone.
//
// It is idempotent: blocking a member who is already blocked writes nothing,
// logs nothing and is never refused for the limit, which makes any retry
// safe. A blocked user that no longer exists is not an error either: there
// is nothing to block, and nothing is stored.
func (s *Service) Block(ctx context.Context, blockerID, blockedID string) error {
	if blockerID == blockedID {
		return &ValidationError{Fields: []*FieldError{ErrMemberSelf}}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("safety: block: %w", err)
	}
	added, err := insertBlock(ctx, s.pool, blockerID, blockedID)
	if err != nil {
		return fmt.Errorf("safety: block: %w", err)
	}
	if added {
		// The member who blocked and never whom.
		s.logger.InfoContext(ctx, "block: added", "user_id", blockerID)
	}
	return nil
}

// Unblock removes the block the user blockerID made of the user blockedID,
// and never the one blockedID may have made of them. Removing a block that
// doesn't exist changes nothing and logs nothing. Naming oneself returns a
// *ValidationError.
func (s *Service) Unblock(ctx context.Context, blockerID, blockedID string) error {
	if blockerID == blockedID {
		return &ValidationError{Fields: []*FieldError{ErrMemberSelf}}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("safety: unblock: %w", err)
	}
	removed, err := deleteBlock(ctx, s.pool, blockerID, blockedID)
	if err != nil {
		return fmt.Errorf("safety: unblock: %w", err)
	}
	if removed {
		s.logger.InfoContext(ctx, "block: removed", "user_id", blockerID)
	}
	return nil
}

// Blocked reports whether there is a block between the users a and b, in
// either direction: the answer is the same in both orders. Every route
// through which one member reads, finds or contacts another asks it, and
// treats true as "that member does not exist".
func (s *Service) Blocked(ctx context.Context, a, b string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("safety: blocked: %w", err)
	}
	blocked, err := blockBetween(ctx, s.pool, a, b)
	if err != nil {
		return false, fmt.Errorf("safety: blocked: %w", err)
	}
	return blocked, nil
}

// ListBlocked returns the ids of the users userID has blocked, most recently
// blocked first: at most MaxBlocks. It never holds who blocked userID.
func (s *Service) ListBlocked(ctx context.Context, userID string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("safety: list blocked: %w", err)
	}
	ids, err := listBlocked(ctx, s.pool, userID)
	if err != nil {
		return nil, fmt.Errorf("safety: list blocked: %w", err)
	}
	return ids, nil
}
