package language

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM user_languages`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// stored returns the rows of userID as "kind position code level", in the
// stored order, spoken first.
func (f fixture) stored(t *testing.T, userID string) []string {
	t.Helper()
	var rows []string
	err := f.pool.QueryRow(context.Background(),
		`SELECT coalesce(array_agg(kind || ' ' || position || ' ' || language_code || ' ' || level
		                           ORDER BY kind DESC, position), '{}')
		 FROM user_languages WHERE user_id = $1`, userID).Scan(&rows)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// rowVersions identifies the stored row versions of userID: it changes
// whenever a row is rewritten, even with the same values.
func (f fixture) rowVersions(t *testing.T, userID string) string {
	t.Helper()
	var v string
	err := f.pool.QueryRow(context.Background(),
		`SELECT coalesce(string_agg(xmin::text || '/' || ctid::text, ' ' ORDER BY language_code), '')
		 FROM user_languages WHERE user_id = $1`, userID).Scan(&v)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (f fixture) savesLogged() int {
	return strings.Count(f.logs.String(), "languages: saved")
}

// mustParse returns in as the Selection saving it stores.
func mustParse(t *testing.T, in Input) Selection {
	t.Helper()
	s, err := parse(in)
	if err != nil {
		t.Fatalf("parse(%+v): %v", in, err)
	}
	return s
}

func TestCatalog(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	catalog, err := f.svc.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM languages`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if len(catalog) != rows || rows == 0 {
		t.Fatalf("catalog has %d languages, the table %d", len(catalog), rows)
	}
	if !slices.IsSortedFunc(catalog, func(a, b Language) int { return strings.Compare(a.Name, b.Name) }) {
		t.Error("the catalog is not ordered by name")
	}
	byCode := make(map[Code]Language, len(catalog))
	for _, l := range catalog {
		if _, ok := parseCode(string(l.Code)); !ok {
			t.Errorf("the catalog holds a code that is not well formed: %q", l.Code)
		}
		if l.Name == "" || l.Endonym == "" {
			t.Errorf("%q has no name or endonym: %+v", l.Code, l)
		}
		byCode[l.Code] = l
	}
	if len(byCode) != len(catalog) {
		t.Errorf("%d distinct codes in %d languages", len(byCode), len(catalog))
	}
	if got, want := byCode["es"], (Language{Code: "es", Name: "Spanish", Endonym: "Español"}); got != want {
		t.Errorf("es = %+v, want %+v", got, want)
	}
	for _, code := range []Code{"en", "zh", "yue", "fil"} {
		if _, ok := byCode[code]; !ok {
			t.Errorf("%q is not in the catalog", code)
		}
	}
}

func TestGetBeforeAnythingWasSaved(t *testing.T) {
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	got, err := f.svc.Get(context.Background(), ana)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Spoken) != 0 || len(got.Learning) != 0 {
		t.Errorf("Get = %+v, want two empty lists", got)
	}
}

func TestSaveCreatesTheSelection(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	in := Input{Spoken: entries("es", "native", "en", "c1"), Learning: entries("ja", "a2")}

	saved, err := f.svc.Save(ctx, ana, in)
	if err != nil {
		t.Fatal(err)
	}
	if want := mustParse(t, in); !saved.Equal(want) {
		t.Errorf("saved = %+v, want %+v", saved, want)
	}
	got, err := f.svc.Get(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(saved) {
		t.Errorf("Get = %+v, want what Save returned %+v", got, saved)
	}
	want := []string{"spoken 0 es 7", "spoken 1 en 5", "learning 0 ja 2"}
	if rows := f.stored(t, ana); !slices.Equal(rows, want) {
		t.Errorf("rows = %q, want %q", rows, want)
	}
}

// A save replaces everything: languages left out are removed, a language can
// change list, and the order given becomes the stored order.
func TestSaveReplacesTheWholeSelection(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	if _, err := f.svc.Save(ctx, ana, Input{Spoken: entries("es", "native", "en", "c1"), Learning: entries("ja", "a2", "fr", "b1")}); err != nil {
		t.Fatal(err)
	}

	in := Input{Spoken: entries("en", "c2", "es", "native", "fr", "b2"), Learning: entries("de", "a1")}
	saved, err := f.svc.Save(ctx, ana, in)
	if err != nil {
		t.Fatal(err)
	}
	if want := mustParse(t, in); !saved.Equal(want) {
		t.Errorf("saved = %+v, want %+v", saved, want)
	}
	want := []string{"spoken 0 en 6", "spoken 1 es 7", "spoken 2 fr 4", "learning 0 de 1"}
	if rows := f.stored(t, ana); !slices.Equal(rows, want) {
		t.Errorf("rows = %q, want %q", rows, want)
	}
	if n := f.savesLogged(); n != 2 {
		t.Errorf("%d saves logged, want 2", n)
	}
}

// Order is part of the selection: the same languages in another order is a
// change.
func TestSaveReorders(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	if _, err := f.svc.Save(ctx, ana, Input{Spoken: entries("es", "native", "en", "c1")}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Save(ctx, ana, Input{Spoken: entries("en", "c1", "es", "native")}); err != nil {
		t.Fatal(err)
	}
	want := []string{"spoken 0 en 5", "spoken 1 es 7"}
	if rows := f.stored(t, ana); !slices.Equal(rows, want) {
		t.Errorf("rows = %q, want %q", rows, want)
	}
}

func TestSaveAcceptsTheMaximumOfEachKind(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	in := Input{
		Spoken:   entries("es", "native", "en", "c1", "fr", "b2", "de", "b1", "it", "a2"),
		Learning: entries("ja", "a1", "ko", "a1", "zh", "a2", "yue", "b1", "pt", "c2"),
	}
	if _, err := f.svc.Save(ctx, ana, in); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.Get(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	if want := mustParse(t, in); !got.Equal(want) {
		t.Errorf("Get = %+v, want %+v", got, want)
	}
	if n := f.count(t); n != 2*MaxPerKind {
		t.Errorf("rows = %d, want %d", n, 2*MaxPerKind)
	}
}

// Nothing is required, so two empty lists are a valid save: they remove the
// languages the member had.
func TestSaveOfEmptyListsRemovesTheLanguages(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")

	// With nothing saved there is nothing to remove, so nothing changes.
	if _, err := f.svc.Save(ctx, ana, Input{}); err != nil {
		t.Fatal(err)
	}
	if n := f.savesLogged(); n != 0 {
		t.Errorf("%d saves logged for an empty save of nothing, want 0", n)
	}

	if _, err := f.svc.Save(ctx, ana, Input{Spoken: entries("es", "native"), Learning: entries("ja", "a2")}); err != nil {
		t.Fatal(err)
	}
	saved, err := f.svc.Save(ctx, ana, Input{})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Equal(Selection{}) {
		t.Errorf("saved = %+v, want nothing", saved)
	}
	if n := f.count(t); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
	if n := f.savesLogged(); n != 2 {
		t.Errorf("%d saves logged, want 2", n)
	}
}

// A retried save (a lost response, a 503 after the commit) is a no-op: the
// same answer, no new row version and no log line.
func TestSaveIsIdempotent(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	in := Input{Spoken: entries("es", "native", "en", "c1"), Learning: entries("ja", "a2")}
	first, err := f.svc.Save(ctx, ana, in)
	if err != nil {
		t.Fatal(err)
	}
	versions := f.rowVersions(t, ana)

	for range 2 {
		again, err := f.svc.Save(ctx, ana, in)
		if err != nil {
			t.Fatal(err)
		}
		if !again.Equal(first) {
			t.Errorf("replay = %+v, want %+v unchanged", again, first)
		}
	}
	if got := f.rowVersions(t, ana); got != versions {
		t.Errorf("an unchanged save rewrote the rows: %s → %s", versions, got)
	}
	if n := f.savesLogged(); n != 1 {
		t.Errorf("%d saves logged, want 1: the replays changed nothing", n)
	}

	// A real change, even of one level, still writes and is logged.
	if _, err := f.svc.Save(ctx, ana, Input{Spoken: entries("es", "native", "en", "c2"), Learning: entries("ja", "a2")}); err != nil {
		t.Fatal(err)
	}
	if f.rowVersions(t, ana) == versions {
		t.Error("a changed save did not write")
	}
	if n := f.savesLogged(); n != 2 {
		t.Errorf("%d saves logged, want 2", n)
	}
}

func TestSaveRejectsInvalidInputWithoutWriting(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	kept := Input{Spoken: entries("es", "native"), Learning: entries("ja", "a2")}
	if _, err := f.svc.Save(ctx, ana, kept); err != nil {
		t.Fatal(err)
	}
	versions := f.rowVersions(t, ana)

	tests := []struct {
		name string
		in   Input
		want []FieldError
	}{
		{
			"rules of the lists",
			Input{Spoken: entries("en", "c3"), Learning: entries("fr", "native", "fr", "a1")},
			[]FieldError{{"spoken", "invalid_level"}, {"learning", "invalid_level"}, {"learning", "duplicate"}},
		},
		// Well formed, but not languages of the catalog (qaa to qtz are
		// reserved for private use and never will be).
		{
			"a language not in the catalog",
			Input{Spoken: entries("en", "c1"), Learning: entries("fr", "b1", "qq", "a1")},
			[]FieldError{{"learning", "unknown_language"}},
		},
		{
			"languages not in the catalog in both lists",
			Input{Spoken: entries("qqq", "c1"), Learning: entries("qq", "a1")},
			[]FieldError{{"spoken", "unknown_language"}, {"learning", "unknown_language"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.svc.Save(ctx, ana, tt.in)
			if fields := fieldsOf(t, err); !slices.Equal(fields, tt.want) {
				t.Errorf("fields = %v, want %v", fields, tt.want)
			}
			if !got.Equal(Selection{}) {
				t.Errorf("a rejected save returned %+v", got)
			}
			if err != nil && strings.Contains(err.Error(), "qq") {
				t.Errorf("the error names what was submitted: %v", err)
			}
		})
	}

	if got := f.rowVersions(t, ana); got != versions {
		t.Errorf("a rejected save wrote: %s → %s", versions, got)
	}
	got, err := f.svc.Get(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	if want := mustParse(t, kept); !got.Equal(want) {
		t.Errorf("selection changed by a rejected save: %+v", got)
	}
	if n := f.savesLogged(); n != 1 {
		t.Errorf("%d saves logged, want 1", n)
	}
}

// Each user's languages are keyed by the ID the caller passes and nothing
// else, and two users can have the same language.
func TestSelectionsAreSeparatePerUser(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	anas := Input{Spoken: entries("es", "native"), Learning: entries("ja", "a2")}
	if _, err := f.svc.Save(ctx, ana, anas); err != nil {
		t.Fatal(err)
	}

	got, err := f.svc.Get(ctx, ben)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(Selection{}) {
		t.Errorf("ben's languages before he saved any: %+v", got)
	}
	bens := Input{Spoken: entries("ja", "native"), Learning: entries("es", "b1")}
	if _, err := f.svc.Save(ctx, ben, bens); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Save(ctx, ben, Input{}); err != nil {
		t.Fatal(err)
	}
	got, err = f.svc.Get(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	if want := mustParse(t, anas); !got.Equal(want) {
		t.Errorf("ana's languages after ben saved and removed his: %+v", got)
	}
}

func TestSaveForADeletedUser(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	if _, err := f.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ana); err != nil {
		t.Fatal(err)
	}

	for _, in := range []Input{{Spoken: entries("es", "native")}, {}} {
		if _, err := f.svc.Save(ctx, ana, in); !errors.Is(err, ErrUserGone) {
			t.Errorf("Save(%+v) err = %v, want ErrUserGone", in, err)
		}
	}
	if n := f.count(t); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
	if n := f.savesLogged(); n != 0 {
		t.Errorf("%d saves logged, want 0", n)
	}
}

// Deleting a user removes their languages with them.
func TestDeletingAUserRemovesTheirLanguages(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	if _, err := f.svc.Save(ctx, ana, Input{Spoken: entries("es", "native"), Learning: entries("ja", "a2")}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ana); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

func TestAnEndedContextReadsAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := f.svc.Save(ctx, ana, Input{Spoken: entries("es", "native")}); !errors.Is(err, context.Canceled) {
		t.Errorf("Save err = %v, want context.Canceled", err)
	}
	if _, err := f.svc.Get(ctx, ana); !errors.Is(err, context.Canceled) {
		t.Errorf("Get err = %v, want context.Canceled", err)
	}
	if _, err := f.svc.Catalog(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Catalog err = %v, want context.Canceled", err)
	}
	if n := f.count(t); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

// Concurrent saves, including the very first ones, all succeed and leave the
// whole selection of one request, never a mix of two.
func TestConcurrentSavesLeaveOneWholeSelection(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")

	// Every input shares languages with the others, at other levels, in
	// other positions and in the other list.
	codes := []string{"es", "en", "fr", "de", "it", "pt", "ja", "ko"}
	levels := []string{"a1", "a2", "b1", "b2", "c1", "c2"}
	const n = 16
	inputs := make([]Input, n)
	for i := range inputs {
		code := func(k int) string { return codes[(i+k)%len(codes)] }
		level := levels[i%len(levels)]
		inputs[i] = Input{
			Spoken:   entries(code(0), level, code(1), level, code(2), level),
			Learning: entries(code(3), level, code(4), level),
		}
		if i%2 == 1 {
			inputs[i].Spoken, inputs[i].Learning = inputs[i].Learning, inputs[i].Spoken
		}
	}

	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			_, errs[i] = f.svc.Save(ctx, ana, inputs[i])
		})
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("save %d: %v", i, err)
		}
	}
	got, err := f.svc.Get(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(inputs, func(in Input) bool { return got.Equal(mustParse(t, in)) }) {
		t.Errorf("the selection is not the one of any request: %+v", got)
	}
	if n := f.count(t); n != 5 {
		t.Errorf("rows = %d, want the 5 of one request", n)
	}
}

// Several identical first saves at once (a client retrying while its first
// request is still running): all succeed with the same selection, written
// once.
func TestConcurrentIdenticalSavesAllSucceed(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	in := Input{Spoken: entries("es", "native", "en", "c1"), Learning: entries("ja", "a2")}
	want := mustParse(t, in)

	const n = 16
	results := make([]Selection, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			results[i], errs[i] = f.svc.Save(ctx, ana, in)
		})
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
		if !results[i].Equal(want) {
			t.Errorf("save %d returned %+v, want %+v", i, results[i], want)
		}
	}
	if got := f.count(t); got != 3 {
		t.Errorf("rows = %d, want 3", got)
	}
	if n := f.savesLogged(); n != 1 {
		t.Errorf("%d saves logged, want 1: only one of them changed anything", n)
	}
}

// A save locks the users row first, like the auth transactions that hold it
// FOR UPDATE (a password reset, a verification): it waits for one and then
// completes, instead of deadlocking or writing around it.
func TestSaveWaitsForATransactionHoldingTheUser(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, ana); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := f.svc.Save(ctx, ana, Input{Spoken: entries("es", "native")})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("Save finished while the user row was locked: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if n := f.count(t); n != 0 {
		t.Errorf("rows = %d while the save waits, want 0", n)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Save after the lock was released: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Save did not finish after the lock was released")
	}
	if n := f.count(t); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}

// A save holds the user FOR NO KEY UPDATE, which must not stop the rest of
// the app from inserting rows that reference the user (a new session, a
// first profile) while it runs.
func TestTheLockASaveTakesAllowsRowsReferencingTheUser(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`, ana); err != nil {
		t.Fatal(err)
	}

	short, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := f.pool.Exec(short, `INSERT INTO profiles (user_id, display_name) VALUES ($1, 'Ana')`, ana); err != nil {
		t.Errorf("inserting a row that references the locked user: %v", err)
	}
}

// What a member chose never reaches the logs: a save logs the user and
// nothing else, and reads and rejected saves log nothing.
func TestLogsNameTheUserButNeverTheLanguages(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	if _, err := f.svc.Save(ctx, ana, Input{Spoken: entries("es", "native"), Learning: entries("ja", "a2")}); err != nil {
		t.Fatal(err)
	}
	_, _ = f.svc.Save(ctx, ana, Input{Spoken: entries("qq", "native"), Learning: entries("ja", "c9")})
	_, _ = f.svc.Save(ctx, ana, Input{Spoken: entries("qq", "native")})
	if _, err := f.svc.Get(ctx, ana); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Catalog(ctx); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(f.logs.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("%d log lines, want the one of the save: %s", len(lines), f.logs)
	}
	var line map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &line); err != nil {
		t.Fatal(err)
	}
	if line["msg"] != "languages: saved" || line["user_id"] != ana {
		t.Errorf("log line = %v", line)
	}
	for key := range line {
		if !slices.Contains([]string{"time", "level", "msg", "user_id"}, key) {
			t.Errorf("the log line carries %q: %s", key, fmt.Sprint(line[key]))
		}
	}
}
