package safety

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	foreignKeyViolation = "23503"
	blockedForeignKey   = "blocks_blocked_id_fkey"
)

// errBlockedGone ends insertBlock's transaction when the user to block no
// longer exists. It never leaves this file.
var errBlockedGone = errors.New("blocked user gone")

// insertBlock stores the block of blockedID by blockerID. added is false
// when nothing was written: the block already existed, or blockedID is not a
// user (deleted since the caller resolved it). It returns ErrUserGone if
// blockerID doesn't exist, and a *ValidationError, having written nothing,
// if blockerID already blocks MaxBlocks members and this would be one more.
//
// The limit can't be checked and the row inserted in one statement that
// stays exact under concurrency, so this is one transaction, and the first
// thing it does is lock the blocker's users row FOR NO KEY UPDATE. That
// makes one member's blocks run in turn: each counts what the one before
// committed, so two at once can't both take the last place, and identical
// ones can't both insert.
//
// Locks: the blocker's users row comes first, which is the lock order of
// auth (see Lock order in auth/store.go). Two kinds of auth transaction hold
// that row, and FOR NO KEY UPDATE conflicts with both, so this waits for
// them and they for it:
//   - the token flows (verification, resend, forgot and reset password) hold
//     it FOR UPDATE;
//   - login and Google sign-in hold it FOR SHARE.
//
// Neither is a cycle. Each of them takes the users row before any other lock
// on an existing user, and after it touches only that user's token and
// session rows, which this transaction never locks: whichever gets the row
// first finishes without waiting for the other.
//
// The insert then takes FOR KEY SHARE on both users rows for the foreign
// keys. FOR KEY SHARE conflicts only with FOR UPDATE: not with FOR NO KEY
// UPDATE, so two members blocking each other at once each hold their own row
// and are not stopped by the other's, and not with the FOR SHARE of a login.
// The key share on the blocked row can wait for a token flow holding it FOR
// UPDATE, which waits for nothing of ours. A transaction that deletes the
// blocker waits for this one, so the blocker's foreign key can't fail after
// the lock.
func insertBlock(ctx context.Context, pool *pgxpool.Pool, blockerID, blockedID string) (added bool, err error) {
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var one int
		err := tx.QueryRow(ctx, `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`, blockerID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserGone
		}
		if err != nil {
			return fmt.Errorf("lock user: %w", err)
		}

		var (
			count   int
			already bool
		)
		err = tx.QueryRow(ctx,
			`SELECT count(*), coalesce(bool_or(blocked_id = $2), false) FROM blocks WHERE blocker_id = $1`,
			blockerID, blockedID).Scan(&count, &already)
		if err != nil {
			return fmt.Errorf("count blocks: %w", err)
		}
		if already {
			return nil
		}
		if count >= MaxBlocks {
			return &ValidationError{Fields: []*FieldError{ErrBlocksTooMany}}
		}

		_, err = tx.Exec(ctx, `INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, blockerID, blockedID)
		var pgErr *pgconn.PgError
		switch {
		case errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation && pgErr.ConstraintName == blockedForeignKey:
			return errBlockedGone
		case err != nil:
			return fmt.Errorf("insert block: %w", err)
		}
		added = true
		return nil
	})
	switch {
	case errors.Is(err, errBlockedGone):
		return false, nil
	case err != nil:
		return false, err
	}
	return added, nil
}

// deleteBlock removes the block of blockedID by blockerID; removed=false if
// there was none. One statement on the primary key: it needs no lock of its
// own and no check of the user, since without the user there is no row.
func deleteBlock(ctx context.Context, pool *pgxpool.Pool, blockerID, blockedID string) (removed bool, err error) {
	tag, err := pool.Exec(ctx, `DELETE FROM blocks WHERE blocker_id = $1 AND blocked_id = $2`, blockerID, blockedID)
	if err != nil {
		return false, fmt.Errorf("delete block: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// blockBetween reports whether either of a and b has blocked the other. One
// statement, without a lock: each order of the pair is an equality match on
// both columns of the primary key.
func blockBetween(ctx context.Context, pool *pgxpool.Pool, a, b string) (blocked bool, err error) {
	err = pool.QueryRow(ctx,
		`SELECT EXISTS (
		     SELECT 1 FROM blocks
		     WHERE (blocker_id, blocked_id) IN (($1::uuid, $2::uuid), ($2::uuid, $1::uuid)))`,
		a, b).Scan(&blocked)
	if err != nil {
		return false, fmt.Errorf("block between: %w", err)
	}
	return blocked, nil
}

// listBlocked returns the users blockerID has blocked, newest first, without
// those who have blocked blockerID themselves: to blockerID such a member
// does not exist, and a list that named them would say otherwise. The id
// only settles the order of two blocks stored at the same instant.
func listBlocked(ctx context.Context, pool *pgxpool.Pool, blockerID string) ([]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT b.blocked_id::text FROM blocks b
		 WHERE b.blocker_id = $1
		   AND NOT EXISTS (SELECT 1 FROM blocks r WHERE r.blocker_id = b.blocked_id AND r.blocked_id = b.blocker_id)
		 ORDER BY b.created_at DESC, b.blocked_id`, blockerID)
	if err != nil {
		return nil, fmt.Errorf("list blocked: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("list blocked: %w", err)
	}
	return ids, nil
}
