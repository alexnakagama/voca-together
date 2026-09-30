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
const (
	PurposeEmailVerification = "email_verification"
	PurposePasswordReset     = "password_reset"
)

// Lock order: a transaction that changes user_tokens or sessions rows of an existing user
// must first lock that user's row (SELECT … FOR UPDATE on users). Every token
// flow (verification, resend, forgot and reset password) follows it, so two
// flows on one account serialize on the user row instead of deadlocking on
// user and token rows taken in opposite orders. Registration is exempt: the
// user row it creates is invisible to others until commit. Login takes the
// user row FOR SHARE (see createSession), so logins don't block each other
// but do serialize with anything that changes the password. Refresh and
// logout are also exempt: each locks a single session row and nothing else,
// so neither can be part of a deadlock cycle (see rotateRefreshToken and
// revokeSessionByAccessToken). Authentication takes no locks at all: it is
// a single read (see findSessionByAccessToken).

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

// issuePasswordResetToken replaces the password reset token of the account
// with the given email, verified or not (decision 017), and returns its id.
// It returns issued=false, having changed nothing, if there is no such account.
func issuePasswordResetToken(ctx context.Context, pool *pgxpool.Pool,
	email string, tokenHash []byte, ttl time.Duration) (userID string, issued bool, err error) {
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		userID, issued, err = issuePasswordResetTokenTx(ctx, tx, email, tokenHash, ttl)
		return err
	})
	if err != nil {
		return "", false, fmt.Errorf("auth: issue password reset token: %w", err)
	}
	return userID, issued, nil
}

func issuePasswordResetTokenTx(ctx context.Context, tx pgx.Tx,
	email string, tokenHash []byte, ttl time.Duration) (string, bool, error) {
	var userID string
	err := tx.QueryRow(ctx, `SELECT id FROM users WHERE email = $1 FOR UPDATE`, email).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("lock user: %w", err)
	}
	if err := issueToken(ctx, tx, userID, PurposePasswordReset, tokenHash, ttl); err != nil {
		return "", false, err
	}
	return userID, true, nil
}

// findPasswordResetOwner returns the email of the account whose password
// reset token has the given hash, if that token is usable now: not used,
// replaced or expired. It only reads, without locking: resetPasswordTx
// decides under the user lock. Its purpose is to reject dead tokens before
// any argon2 work and to learn the email for the password policy.
func findPasswordResetOwner(ctx context.Context, pool *pgxpool.Pool, tokenHash []byte) (email string, found bool, err error) {
	err = pool.QueryRow(ctx,
		`SELECT u.email FROM user_tokens t JOIN users u ON u.id = t.user_id
		 WHERE t.token_hash = $1 AND t.purpose = $2 AND t.used_at IS NULL AND t.expires_at > now()`,
		tokenHash, PurposePasswordReset).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("auth: find password reset owner: %w", err)
	}
	return email, true, nil
}

// passwordReset reports what resetPassword did. userID and email are set only
// when ok.
type passwordReset struct {
	ok              bool
	userID, email   string
	sessionsRevoked int64
}

// resetPassword consumes the password reset token with the given hash and,
// in the same transaction, replaces its owner's password hash with newHash,
// revokes all of the owner's sessions and deletes the owner's other unused
// one-time tokens (decision 017). Completing a reset proves control of the
// mailbox, so an unverified account becomes verified. It returns ok=false,
// having changed nothing, if the token is not usable: unknown, wrong purpose,
// used, replaced or expired.
//
// The new password takes effect at commit, together with everything else:
// no other transaction sees the new hash with live old sessions, or an
// unused token with the new hash.
func resetPassword(ctx context.Context, pool *pgxpool.Pool, tokenHash []byte, newHash string) (r passwordReset, err error) {
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		r, err = resetPasswordTx(ctx, tx, tokenHash, newHash)
		return err
	})
	if err != nil {
		return passwordReset{}, fmt.Errorf("auth: consume password reset token: %w", err)
	}
	return r, nil
}

func resetPasswordTx(ctx context.Context, tx pgx.Tx, tokenHash []byte, newHash string) (passwordReset, error) {
	// Only takes the user lock (see Lock order); the UPDATE below decides.
	var userID, email string
	err := tx.QueryRow(ctx,
		`SELECT u.id, u.email FROM user_tokens t JOIN users u ON u.id = t.user_id
		 WHERE t.token_hash = $1 AND t.purpose = $2
		 FOR UPDATE OF u`,
		tokenHash, PurposePasswordReset).Scan(&userID, &email)
	if errors.Is(err, pgx.ErrNoRows) {
		return passwordReset{}, nil
	}
	if err != nil {
		return passwordReset{}, fmt.Errorf("lock user: %w", err)
	}

	// Single use (decision 004). The statement sees rows committed while it
	// waited for the lock above, so a concurrent reset with the same token or
	// a replacement by forgot-password makes it match nothing.
	tag, err := tx.Exec(ctx,
		`UPDATE user_tokens SET used_at = now()
		 WHERE token_hash = $1 AND purpose = $2 AND user_id = $3
		   AND used_at IS NULL AND expires_at > now()`,
		tokenHash, PurposePasswordReset, userID)
	if err != nil {
		return passwordReset{}, fmt.Errorf("use token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return passwordReset{}, nil
	}

	// A login that verified the old hash re-checks it under FOR SHARE (see
	// createSession), so it either finished before this lock, and its session
	// is revoked below, or waits and then sees the new hash.
	if _, err := tx.Exec(ctx,
		`UPDATE users SET password_hash = $2, email_verified_at = COALESCE(email_verified_at, now()), updated_at = now()
		 WHERE id = $1`,
		userID, newHash); err != nil {
		return passwordReset{}, fmt.Errorf("update password: %w", err)
	}

	// A new statement: it sees every session committed before it started,
	// including one a login committed while this transaction waited for the
	// user lock. A session locked by a refresh or logout is waited for and
	// re-checked against its committed version.
	tag, err = tx.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	if err != nil {
		return passwordReset{}, fmt.Errorf("revoke sessions: %w", err)
	}
	revoked := tag.RowsAffected()

	// No other one-time secret (e.g. a pending verification link) outlives
	// the account's recovery. The consumed token is kept, as used.
	if _, err := tx.Exec(ctx,
		`DELETE FROM user_tokens WHERE user_id = $1 AND used_at IS NULL`, userID); err != nil {
		return passwordReset{}, fmt.Errorf("delete unused tokens: %w", err)
	}
	return passwordReset{ok: true, userID: userID, email: email, sessionsRevoked: revoked}, nil
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
// A password reset (which revokes all sessions, see resetPasswordTx) holding
// the row FOR UPDATE makes this wait and then see the new hash, so no session
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

// refreshOutcome is what rotateRefreshToken did with a presented token.
type refreshOutcome int

const (
	refreshRotated        refreshOutcome = iota // new tokens stored
	refreshUnknown                              // no session has this token
	refreshRevoked                              // session already revoked
	refreshReused                               // an already-rotated token: session revoked now
	refreshSessionExpired                       // absolute lifetime over (or under minAccessTokenLifetime left)
	refreshTokenExpired                         // sliding refresh expiry passed
)

// refreshReason names each outcome for logs.
var refreshReason = map[refreshOutcome]string{
	refreshUnknown:        "unknown",
	refreshRevoked:        "revoked",
	refreshReused:         "reused",
	refreshSessionExpired: "session_expired",
	refreshTokenExpired:   "refresh_expired",
}

// rotation reports the result of rotateRefreshToken. sessionID and userID
// are set whenever a session matched; expiresIn only when rotated.
type rotation struct {
	outcome           refreshOutcome
	sessionID, userID string
	expiresIn         time.Duration
}

// rotateRefreshToken validates the refresh token with hash presented and, if
// it is the current token of a live session, replaces both token hashes with
// accessHash and refreshHash in one transaction (decision 002). The old
// access token stops working at once. The rotated-out refresh hash is kept
// as previous_refresh_token_hash; presenting it again revokes the session
// (reuse detection), which is committed although no tokens are issued.
// Nothing else is written for an unusable token.
//
// The session row is locked FOR UPDATE, so of two requests presenting the
// same token only one rotates. The other waits, and PostgreSQL re-evaluates
// its WHERE against the committed row: it now matches through
// previous_refresh_token_hash, so it is treated as reuse (strict policy,
// decision 014). If the first rolls back, the second sees the original row.
//
// Both new expiries are capped at the session's absolute expires_at (the
// sessions_expiry_order CHECK enforces it). A session with less than
// minAccessTokenLifetime left is treated as expired. All times use the
// transaction clock.
func rotateRefreshToken(ctx context.Context, pool *pgxpool.Pool, presented, accessHash, refreshHash []byte) (r rotation, err error) {
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		r, err = rotateRefreshTokenTx(ctx, tx, presented, accessHash, refreshHash)
		return err
	})
	if err != nil {
		return rotation{}, fmt.Errorf("auth: rotate refresh token: %w", err)
	}
	return r, nil
}

func rotateRefreshTokenTx(ctx context.Context, tx pgx.Tx, presented, accessHash, refreshHash []byte) (rotation, error) {
	var r rotation
	var current, revoked, sessionExpired, refreshExpired bool
	err := tx.QueryRow(ctx,
		`SELECT id, user_id, refresh_token_hash = $1, revoked_at IS NOT NULL,
		        expires_at <= now() + make_interval(secs => $2), refresh_expires_at <= now()
		 FROM sessions
		 WHERE refresh_token_hash = $1 OR previous_refresh_token_hash = $1
		 FOR UPDATE`,
		presented, minAccessTokenLifetime.Seconds()).
		Scan(&r.sessionID, &r.userID, &current, &revoked, &sessionExpired, &refreshExpired)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		r.outcome = refreshUnknown
		return r, nil
	case err != nil:
		return rotation{}, fmt.Errorf("lock session: %w", err)
	}

	// Order matters: reuse is detected before expiry, so a replayed token
	// revokes the session even if it has expired (a signal worth logging).
	switch {
	case revoked:
		r.outcome = refreshRevoked
		return r, nil
	case !current:
		if _, err := tx.Exec(ctx,
			`UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`,
			r.sessionID); err != nil {
			return rotation{}, fmt.Errorf("revoke session: %w", err)
		}
		r.outcome = refreshReused
		return r, nil
	case sessionExpired:
		r.outcome = refreshSessionExpired
		return r, nil
	case refreshExpired:
		r.outcome = refreshTokenExpired
		return r, nil
	}

	var accessExpiresAt, now time.Time
	err = tx.QueryRow(ctx,
		`UPDATE sessions SET
		     previous_refresh_token_hash = refresh_token_hash,
		     refresh_token_hash = $2,
		     refresh_expires_at = LEAST(now() + make_interval(secs => $3), expires_at),
		     access_token_hash  = $4,
		     access_expires_at  = LEAST(now() + make_interval(secs => $5), expires_at),
		     last_used_at       = now()
		 WHERE id = $1
		 RETURNING access_expires_at, now()`,
		r.sessionID, refreshHash, refreshTokenTTL.Seconds(), accessHash, accessTokenTTL.Seconds()).
		Scan(&accessExpiresAt, &now)
	if err != nil {
		return rotation{}, fmt.Errorf("rotate tokens: %w", err)
	}
	r.outcome = refreshRotated
	r.expiresIn = accessExpiresAt.Sub(now)
	return r, nil
}

// revokeSessionByAccessToken revokes the live session whose current access
// token hash is accessHash, which kills its access and refresh tokens alike
// (decision 015). It reports the session it revoked; revoked=false, with
// nothing written, if none matched: unknown, rotated out, or already revoked
// (whose original revocation time is kept). Expiry is deliberately not
// checked: logging out only reduces privilege.
//
// The single UPDATE locks the row it changes. If a refresh holds the row, it
// waits and re-evaluates its WHERE against the committed row: after a
// rotation the old access hash no longer matches, and after a rollback the
// original row is revoked.
func revokeSessionByAccessToken(ctx context.Context, pool *pgxpool.Pool, accessHash []byte) (sessionID, userID string, revoked bool, err error) {
	err = pool.QueryRow(ctx,
		`UPDATE sessions SET revoked_at = now()
		 WHERE access_token_hash = $1 AND revoked_at IS NULL
		 RETURNING id, user_id`,
		accessHash).Scan(&sessionID, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("auth: revoke session: %w", err)
	}
	return sessionID, userID, true, nil
}

// findSessionByAccessToken returns the identity of the live session whose
// current access token hash is accessHash: not revoked, access token not
// expired, and within the absolute lifetime (implied by the
// sessions_expiry_order CHECK, but stated so the rule doesn't rest on it).
// found=false if none matches, including a token rotated out by refresh.
//
// One plain SELECT on the unique access_token_hash index, without a lock:
// it sees the last committed row, so a concurrent refresh or logout neither
// blocks it nor leaves it seeing a half-written session.
func findSessionByAccessToken(ctx context.Context, pool *pgxpool.Pool, accessHash []byte) (id Identity, found bool, err error) {
	err = pool.QueryRow(ctx,
		`SELECT id, user_id FROM sessions
		 WHERE access_token_hash = $1 AND revoked_at IS NULL
		   AND access_expires_at > now() AND expires_at > now()`,
		accessHash).Scan(&id.SessionID, &id.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Identity{}, false, nil
	}
	if err != nil {
		return Identity{}, false, fmt.Errorf("auth: find session: %w", err)
	}
	return id, true, nil
}

// findUserByID returns the user's public account data; the password hash is
// never selected.
func findUserByID(ctx context.Context, pool *pgxpool.Pool, userID string) (u User, found bool, err error) {
	err = pool.QueryRow(ctx,
		`SELECT id, email, email_verified_at, created_at FROM users WHERE id = $1`,
		userID).Scan(&u.ID, &u.Email, &u.EmailVerifiedAt, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, fmt.Errorf("auth: find user by id: %w", err)
	}
	return u, true, nil
}

// Cleanup deletes (see Service.cleanup) take the rows they delete with
// FOR UPDATE SKIP LOCKED: they never wait for a lock, so they can't be part of
// a deadlock cycle with the token flows (which lock the user row, then token
// rows) or with refresh and logout (one session row). A locked row is skipped
// and deleted by a later run. Deleting a referencing row locks nothing in
// users.

// deleteDeadSessions deletes up to limit sessions that ended (absolute
// expiry, sliding refresh expiry or revocation) more than retention ago.
func deleteDeadSessions(ctx context.Context, pool *pgxpool.Pool, retention time.Duration, limit int) (int64, error) {
	tag, err := pool.Exec(ctx,
		`DELETE FROM sessions WHERE id IN (
		     SELECT id FROM sessions
		     WHERE expires_at < now() - make_interval(secs => $1)
		        OR refresh_expires_at < now() - make_interval(secs => $1)
		        OR revoked_at < now() - make_interval(secs => $1)
		     LIMIT $2
		     FOR UPDATE SKIP LOCKED)`,
		retention.Seconds(), limit)
	if err != nil {
		return 0, fmt.Errorf("delete sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// deleteDeadTokens deletes up to limit one-time tokens, used or not, that
// expired more than retention ago. A used token is never usable again, so
// its expiry bounds how long it is kept as an audit record.
func deleteDeadTokens(ctx context.Context, pool *pgxpool.Pool, retention time.Duration, limit int) (int64, error) {
	tag, err := pool.Exec(ctx,
		`DELETE FROM user_tokens WHERE id IN (
		     SELECT id FROM user_tokens
		     WHERE expires_at < now() - make_interval(secs => $1)
		     LIMIT $2
		     FOR UPDATE SKIP LOCKED)`,
		retention.Seconds(), limit)
	if err != nil {
		return 0, fmt.Errorf("delete tokens: %w", err)
	}
	return tag.RowsAffected(), nil
}
