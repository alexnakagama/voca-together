package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Values of user_tokens.purpose.
const PurposeEmailVerification = "email_verification"

// Lock order: a transaction that changes user_tokens rows of an existing user
// must first lock that user's row (SELECT … FOR UPDATE on users). Every token
// flow (verification, resend, and later password reset) follows it, so two
// flows on one account serialize on the user row instead of deadlocking on
// user and token rows taken in opposite orders. Registration is exempt: the
// user row it creates is invisible to others until commit.

// createUserWithVerificationToken inserts a user and its email verification
// token in one transaction, so a user never exists without the token that
// lets them verify. If the address already has an account it writes nothing
// and returns created=false; the existing account is left untouched.
//
// ON CONFLICT (rather than catching the unique violation) keeps concurrent
// registrations of one address safe, and avoids a PostgreSQL error whose
// detail would contain the address.
func createUserWithVerificationToken(ctx context.Context, pool *pgxpool.Pool,
	email, passwordHash string, tokenHash []byte, ttl time.Duration) (created bool, err error) {
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var userID string
		err := tx.QueryRow(ctx,
			`INSERT INTO users (email, password_hash) VALUES ($1, $2)
			 ON CONFLICT ON CONSTRAINT users_email_key DO NOTHING
			 RETURNING id`,
			email, passwordHash).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("insert user: %w", err)
		}

		created = true
		return issueToken(ctx, tx, userID, PurposeEmailVerification, tokenHash, ttl)
	})
	if err != nil {
		return false, fmt.Errorf("auth: create user: %w", err)
	}

	return created, nil
}

// issueToken stores a one-time token hash for the user, first deleting any
// unused token with the same purpose so older links stop working (decision
// 004). The expiry uses the database clock, like token consumption does.
func issueToken(ctx context.Context, tx pgx.Tx, userID, purpose string, tokenHash []byte, ttl time.Duration) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM user_tokens WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL`,
		userID, purpose); err != nil {
		return fmt.Errorf("delete old token: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO user_tokens (user_id, purpose, token_hash, expires_at)
		 VALUES ($1, $2, $3, now() + make_interval(secs => $4))`,
		userID, purpose, tokenHash, ttl.Seconds()); err != nil {
		return fmt.Errorf("insert token: %w", err)
	}

	return nil
}

// consumeVerificationToken marks the email verification token with the given
// hash as used and verifies its owner's email, atomically. It returns
// ok=false, having changed nothing, if no such token is usable: unknown,
// wrong purpose, already used, or expired.
func consumeVerificationToken(ctx context.Context, pool *pgxpool.Pool, tokenHash []byte) (ok bool, err error) {
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		ok, err = consumeVerificationTokenTx(ctx, tx, tokenHash)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("auth: consume verification token: %w", err)
	}
	return ok, nil
}

func consumeVerificationTokenTx(ctx context.Context, tx pgx.Tx, tokenHash []byte) (bool, error) {
	// Only takes the user lock (see Lock order); the UPDATE below decides.
	var userID string
	err := tx.QueryRow(ctx,
		`SELECT u.id FROM user_tokens t JOIN users u ON u.id = t.user_id
		 WHERE t.token_hash = $1 AND t.purpose = $2
		 FOR UPDATE OF u`,
		tokenHash, PurposeEmailVerification).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock user: %w", err)
	}

	// Single use (decision 004). The statement sees rows committed while it
	// waited for the lock above, so a concurrent consumer or a rotation
	// by resend makes it match nothing.
	tag, err := tx.Exec(ctx,
		`UPDATE user_tokens SET used_at = now()
		 WHERE token_hash = $1 AND purpose = $2 AND user_id = $3
		   AND used_at IS NULL AND expires_at > now()`,
		tokenHash, PurposeEmailVerification, userID)
	if err != nil {
		return false, fmt.Errorf("use token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	// An already verified account keeps its original verification time.
	if _, err := tx.Exec(ctx,
		`UPDATE users SET email_verified_at = now(), updated_at = now()
		 WHERE id = $1 AND email_verified_at IS NULL`,
		userID); err != nil {
		return false, fmt.Errorf("verify user: %w", err)
	}
	return true, nil
}

// reissueVerificationToken replaces the verification token of the unverified
// account with the given email. It returns issued=false, having changed
// nothing, if there is no such account or it is already verified.
func reissueVerificationToken(ctx context.Context, pool *pgxpool.Pool,
	email string, tokenHash []byte, ttl time.Duration) (issued bool, err error) {
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		issued, err = reissueVerificationTokenTx(ctx, tx, email, tokenHash, ttl)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("auth: reissue verification token: %w", err)
	}
	return issued, nil
}

func reissueVerificationTokenTx(ctx context.Context, tx pgx.Tx,
	email string, tokenHash []byte, ttl time.Duration) (bool, error) {
	var userID string
	var verified bool
	err := tx.QueryRow(ctx,
		`SELECT id, email_verified_at IS NOT NULL FROM users WHERE email = $1 FOR UPDATE`,
		email).Scan(&userID, &verified)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock user: %w", err)
	}
	if verified {
		return false, nil
	}
	if err := issueToken(ctx, tx, userID, PurposeEmailVerification, tokenHash, ttl); err != nil {
		return false, err
	}
	return true, nil
}
