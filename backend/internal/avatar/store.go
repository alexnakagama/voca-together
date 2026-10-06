package avatar

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
	userForeignKey      = "avatars_user_id_fkey"
)

// findImage returns the picture of userID; found=false if there is none. One
// plain read on the primary key, without a lock.
func findImage(ctx context.Context, pool *pgxpool.Pool, userID string) (image []byte, found bool, err error) {
	err = pool.QueryRow(ctx, `SELECT image FROM avatars WHERE user_id = $1`, userID).Scan(&image)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("find image: %w", err)
	}
	return image, true, nil
}

// findImageOfSource returns the picture of userID if it was made from the
// upload whose SHA-256 is source; found=false if they have none or it was
// made from something else.
func findImageOfSource(ctx context.Context, pool *pgxpool.Pool, userID string, source []byte) (image []byte, found bool, err error) {
	err = pool.QueryRow(ctx,
		`SELECT image FROM avatars WHERE user_id = $1 AND source_sha256 = $2`, userID, source).Scan(&image)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("find image of source: %w", err)
	}
	return image, true, nil
}

// imageExists reports whether userID has a picture, without reading it.
func imageExists(ctx context.Context, pool *pgxpool.Pool, userID string) (exists bool, err error) {
	err = pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM avatars WHERE user_id = $1)`, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("image exists: %w", err)
	}
	return exists, nil
}

// upsertImage makes image, produced from the upload whose SHA-256 is source,
// the picture of userID. changed is false when their picture was already
// made from that upload: nothing is written then, and since the same upload
// always gives the same picture, image is what the row holds. It returns
// ErrUserGone if the user doesn't exist.
//
// The write is a single statement: two first uploads can't both insert (the
// loser of the race updates instead), concurrent uploads take the row lock
// in turn, and each writes the picture and its hash together, so the row
// always holds one upload's.
//
// Locks: inserting takes FOR KEY SHARE on the users row for the foreign key,
// which can wait for an auth transaction holding it FOR UPDATE, and nothing
// after it, so it can't be part of a deadlock. Updating doesn't touch users.
func upsertImage(ctx context.Context, pool *pgxpool.Pool, userID string, image, source []byte) (changed bool, err error) {
	tag, err := pool.Exec(ctx,
		`INSERT INTO avatars (user_id, image, source_sha256) VALUES ($1, $2, $3)
		 ON CONFLICT (user_id) DO UPDATE SET
		     image         = EXCLUDED.image,
		     source_sha256 = EXCLUDED.source_sha256,
		     updated_at    = now()
		 WHERE avatars.source_sha256 IS DISTINCT FROM EXCLUDED.source_sha256`,
		userID, image, source)
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation && pgErr.ConstraintName == userForeignKey:
		return false, ErrUserGone
	case err != nil:
		return false, fmt.Errorf("upsert image: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// deleteImage removes the picture of userID; removed=false if there was
// none.
func deleteImage(ctx context.Context, pool *pgxpool.Pool, userID string) (removed bool, err error) {
	tag, err := pool.Exec(ctx, `DELETE FROM avatars WHERE user_id = $1`, userID)
	if err != nil {
		return false, fmt.Errorf("delete image: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
