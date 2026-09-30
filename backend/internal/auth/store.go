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

// Lock order: a transaction that changes user_tokens or sessions rows of an existing user
// must first lock that user's row (SELECT … FOR UPDATE on users). Every token
// flow (verification, resend, and later password reset) follows it, so two
// flows on one account serialize on the user row instead of deadlocking on
// user and token rows taken in opposite orders. Registration is exempt: the
// user row it creates is invisible to others until commit. Login takes the
// user row FOR SHARE (see createSession), so logins don't block each other
// but do serialize with anything that changes the password.

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

// loginUser is what login needs to know about the account for an email.
type loginUser struct {
	id           string
	passwordHash string
	verified     bool
}

// findUserByEmail looks up the account for a normalized email, without
// locking: createSession re-checks the password hash under a lock.
func findUserByEmail(ctx context.Context, pool *pgxpool.Pool, email string) (u loginUser, found bool, err error) {
	err = pool.QueryRow(ctx,
		`SELECT id, password_hash, email_verified_at IS NOT NULL FROM users WHERE email = $1`,
		email).Scan(&u.id, &u.passwordHash, &u.verified)
	if errors.Is(err, pgx.ErrNoRows) {
		return loginUser{}, false, nil
	}
	if err != nil {
		return loginUser{}, false, fmt.Errorf("auth: find user: %w", err)
	}
	return u, true, nil
}

// errPasswordChanged reports that the user's password hash is no longer the
// one login verified against.
var errPasswordChanged = errors.New("auth: password changed during login")

// createSession inserts a new session for the user and returns its id, but
// only if the user's password hash is still passwordHash, the one login
// verified. Otherwise it writes nothing and returns errPasswordChanged.
//
// The re-check holds the user row FOR SHARE until commit (see Lock order).
// A password change (later: reset, which revokes all sessions) holding the
// row FOR UPDATE makes this wait and then see the new hash, so no session
// created with the old password can slip past the revocation; if login gets
// the lock first, the change waits until the session exists and can revoke
// it. Concurrent logins share the lock and don't wait for each other.
//
// All timestamps use one transaction clock (now()), so created_at equals
// last_used_at and each expiry is an exact offset from it. The refresh-token
// fields start in the state rotation expects: no previous hash, not revoked.
func createSession(ctx context.Context, pool *pgxpool.Pool,
	userID, passwordHash string, accessHash, refreshHash []byte, userAgent *string) (sessionID string, err error) {
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var one int
		err := tx.QueryRow(ctx,
			`SELECT 1 FROM users WHERE id = $1 AND password_hash = $2 FOR SHARE`,
			userID, passwordHash).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return errPasswordChanged
		}
		if err != nil {
			return fmt.Errorf("lock user: %w", err)
		}

		err = tx.QueryRow(ctx,
			`INSERT INTO sessions (user_id, access_token_hash, access_expires_at,
			                       refresh_token_hash, refresh_expires_at, expires_at, user_agent)
			 VALUES ($1, $2, now() + make_interval(secs => $3),
			         $4, now() + make_interval(secs => $5), now() + make_interval(secs => $6), $7)
			 RETURNING id`,
			userID, accessHash, accessTokenTTL.Seconds(),
			refreshHash, refreshTokenTTL.Seconds(), sessionMaxLifetime.Seconds(), userAgent).Scan(&sessionID)
		if err != nil {
			return fmt.Errorf("insert session: %w", err)
		}
		return nil
	})
	if errors.Is(err, errPasswordChanged) {
		return "", err
	}
	if err != nil {
		return "", fmt.Errorf("auth: create session: %w", err)
	}
	return sessionID, nil
}

// updatePasswordHash replaces the user's password hash with newHash only if
// it is still oldHash (compare-and-swap), so a transparent rehash never
// overwrites a password changed concurrently. It reports whether it wrote.
func updatePasswordHash(ctx context.Context, pool *pgxpool.Pool, userID, oldHash, newHash string) (bool, error) {
	tag, err := pool.Exec(ctx,
		`UPDATE users SET password_hash = $3, updated_at = now() WHERE id = $1 AND password_hash = $2`,
		userID, oldHash, newHash)
	if err != nil {
		return false, fmt.Errorf("auth: update password hash: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
