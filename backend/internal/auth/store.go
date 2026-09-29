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
