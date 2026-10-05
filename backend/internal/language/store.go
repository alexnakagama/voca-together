package language

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// querier is what a read needs: the pool, or a transaction when the read
// must see what the transaction holds.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// listCatalog returns every language of the catalog, ordered by name.
func listCatalog(ctx context.Context, q querier) ([]Language, error) {
	rows, err := q.Query(ctx, `SELECT code, name, endonym FROM languages ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list catalog: %w", err)
	}
	catalog, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Language, error) {
		var l Language
		err := row.Scan(&l.Code, &l.Name, &l.Endonym)
		return l, err
	})
	if err != nil {
		return nil, fmt.Errorf("list catalog: %w", err)
	}
	return catalog, nil
}

// findSelection returns the languages of userID, each list in its stored
// order; both are empty if there is no row. One plain read on the primary
// key, without a lock.
func findSelection(ctx context.Context, q querier, userID string) (Selection, error) {
	rows, err := q.Query(ctx,
		`SELECT language_code, kind, level FROM user_languages WHERE user_id = $1 ORDER BY kind, position`,
		userID)
	if err != nil {
		return Selection{}, fmt.Errorf("find selection: %w", err)
	}
	var (
		s    Selection
		code Code
		kind Kind
		rank int16
	)
	_, err = pgx.ForEachRow(rows, []any{&code, &kind, &rank}, func() error {
		level, ok := levelFromRank(int(rank))
		if !ok {
			return errors.New("a stored level is off the scale")
		}
		switch kind {
		case KindSpoken:
			s.Spoken = append(s.Spoken, Entry{Code: code, Level: level})
		case KindLearning:
			s.Learning = append(s.Learning, Entry{Code: code, Level: level})
		default:
			return errors.New("a stored kind is not a kind")
		}
		return nil
	})
	if err != nil {
		return Selection{}, fmt.Errorf("find selection: %w", err)
	}
	return s, nil
}

// replaceSelection makes want, already parsed, all the languages of userID.
// changed is false when the user already had exactly that selection: nothing
// is written then. It returns ErrUserGone if the user doesn't exist, and a
// *ValidationError, having written nothing, if want holds a language that
// isn't in the catalog.
//
// A set can't be compared and replaced in one statement, so this is one
// transaction, and the first thing it does is lock the users row FOR NO KEY
// UPDATE. That makes one user's saves run in turn: each sees what the one
// before committed, and the rows always hold one request's whole selection.
// The catalog is checked inside the transaction, and the rows inserted then
// hold their languages FOR KEY SHARE, so none can leave the catalog between
// the check and the commit.
//
// Locks: the users row comes first, which is the order of every auth
// transaction (they take it FOR UPDATE), so this waits for them and they for
// it, without a deadlock. FOR NO KEY UPDATE doesn't block the FOR KEY SHARE
// that inserting a row referencing the user takes (a session, a profile).
// After it come only this user's own user_languages rows and key-share locks
// on the catalog, which nothing updates.
func replaceSelection(ctx context.Context, pool *pgxpool.Pool, userID string, want Selection) (changed bool, err error) {
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var one int
		err := tx.QueryRow(ctx, `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`, userID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserGone
		}
		if err != nil {
			return fmt.Errorf("lock user: %w", err)
		}

		var (
			codes     []string
			kinds     []string
			levels    []int16
			positions []int16
		)
		for _, list := range []struct {
			kind    Kind
			entries []Entry
		}{
			{KindSpoken, want.Spoken},
			{KindLearning, want.Learning},
		} {
			for i, e := range list.entries {
				codes = append(codes, string(e.Code))
				kinds = append(kinds, string(list.kind))
				levels = append(levels, int16(e.Level))
				positions = append(positions, int16(i))
			}
		}

		if len(codes) > 0 {
			known, err := knownCodes(ctx, tx, codes)
			if err != nil {
				return err
			}
			if err := want.checkKnown(func(c Code) bool { return known[c] }); err != nil {
				return err
			}
		}

		current, err := findSelection(ctx, tx, userID)
		if err != nil {
			return err
		}
		if current.Equal(want) {
			return nil
		}

		if _, err := tx.Exec(ctx, `DELETE FROM user_languages WHERE user_id = $1`, userID); err != nil {
			return fmt.Errorf("delete selection: %w", err)
		}
		if len(codes) > 0 {
			_, err := tx.Exec(ctx,
				`INSERT INTO user_languages (user_id, language_code, kind, level, position)
				 SELECT $1, * FROM unnest($2::text[], $3::text[], $4::smallint[], $5::smallint[])`,
				userID, codes, kinds, levels, positions)
			if err != nil {
				return fmt.Errorf("insert selection: %w", err)
			}
		}
		changed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}

// knownCodes returns which of codes are in the catalog.
func knownCodes(ctx context.Context, q querier, codes []string) (map[Code]bool, error) {
	rows, err := q.Query(ctx, `SELECT code FROM languages WHERE code = ANY($1)`, codes)
	if err != nil {
		return nil, fmt.Errorf("check catalog: %w", err)
	}
	known := make(map[Code]bool, len(codes))
	var code Code
	_, err = pgx.ForEachRow(rows, []any{&code}, func() error {
		known[code] = true
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("check catalog: %w", err)
	}
	return known, nil
}
