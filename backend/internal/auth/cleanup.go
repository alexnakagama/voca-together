package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// cleanupRetention is how long dead rows are kept after they stop being
	// usable: an audit window for used tokens and revoked sessions.
	cleanupRetention = 30 * 24 * time.Hour
	// cleanupBatchSize keeps each DELETE short.
	cleanupBatchSize = 1000
)

// RunCleanup deletes dead sessions, one-time tokens and Google ID token uses
// (see cleanup) after firstDelay and then every interval, until ctx ends.
// Failures are logged and retried at the next run. Deletion is idempotent and
// never waits for a lock, so several instances may run it at once.
func (s *Service) RunCleanup(ctx context.Context, firstDelay, interval time.Duration) {
	timer := time.NewTimer(firstDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		sessions, tokens, googleTokens, err := s.cleanup(ctx)
		counts := []any{"sessions_deleted", sessions, "tokens_deleted", tokens, "google_token_uses_deleted", googleTokens}
		switch {
		case errors.Is(err, context.Canceled) && ctx.Err() != nil:
			return
		case err != nil:
			s.logger.ErrorContext(ctx, "auth: cleanup failed", append([]any{"err", err}, counts...)...)
		default:
			s.logger.InfoContext(ctx, "auth: cleanup", counts...)
		}
		timer.Reset(interval)
	}
}

// cleanup deletes, in batches, the sessions and one-time tokens that stopped
// being usable more than cleanupRetention ago (decision 018), and the Google
// ID token uses past their expiry (decision 020). Their responses don't
// change: a deleted session's tokens were already rejected, a deleted
// one-time token was already invalid, and the verifier already rejects an
// expired Google ID token. Users are never deleted.
func (s *Service) cleanup(ctx context.Context) (sessions, tokens, googleTokens int64, err error) {
	sessions, err = deleteInBatches(ctx, s, deleteDeadSessions)
	if err != nil {
		return sessions, 0, 0, fmt.Errorf("auth: cleanup sessions: %w", err)
	}
	tokens, err = deleteInBatches(ctx, s, deleteDeadTokens)
	if err != nil {
		return sessions, tokens, 0, fmt.Errorf("auth: cleanup tokens: %w", err)
	}
	googleTokens, err = deleteInBatches(ctx, s,
		func(ctx context.Context, pool *pgxpool.Pool, _ time.Duration, limit int) (int64, error) {
			return deleteExpiredGoogleTokenUses(ctx, pool, limit)
		})
	if err != nil {
		return sessions, tokens, googleTokens, fmt.Errorf("auth: cleanup google id token uses: %w", err)
	}
	return sessions, tokens, googleTokens, nil
}

// deleteInBatches runs del until a batch deletes fewer than
// s.cleanupBatchSize rows, and returns the total deleted.
func deleteInBatches(ctx context.Context, s *Service,
	del func(context.Context, *pgxpool.Pool, time.Duration, int) (int64, error)) (int64, error) {
	var total int64
	for {
		n, err := del(ctx, s.pool, cleanupRetention, s.cleanupBatchSize)
		total += n
		if err != nil || n < int64(s.cleanupBatchSize) {
			return total, err
		}
	}
}
