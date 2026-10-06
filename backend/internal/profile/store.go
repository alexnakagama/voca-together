package profile

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
	userForeignKey      = "profiles_user_id_fkey"
)

// findProfile returns the profile of userID; found=false if there is none.
// One plain read on the primary key, without a lock.
func findProfile(ctx context.Context, pool *pgxpool.Pool, userID string) (p Profile, found bool, err error) {
	err = pool.QueryRow(ctx,
		`SELECT public_id, display_name, bio, created_at, updated_at FROM profiles WHERE user_id = $1`,
		userID).Scan(&p.PublicID, &p.DisplayName, &p.Bio, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, false, nil
	}
	if err != nil {
		return Profile{}, false, fmt.Errorf("find profile: %w", err)
	}
	return p, true, nil
}

// findPublicProfile returns the profile whose public id is publicID, which
// must be well formed (ParsePublicID); found=false if no profile has it. One
// plain read on the unique index, without a lock.
func findPublicProfile(ctx context.Context, pool *pgxpool.Pool, publicID string) (p PublicProfile, found bool, err error) {
	err = pool.QueryRow(ctx,
		`SELECT user_id, public_id, display_name, bio FROM profiles WHERE public_id = $1`,
		publicID).Scan(&p.UserID, &p.PublicID, &p.DisplayName, &p.Bio)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicProfile{}, false, nil
	}
	if err != nil {
		return PublicProfile{}, false, fmt.Errorf("find public profile: %w", err)
	}
	return p, true, nil
}

// upsertProfile makes in, already normalized, the whole profile of userID
// and returns the stored row. changed is false when the profile already held
// exactly that text: nothing is written then, so updated_at keeps meaning
// "last change". It returns ErrUserGone if the user doesn't exist.
//
// The write is a single statement: two first saves can't both insert (the
// loser of the race updates instead), concurrent updates take the row lock in
// turn, and each writes both columns, so the row always holds one request's
// text. public_id is set by its default on the insert and never written
// again. When the text is the same the statement only locks the row and
// returns nothing; the row is then read back, which shows the profile as it
// is at that moment (after a concurrent save, the newer text).
//
// Locks: inserting takes FOR KEY SHARE on the users row for the foreign key,
// which can wait for an auth transaction holding it FOR UPDATE, and nothing
// after it, so it can't be part of a deadlock. Updating doesn't touch users.
func upsertProfile(ctx context.Context, pool *pgxpool.Pool, userID string, in Input) (p Profile, changed bool, err error) {
	err = pool.QueryRow(ctx,
		`INSERT INTO profiles (user_id, display_name, bio) VALUES ($1, $2, $3)
		 ON CONFLICT (user_id) DO UPDATE SET
		     display_name = EXCLUDED.display_name,
		     bio          = EXCLUDED.bio,
		     updated_at   = now()
		 WHERE (profiles.display_name, profiles.bio)
		       IS DISTINCT FROM (EXCLUDED.display_name, EXCLUDED.bio)
		 RETURNING public_id, display_name, bio, created_at, updated_at`,
		userID, in.DisplayName, in.Bio).Scan(&p.PublicID, &p.DisplayName, &p.Bio, &p.CreatedAt, &p.UpdatedAt)
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return p, true, nil
	case errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation && pgErr.ConstraintName == userForeignKey:
		return Profile{}, false, ErrUserGone
	case !errors.Is(err, pgx.ErrNoRows):
		return Profile{}, false, fmt.Errorf("upsert profile: %w", err)
	}

	// Unchanged: the profile exists with this text.
	p, found, err := findProfile(ctx, pool, userID)
	if err != nil {
		return Profile{}, false, err
	}
	if !found {
		// It existed a moment ago; only deleting the user removes it.
		return Profile{}, false, ErrUserGone
	}
	return p, false, nil
}
