package profile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"vocatogether/backend/internal/testutil"
)

type fixture struct {
	pool *pgxpool.Pool
	svc  *Service
	logs *bytes.Buffer
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	pool := testutil.DB(t)
	logs := &bytes.Buffer{}
	return fixture{pool: pool, svc: NewService(pool, slog.New(slog.NewJSONHandler(logs, nil))), logs: logs}
}

func (f fixture) user(t *testing.T, addr string) string {
	t.Helper()
	var id string
	err := f.pool.QueryRow(context.Background(),
		`INSERT INTO users (email, password_hash, email_verified_at) VALUES ($1, 'x', now()) RETURNING id`, addr).Scan(&id)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func (f fixture) count(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM profiles`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestGetBeforeAnythingWasSaved(t *testing.T) {
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	if _, err := f.svc.Get(context.Background(), ana); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestSaveCreatesTheProfile(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")

	saved, err := f.svc.Save(ctx, ana, Input{DisplayName: "  Ana  López ", Bio: "Hi\r\nthere "})
	if err != nil {
		t.Fatal(err)
	}
	if saved.DisplayName != "Ana López" || saved.Bio != "Hi\nthere" {
		t.Errorf("saved = %+v, want the normalized text", saved)
	}
	if saved.CreatedAt.IsZero() || !saved.UpdatedAt.Equal(saved.CreatedAt) {
		t.Errorf("timestamps = %v, %v", saved.CreatedAt, saved.UpdatedAt)
	}

	got, err := f.svc.Get(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	if got != saved {
		t.Errorf("Get = %+v, want what Save returned %+v", got, saved)
	}
}

func TestSaveReplacesTheWholeProfile(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	first, err := f.svc.Save(ctx, ana, Input{DisplayName: "Ana", Bio: "First"})
	if err != nil {
		t.Fatal(err)
	}

	// No bio in the second save: it is cleared, not kept.
	second, err := f.svc.Save(ctx, ana, Input{DisplayName: "Ana L."})
	if err != nil {
		t.Fatal(err)
	}
	if second.DisplayName != "Ana L." || second.Bio != "" {
		t.Errorf("second = %+v", second)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("created_at changed: %v → %v", first.CreatedAt, second.CreatedAt)
	}
	if !second.UpdatedAt.After(first.UpdatedAt) {
		t.Errorf("updated_at = %v, want after %v", second.UpdatedAt, first.UpdatedAt)
	}
	if n := f.count(t); n != 1 {
		t.Errorf("profiles = %d, want 1", n)
	}
}

// rowVersion identifies the stored row version: it changes whenever the row
// is rewritten, even with the same values.
func (f fixture) rowVersion(t *testing.T, userID string) string {
	t.Helper()
	var v string
	err := f.pool.QueryRow(context.Background(),
		`SELECT xmin::text || '/' || ctid::text FROM profiles WHERE user_id = $1`, userID).Scan(&v)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// A retried save (a lost response, a 503 after the commit) is a no-op: the
// same answer, the same updated_at, no new row version and no log line, even
// when the retry is typed differently but normalizes to the same text.
func TestSaveIsIdempotent(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	first, err := f.svc.Save(ctx, ana, Input{DisplayName: "Ana", Bio: "Hi"})
	if err != nil {
		t.Fatal(err)
	}
	version := f.rowVersion(t, ana)

	for _, in := range []Input{{DisplayName: "Ana", Bio: "Hi"}, {DisplayName: " Ana ", Bio: "Hi\n"}} {
		again, err := f.svc.Save(ctx, ana, in)
		if err != nil {
			t.Fatal(err)
		}
		if again != first {
			t.Errorf("replay of %+v = %+v, want %+v unchanged", in, again, first)
		}
	}
	if got := f.rowVersion(t, ana); got != version {
		t.Errorf("an unchanged save rewrote the row: %s → %s", version, got)
	}
	if n := strings.Count(f.logs.String(), "profile: saved"); n != 1 {
		t.Errorf("%d saves logged, want 1: the replays changed nothing", n)
	}

	// A real change still writes, moves updated_at and is logged.
	changed, err := f.svc.Save(ctx, ana, Input{DisplayName: "Ana", Bio: "Hi!"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed.UpdatedAt.After(first.UpdatedAt) || !changed.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("after a change: %+v, first %+v", changed, first)
	}
	if f.rowVersion(t, ana) == version {
		t.Error("a changed save did not write")
	}
	if n := strings.Count(f.logs.String(), "profile: saved"); n != 2 {
		t.Errorf("%d saves logged, want 2", n)
	}
}

// Several identical first saves at once (a client retrying while its first
// request is still running): all succeed with the same profile, one row.
func TestConcurrentIdenticalSavesAllSucceed(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")

	const n = 16
	results := make([]Profile, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			results[i], errs[i] = f.svc.Save(ctx, ana, Input{DisplayName: "Ana", Bio: "Hi"})
		})
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
		if results[i] != results[0] {
			t.Errorf("save %d returned %+v, save 0 %+v", i, results[i], results[0])
		}
	}
	if got := f.count(t); got != 1 {
		t.Errorf("profiles = %d, want 1", got)
	}
	if n := strings.Count(f.logs.String(), "profile: saved"); n != 1 {
		t.Errorf("%d saves logged, want 1: only one of them changed anything", n)
	}
}

func TestSaveRejectsInvalidInputWithoutWriting(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	if _, err := f.svc.Save(ctx, ana, Input{DisplayName: "Ana", Bio: "Kept"}); err != nil {
		t.Fatal(err)
	}

	_, err := f.svc.Save(ctx, ana, Input{DisplayName: "", Bio: strings.Repeat("a", BioMaxLength+1)})
	var verr *ValidationError
	if !errors.As(err, &verr) || len(verr.Fields) != 2 {
		t.Fatalf("err = %v, want both fields rejected", err)
	}
	got, err := f.svc.Get(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Ana" || got.Bio != "Kept" {
		t.Errorf("profile changed by a rejected save: %+v", got)
	}
}

// Each user's profile is keyed by the ID the caller passes and nothing else.
func TestProfilesAreSeparatePerUser(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	if _, err := f.svc.Save(ctx, ana, Input{DisplayName: "Ana", Bio: "Ana's"}); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.Get(ctx, ben); !errors.Is(err, ErrNotFound) {
		t.Errorf("ben's profile before he saved one: %v, want ErrNotFound", err)
	}
	if _, err := f.svc.Save(ctx, ben, Input{DisplayName: "Ben"}); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.Get(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Ana" || got.Bio != "Ana's" {
		t.Errorf("ana's profile after ben saved his: %+v", got)
	}
}

func TestSaveForADeletedUser(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	if _, err := f.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ana); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.Save(ctx, ana, Input{DisplayName: "Ana"}); !errors.Is(err, ErrUserGone) {
		t.Errorf("err = %v, want ErrUserGone", err)
	}
	if n := f.count(t); n != 0 {
		t.Errorf("profiles = %d, want 0", n)
	}
}

func TestSaveWithAnEndedContextWritesNothing(t *testing.T) {
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := f.svc.Save(ctx, ana, Input{DisplayName: "Ana"}); !errors.Is(err, context.Canceled) {
		t.Errorf("Save err = %v, want context.Canceled", err)
	}
	if _, err := f.svc.Get(ctx, ana); !errors.Is(err, context.Canceled) {
		t.Errorf("Get err = %v, want context.Canceled", err)
	}
	if n := f.count(t); n != 0 {
		t.Errorf("profiles = %d, want 0", n)
	}
}

// Concurrent saves, including the very first ones, all succeed and leave one
// row holding one request's name and bio together, never a mix of two.
func TestConcurrentSavesLeaveOneWholeProfile(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")

	const n = 16
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			_, errs[i] = f.svc.Save(ctx, ana, Input{DisplayName: fmt.Sprintf("Name %d", i), Bio: fmt.Sprintf("Bio %d", i)})
		})
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("save %d: %v", i, err)
		}
	}
	if got := f.count(t); got != 1 {
		t.Fatalf("profiles = %d, want 1", got)
	}
	got, err := f.svc.Get(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	var a, b int
	if _, err := fmt.Sscanf(got.DisplayName, "Name %d", &a); err != nil {
		t.Fatalf("display name %q: %v", got.DisplayName, err)
	}
	if _, err := fmt.Sscanf(got.Bio, "Bio %d", &b); err != nil {
		t.Fatalf("bio %q: %v", got.Bio, err)
	}
	if a != b {
		t.Errorf("profile mixes two saves: %+v", got)
	}
	if got.UpdatedAt.Before(got.CreatedAt) {
		t.Errorf("updated_at %v before created_at %v", got.UpdatedAt, got.CreatedAt)
	}
}

// What a member writes about themselves never reaches the logs.
func TestLogsNameTheUserButNeverTheProfile(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	if _, err := f.svc.Save(ctx, ana, Input{DisplayName: "MARKERNAME", Bio: "MARKERBIO"}); err != nil {
		t.Fatal(err)
	}
	_, _ = f.svc.Save(ctx, ana, Input{DisplayName: "MARKERNAME\x00", Bio: "MARKERBIO"})
	if _, err := f.svc.Get(ctx, ana); err != nil {
		t.Fatal(err)
	}

	logs := f.logs.String()
	if !strings.Contains(logs, "profile: saved") || !strings.Contains(logs, ana) {
		t.Errorf("no save logged for the user: %s", logs)
	}
	if strings.Contains(logs, "MARKER") {
		t.Errorf("logs contain profile text: %s", logs)
	}
}

// The public identifier is assigned on the first save and never changes: not
// with an edit, not with an unchanged save, and Get returns the same one.
func TestPublicIDIsAssignedOnceAndStable(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")

	first, err := f.svc.Save(ctx, ana, Input{DisplayName: "Ana", Bio: "Hi"})
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := ParsePublicID(first.PublicID); !ok || id != first.PublicID {
		t.Fatalf("public id %q is not a canonical identifier", first.PublicID)
	}
	if first.PublicID == ana {
		t.Error("the public id is the user's id")
	}

	for name, in := range map[string]Input{
		"unchanged":   {DisplayName: "Ana", Bio: "Hi"},
		"edited":      {DisplayName: "Ana L.", Bio: ""},
		"edited back": {DisplayName: "Ana", Bio: "Hi"},
	} {
		saved, err := f.svc.Save(ctx, ana, in)
		if err != nil {
			t.Fatal(err)
		}
		if saved.PublicID != first.PublicID {
			t.Errorf("%s save: public id %s, want %s", name, saved.PublicID, first.PublicID)
		}
		got, err := f.svc.Get(ctx, ana)
		if err != nil {
			t.Fatal(err)
		}
		if got.PublicID != first.PublicID {
			t.Errorf("%s save: Get returned public id %s, want %s", name, got.PublicID, first.PublicID)
		}
	}

	other, err := f.svc.Save(ctx, ben, Input{DisplayName: "Ben"})
	if err != nil {
		t.Fatal(err)
	}
	if other.PublicID == first.PublicID {
		t.Errorf("two profiles share the public id %s", first.PublicID)
	}
}

func TestPublicFindsAProfileByItsPublicID(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	saved, err := f.svc.Save(ctx, ana, Input{DisplayName: "Ana", Bio: "Hi"})
	if err != nil {
		t.Fatal(err)
	}
	benSaved, err := f.svc.Save(ctx, ben, Input{DisplayName: "Ben"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := f.svc.Public(ctx, saved.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if want := (PublicProfile{UserID: ana, PublicID: saved.PublicID, DisplayName: "Ana", Bio: "Hi"}); got != want {
		t.Errorf("Public = %+v, want %+v", got, want)
	}
	if got, err := f.svc.Public(ctx, benSaved.PublicID); err != nil || got.UserID != ben || got.DisplayName != "Ben" || got.Bio != "" {
		t.Errorf("ben's public profile = %+v, %v", got, err)
	}

	// It shows the profile as last saved.
	if _, err := f.svc.Save(ctx, ana, Input{DisplayName: "Ana L."}); err != nil {
		t.Fatal(err)
	}
	if got, err := f.svc.Public(ctx, saved.PublicID); err != nil || got.DisplayName != "Ana L." || got.Bio != "" {
		t.Errorf("after an edit: %+v, %v", got, err)
	}
}

// Everything that is not the public id of a profile misses the same way: an
// id no profile has, a user's internal id (with or without a profile), and
// any spelling but the canonical one.
func TestPublicMisses(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com") // no profile
	saved, err := f.svc.Save(ctx, ana, Input{DisplayName: "Ana"})
	if err != nil {
		t.Fatal(err)
	}

	for name, id := range map[string]string{
		"unknown":                     "00000000-0000-4000-8000-000000000000",
		"the owner's user id":         ana,
		"a user id without a profile": ben,
		"upper case":                  strings.ToUpper(saved.PublicID),
		"braces":                      "{" + saved.PublicID + "}",
		"no hyphens":                  strings.ReplaceAll(saved.PublicID, "-", ""),
		"padded":                      " " + saved.PublicID,
		"empty":                       "",
		"not an id":                   "not-an-id",
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := f.svc.Public(ctx, id); !errors.Is(err, ErrNotFound) || got != (PublicProfile{}) {
				t.Errorf("Public(%q) = %+v, %v; want ErrNotFound", id, got, err)
			}
		})
	}
}

// A malformed identifier is answered without asking the database: it misses
// even when no query could run.
func TestPublicDoesNotQueryForAMalformedID(t *testing.T) {
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	saved, err := f.svc.Save(context.Background(), ana, Input{DisplayName: "Ana"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := f.svc.Public(ctx, strings.ToUpper(saved.PublicID)); !errors.Is(err, ErrNotFound) {
		t.Errorf("malformed id with an ended context: %v, want ErrNotFound", err)
	}
	// A well-formed one does reach the database, so the ended context shows.
	if _, err := f.svc.Public(ctx, saved.PublicID); !errors.Is(err, context.Canceled) {
		t.Errorf("well-formed id with an ended context: %v, want context.Canceled", err)
	}
}

func TestPublicMissesAfterTheUserIsDeleted(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	saved, err := f.svc.Save(ctx, ana, Input{DisplayName: "Ana"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Public(ctx, saved.PublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ana); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Public(ctx, saved.PublicID); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// Reading a public profile logs nothing: not the id asked for, not the text.
func TestPublicLogsNothing(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	saved, err := f.svc.Save(ctx, ana, Input{DisplayName: "MARKERNAME", Bio: "MARKERBIO"})
	if err != nil {
		t.Fatal(err)
	}
	f.logs.Reset()

	if _, err := f.svc.Public(ctx, saved.PublicID); err != nil {
		t.Fatal(err)
	}
	_, _ = f.svc.Public(ctx, "00000000-0000-4000-8000-000000000000")
	_, _ = f.svc.Public(ctx, "MARKERID")
	if logs := f.logs.String(); logs != "" {
		t.Errorf("public reads logged: %s", logs)
	}
}
