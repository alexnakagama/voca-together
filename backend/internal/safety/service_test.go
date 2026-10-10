package safety

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
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

func (f fixture) deleteUser(t *testing.T, id string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id); err != nil {
		t.Fatalf("delete user: %v", err)
	}
}

// count returns how many blocks are stored, by anyone.
func (f fixture) count(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM blocks`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// has reports whether the block of blocked by blocker is stored, in that
// direction.
func (f fixture) has(t *testing.T, blocker, blocked string) bool {
	t.Helper()
	var ok bool
	err := f.pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM blocks WHERE blocker_id = $1 AND blocked_id = $2)`, blocker, blocked).Scan(&ok)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// rowVersion identifies the stored row version of a block and when it was
// made: it changes whenever the row is rewritten, even with the same values.
func (f fixture) rowVersion(t *testing.T, blocker, blocked string) string {
	t.Helper()
	var v string
	err := f.pool.QueryRow(context.Background(),
		`SELECT xmin::text || '/' || ctid::text || ' ' || created_at::text
		 FROM blocks WHERE blocker_id = $1 AND blocked_id = $2`, blocker, blocked).Scan(&v)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// fill makes blocker block n new users, straight in the table, and returns
// them oldest block first.
func (f fixture) fill(t *testing.T, blocker string, n int) []string {
	t.Helper()
	ctx := context.Background()
	var ids []string
	err := f.pool.QueryRow(ctx,
		`WITH new AS (
		     INSERT INTO users (email, password_hash, email_verified_at)
		     SELECT 'filler' || g || '@example.com', 'x', now() FROM generate_series(1, $1::int) AS g
		     RETURNING id, email)
		 SELECT coalesce(array_agg(id::text ORDER BY length(email), email), '{}') FROM new`, n).Scan(&ids)
	if err != nil {
		t.Fatalf("insert users: %v", err)
	}
	_, err = f.pool.Exec(ctx,
		`INSERT INTO blocks (blocker_id, blocked_id, created_at)
		 SELECT $1, id::uuid, now() - make_interval(hours => 1) + make_interval(secs => ord)
		 FROM unnest($2::text[]) WITH ORDINALITY AS u (id, ord)`, blocker, ids)
	if err != nil {
		t.Fatalf("insert blocks: %v", err)
	}
	return ids
}

// warm opens n of the pool's connections at once (as many as it allows), so
// that requests started together really run together instead of each waiting
// for a connection to be dialled while the one before it finishes.
func (f fixture) warm(t *testing.T, n int) {
	t.Helper()
	ctx := context.Background()
	n = min(n, int(f.pool.Config().MaxConns))
	conns := make([]*pgxpool.Conn, n)
	for i := range conns {
		c, err := f.pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquire connection: %v", err)
		}
		conns[i] = c
	}
	for _, c := range conns {
		c.Release()
	}
}

func (f fixture) logged(msg string) int {
	return strings.Count(f.logs.String(), msg)
}

// fieldsOf returns the fields of err as "field: code" if it is a
// *ValidationError, and fails the test otherwise.
func fieldsOf(t *testing.T, err error) []string {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want a *ValidationError", err)
	}
	fields := make([]string, len(ve.Fields))
	for i, fe := range ve.Fields {
		fields[i] = fe.Error()
	}
	return fields
}

func TestBlock(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")

	if err := f.svc.Block(ctx, ana, ben); err != nil {
		t.Fatal(err)
	}
	if !f.has(t, ana, ben) {
		t.Error("ana's block of ben is not stored")
	}
	if f.has(t, ben, ana) {
		t.Error("a block of ana by ben is stored: a block is one row, in one direction")
	}
	if n := f.count(t); n != 1 {
		t.Errorf("blocks = %d, want 1", n)
	}
	got, err := f.svc.ListBlocked(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{ben}) {
		t.Errorf("ana's list = %v, want ben", got)
	}
}

// Blocking a member who is already blocked changes nothing: the row is not
// rewritten, it keeps its date (and so its place in the list), and nothing
// is logged.
func TestBlockIsIdempotent(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	cho := f.user(t, "cho@example.com")
	if err := f.svc.Block(ctx, ana, ben); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Block(ctx, ana, cho); err != nil {
		t.Fatal(err)
	}
	before := f.rowVersion(t, ana, ben)

	for range 3 {
		if err := f.svc.Block(ctx, ana, ben); err != nil {
			t.Fatalf("the same block again: %v", err)
		}
	}
	if after := f.rowVersion(t, ana, ben); after != before {
		t.Errorf("the row was rewritten: %s, was %s", after, before)
	}
	if n := f.count(t); n != 2 {
		t.Errorf("blocks = %d, want 2", n)
	}
	got, err := f.svc.ListBlocked(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{cho, ben}) {
		t.Errorf("ana's list = %v, want cho then ben: a repeat must not move ben", got)
	}
	if n := f.logged("block: added"); n != 2 {
		t.Errorf("%d blocks logged, want 2: a repeat logs nothing", n)
	}
}

func TestUnblock(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	cho := f.user(t, "cho@example.com")

	// Without a row: nothing to remove, and no error.
	if err := f.svc.Unblock(ctx, ana, ben); err != nil {
		t.Fatalf("unblocking a member who was not blocked: %v", err)
	}
	if n := f.logged("block: removed"); n != 0 {
		t.Errorf("%d unblocks logged, want 0: nothing was removed", n)
	}

	if err := f.svc.Block(ctx, ana, ben); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Block(ctx, ana, cho); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Unblock(ctx, ana, ben); err != nil {
		t.Fatal(err)
	}
	if f.has(t, ana, ben) {
		t.Error("ana's block of ben is still stored")
	}
	if !f.has(t, ana, cho) {
		t.Error("ana's block of cho went with the one of ben")
	}
	if n := f.logged("block: removed"); n != 1 {
		t.Errorf("%d unblocks logged, want 1", n)
	}

	// Again: the same answer, nothing removed, nothing logged.
	if err := f.svc.Unblock(ctx, ana, ben); err != nil {
		t.Fatalf("the same unblock again: %v", err)
	}
	if n := f.count(t); n != 1 {
		t.Errorf("blocks = %d, want 1", n)
	}
	if n := f.logged("block: removed"); n != 1 {
		t.Errorf("%d unblocks logged, want 1", n)
	}
}

// A member removes their own block and never the one made of them.
func TestUnblockLeavesTheOtherMembersBlock(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	if err := f.svc.Block(ctx, ana, ben); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Block(ctx, ben, ana); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.Unblock(ctx, ana, ben); err != nil {
		t.Fatal(err)
	}
	if f.has(t, ana, ben) {
		t.Error("ana's block of ben is still stored")
	}
	if !f.has(t, ben, ana) {
		t.Error("ben's block of ana was removed by ana's unblock")
	}
	blocked, err := f.svc.Blocked(ctx, ana, ben)
	if err != nil {
		t.Fatal(err)
	}
	if !blocked {
		t.Error("Blocked = false while ben's block remains")
	}

	// A member who never blocked anyone can't remove a block of themselves.
	if err := f.svc.Unblock(ctx, ana, ben); err != nil {
		t.Fatal(err)
	}
	if !f.has(t, ben, ana) {
		t.Error("ben's block of ana was removed by ana")
	}
}

// One row hides both members from each other: the answer doesn't depend on
// who asks, and a third member is not part of it.
func TestBlockedIsSymmetric(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	cho := f.user(t, "cho@example.com")
	dan := f.user(t, "dan@example.com")

	blocked := func(a, b string) bool {
		t.Helper()
		ok, err := f.svc.Blocked(ctx, a, b)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if blocked(ana, ben) || blocked(ben, ana) {
		t.Fatal("Blocked = true before any block")
	}

	if err := f.svc.Block(ctx, ana, ben); err != nil {
		t.Fatal(err)
	}
	if !blocked(ana, ben) {
		t.Error("Blocked(blocker, blocked) = false")
	}
	if !blocked(ben, ana) {
		t.Error("Blocked(blocked, blocker) = false: one row must answer for both orders")
	}
	for _, pair := range [][2]string{{ana, cho}, {cho, ana}, {ben, cho}, {cho, ben}, {cho, dan}, {ana, ana}} {
		if blocked(pair[0], pair[1]) {
			t.Errorf("Blocked = true for a pair with no block between them")
		}
	}
	// Someone who is not a user is blocked by nobody.
	if blocked(ana, "00000000-0000-0000-0000-000000000000") {
		t.Error("Blocked = true for an id that is no user")
	}

	// Both rows, then one: still blocked until the last one goes.
	if err := f.svc.Block(ctx, ben, ana); err != nil {
		t.Fatal(err)
	}
	if !blocked(ana, ben) || !blocked(ben, ana) {
		t.Error("Blocked = false with a block in each direction")
	}
	if err := f.svc.Unblock(ctx, ana, ben); err != nil {
		t.Fatal(err)
	}
	if !blocked(ana, ben) || !blocked(ben, ana) {
		t.Error("Blocked = false while one of the two blocks remains")
	}
	if err := f.svc.Unblock(ctx, ben, ana); err != nil {
		t.Fatal(err)
	}
	if blocked(ana, ben) || blocked(ben, ana) {
		t.Error("Blocked = true after both blocks were removed")
	}
}

func TestListBlockedIsNewestFirstAndOnlyTheCallers(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	cho := f.user(t, "cho@example.com")
	dan := f.user(t, "dan@example.com")
	eve := f.user(t, "eve@example.com")

	list := func(id string) []string {
		t.Helper()
		ids, err := f.svc.ListBlocked(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return ids
	}
	if got := list(ana); len(got) != 0 {
		t.Errorf("the list of a member who blocked nobody = %v", got)
	}

	for _, blocked := range []string{ben, cho, dan} {
		if err := f.svc.Block(ctx, ana, blocked); err != nil {
			t.Fatal(err)
		}
	}
	// Blocks by others, of ana and of someone else, are theirs alone.
	if err := f.svc.Block(ctx, eve, ana); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Block(ctx, cho, dan); err != nil {
		t.Fatal(err)
	}

	if got, want := list(ana), []string{dan, cho, ben}; !slices.Equal(got, want) {
		t.Errorf("ana's list = %v, want the newest first %v", got, want)
	}
	if got := list(eve); !slices.Equal(got, []string{ana}) {
		t.Errorf("eve's list = %v, want ana only", got)
	}
	if got := list(dan); len(got) != 0 {
		t.Errorf("dan, blocked by two members, lists %v: who blocked a member is never listed", got)
	}

	// Blocking again after an unblock is a new block, at the top.
	if err := f.svc.Unblock(ctx, ana, ben); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Block(ctx, ana, ben); err != nil {
		t.Fatal(err)
	}
	if got, want := list(ana), []string{ben, dan, cho}; !slices.Equal(got, want) {
		t.Errorf("ana's list = %v, want %v", got, want)
	}
}

// A member who has blocked the caller is left out of the caller's list,
// whoever blocked first: the list must not say that a member who is hidden
// from the caller exists. The caller's own block of them is still stored,
// still counts, and shows again once the other member's block is gone.
func TestListBlockedLeavesOutAMemberWhoBlockedTheCaller(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	cho := f.user(t, "cho@example.com")
	dan := f.user(t, "dan@example.com")
	list := func(id string) []string {
		t.Helper()
		ids, err := f.svc.ListBlocked(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return ids
	}
	block := func(blocker, blocked string) {
		t.Helper()
		if err := f.svc.Block(ctx, blocker, blocked); err != nil {
			t.Fatal(err)
		}
	}

	// Ana blocks Ben, Cho and Dan; Ben had blocked her first, Dan does after.
	block(ben, ana)
	block(ana, ben)
	block(ana, cho)
	block(ana, dan)
	if got, want := list(ana), []string{dan, cho}; !slices.Equal(got, want) {
		t.Errorf("ana's list = %v, want %v: ben blocked her", got, want)
	}
	block(dan, ana)
	if got, want := list(ana), []string{cho}; !slices.Equal(got, want) {
		t.Errorf("ana's list = %v, want %v: ben and dan blocked her", got, want)
	}
	if got := list(ben); len(got) != 0 {
		t.Errorf("ben's list = %v, want nothing: ana blocked him", got)
	}
	// Every block is stored and counted all the same.
	if n := f.count(t); n != 5 {
		t.Errorf("blocks = %d, want 5", n)
	}
	if !f.has(t, ana, ben) || !f.has(t, ana, dan) {
		t.Error("ana's blocks of ben and dan are not stored")
	}

	// Ben's block goes: Ana's block of him is listed again, in its place.
	if err := f.svc.Unblock(ctx, ben, ana); err != nil {
		t.Fatal(err)
	}
	if got, want := list(ana), []string{cho, ben}; !slices.Equal(got, want) {
		t.Errorf("ana's list = %v, want %v", got, want)
	}
	// And a block that is not listed can still be removed.
	if err := f.svc.Unblock(ctx, ana, dan); err != nil {
		t.Fatal(err)
	}
	if f.has(t, ana, dan) || !f.has(t, dan, ana) {
		t.Error("want ana's block of dan removed and dan's of ana kept")
	}
}

// A member can't block or unblock themselves, and that is known without
// asking the database: it is refused even when the database can't be
// reached.
func TestSelfIsRefusedBeforeAnyQuery(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	closed, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	svc := NewService(closed, slog.New(slog.NewJSONHandler(f.logs, nil)))

	for name, call := range map[string]func(context.Context, string, string) error{
		"Block":   svc.Block,
		"Unblock": svc.Unblock,
	} {
		err := call(ctx, ana, ana)
		if got := fieldsOf(t, err); !slices.Equal(got, []string{"member: self"}) {
			t.Errorf("%s of oneself: fields = %v, want member: self", name, got)
		}
		// The same call for someone else does need the database.
		if err := call(ctx, ana, "00000000-0000-0000-0000-000000000000"); err == nil {
			t.Errorf("%s reached a closed pool without an error", name)
		}
	}
	if n := f.count(t); n != 0 {
		t.Errorf("blocks = %d, want 0", n)
	}
	if f.logs.Len() != 0 {
		t.Errorf("a refused request was logged: %s", f.logs)
	}
}

func TestBlockByADeletedUser(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	f.deleteUser(t, ana)

	if err := f.svc.Block(ctx, ana, ben); !errors.Is(err, ErrUserGone) {
		t.Errorf("Block err = %v, want ErrUserGone", err)
	}
	if n := f.count(t); n != 0 {
		t.Errorf("blocks = %d, want 0", n)
	}
	if f.logs.Len() != 0 {
		t.Errorf("a block by a deleted user was logged: %s", f.logs)
	}
}

// The member to block was deleted after the caller resolved them: there is
// nothing to block, which is not an error, and nothing is stored or logged.
func TestBlockOfADeletedUser(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	cho := f.user(t, "cho@example.com")
	f.deleteUser(t, ben)

	if err := f.svc.Block(ctx, ana, ben); err != nil {
		t.Errorf("Block of a deleted user: %v, want nil", err)
	}
	if n := f.count(t); n != 0 {
		t.Errorf("blocks = %d, want 0", n)
	}
	if f.logs.Len() != 0 {
		t.Errorf("a block that stored nothing was logged: %s", f.logs)
	}

	// The failed insert left nothing behind: the blocker can still block.
	if err := f.svc.Block(ctx, ana, cho); err != nil {
		t.Fatal(err)
	}
	if !f.has(t, ana, cho) {
		t.Error("ana's block of cho is not stored")
	}
}

// A block goes with either account, and one of a deleted member no longer
// counts.
func TestDeletingAUserRemovesTheirBlocks(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	cho := f.user(t, "cho@example.com")
	for _, pair := range [][2]string{{ana, ben}, {ana, cho}, {ben, cho}} {
		if err := f.svc.Block(ctx, pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}

	f.deleteUser(t, ben)
	got, err := f.svc.ListBlocked(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{cho}) {
		t.Errorf("ana's list after ben was deleted = %v, want cho", got)
	}
	if n := f.count(t); n != 1 {
		t.Errorf("blocks = %d, want 1: ben's own block goes with him too", n)
	}

	f.deleteUser(t, ana)
	if n := f.count(t); n != 0 {
		t.Errorf("blocks = %d after the blocker was deleted, want 0", n)
	}
}

func TestAnEndedContextReadsAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	cho := f.user(t, "cho@example.com")
	if err := f.svc.Block(context.Background(), ana, ben); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := f.svc.Block(ctx, ana, cho); !errors.Is(err, context.Canceled) {
		t.Errorf("Block err = %v, want context.Canceled", err)
	}
	if err := f.svc.Unblock(ctx, ana, ben); !errors.Is(err, context.Canceled) {
		t.Errorf("Unblock err = %v, want context.Canceled", err)
	}
	if _, err := f.svc.Blocked(ctx, ana, ben); !errors.Is(err, context.Canceled) {
		t.Errorf("Blocked err = %v, want context.Canceled", err)
	}
	if _, err := f.svc.ListBlocked(ctx, ana); !errors.Is(err, context.Canceled) {
		t.Errorf("ListBlocked err = %v, want context.Canceled", err)
	}
	if !f.has(t, ana, ben) || f.count(t) != 1 {
		t.Errorf("blocks = %d, want ana's block of ben and nothing else", f.count(t))
	}
}

func TestBlockLimit(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	cho := f.user(t, "cho@example.com")
	others := f.fill(t, ana, MaxBlocks-1)

	// The last place.
	if err := f.svc.Block(ctx, ana, ben); err != nil {
		t.Fatalf("block number %d: %v", MaxBlocks, err)
	}
	if n := f.count(t); n != MaxBlocks {
		t.Fatalf("blocks = %d, want %d", n, MaxBlocks)
	}

	// One more is refused and stores nothing.
	err := f.svc.Block(ctx, ana, cho)
	if got := fieldsOf(t, err); !slices.Equal(got, []string{"blocks: too_many"}) {
		t.Errorf("block number %d: fields = %v, want blocks: too_many", MaxBlocks+1, got)
	}
	if f.has(t, ana, cho) || f.count(t) != MaxBlocks {
		t.Errorf("blocks = %d after a refused block, want %d", f.count(t), MaxBlocks)
	}

	// Repeating a block that exists is not one more.
	for _, blocked := range []string{ben, others[0], others[len(others)-1]} {
		if err := f.svc.Block(ctx, ana, blocked); err != nil {
			t.Errorf("repeating an existing block at the limit: %v", err)
		}
	}

	// The limit is each member's own: cho, who blocked nobody, is not at it,
	// and being blocked by many doesn't count.
	if err := f.svc.Block(ctx, cho, ana); err != nil {
		t.Errorf("a block by another member: %v", err)
	}
	if err := f.svc.Block(ctx, ben, cho); err != nil {
		t.Errorf("a block by a member who is blocked: %v", err)
	}

	// An unblock makes room for exactly one.
	if err := f.svc.Unblock(ctx, ana, others[0]); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Block(ctx, ana, cho); err != nil {
		t.Errorf("a block after an unblock made room: %v", err)
	}
	err = f.svc.Block(ctx, ana, others[0])
	if got := fieldsOf(t, err); !slices.Equal(got, []string{"blocks: too_many"}) {
		t.Errorf("at the limit again: fields = %v, want blocks: too_many", got)
	}

	list, err := f.svc.ListBlocked(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	// Ana is at the limit with MaxBlocks stored. Cho blocked her too, so
	// her block of cho counts and is not listed.
	if f.count(t) != MaxBlocks+2 || !f.has(t, ana, cho) {
		t.Errorf("blocks = %d, want ana's %d and the two by others", f.count(t), MaxBlocks)
	}
	if len(list) != MaxBlocks-1 || list[0] != ben || slices.Contains(list, cho) {
		t.Errorf("ana's list holds %d members, want %d with ben first and without cho, who blocked her",
			len(list), MaxBlocks-1)
	}
	if n := f.logged("block: added"); n != 4 {
		t.Errorf("%d blocks logged, want 4: a refused block and a repeat log nothing", n)
	}
}

// A blocked member whose account is deleted frees their place.
func TestADeletedBlockedUserNoLongerCountsTowardTheLimit(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	others := f.fill(t, ana, MaxBlocks)

	err := f.svc.Block(ctx, ana, ben)
	if got := fieldsOf(t, err); !slices.Equal(got, []string{"blocks: too_many"}) {
		t.Fatalf("at the limit: fields = %v, want blocks: too_many", got)
	}
	f.deleteUser(t, others[0])
	if err := f.svc.Block(ctx, ana, ben); err != nil {
		t.Errorf("a block after a blocked user was deleted: %v", err)
	}
}

// Several identical blocks at once (a client retrying while its first
// request is still running, or two sessions): all succeed, one row.
func TestConcurrentIdenticalBlocksLeaveOneRow(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")

	const n = 16
	f.warm(t, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			errs[i] = f.svc.Block(ctx, ana, ben)
		})
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("block %d: %v", i, err)
		}
	}
	if got := f.count(t); got != 1 {
		t.Errorf("blocks = %d, want 1", got)
	}
	if got := f.logged("block: added"); got != 1 {
		t.Errorf("%d blocks logged, want 1: only one of them stored anything", got)
	}
}

// Two blocks of different members at once, with one place left: the limit
// holds exactly, whichever comes first.
func TestConcurrentBlocksAtTheLimit(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	targets := []string{f.user(t, "ben@example.com"), f.user(t, "cho@example.com")}
	f.fill(t, ana, MaxBlocks-1)
	f.warm(t, len(targets))

	errs := make([]error, len(targets))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Go(func() {
			<-start
			errs[i] = f.svc.Block(ctx, ana, target)
		})
	}
	close(start)
	wg.Wait()

	var stored, refused int
	for i, err := range errs {
		switch {
		case err == nil:
			stored++
			if !f.has(t, ana, targets[i]) {
				t.Errorf("block %d answered nil and is not stored", i)
			}
		default:
			if got := fieldsOf(t, err); !slices.Equal(got, []string{"blocks: too_many"}) {
				t.Errorf("block %d: fields = %v, want blocks: too_many", i, got)
			}
			refused++
			if f.has(t, ana, targets[i]) {
				t.Errorf("block %d was refused and is stored", i)
			}
		}
	}
	if stored != 1 || refused != 1 {
		t.Errorf("%d stored and %d refused, want one of each", stored, refused)
	}
	if n := f.count(t); n != MaxBlocks {
		t.Errorf("blocks = %d, want %d", n, MaxBlocks)
	}
}

// What makes the limit exact: one member's blocks run in turn. While one of
// them holds the last place without having committed, the next waits, and
// then counts it.
func TestBlocksOfOneMemberRunInTurn(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	cho := f.user(t, "cho@example.com")
	f.fill(t, ana, MaxBlocks-1)

	// A block of ben in flight: the lock it takes and the row it inserts.
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`, ana); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)`, ana, ben); err != nil {
		t.Fatal(err)
	}

	// A block of someone else and a repeat of the one in flight.
	other := make(chan error, 1)
	same := make(chan error, 1)
	go func() { other <- f.svc.Block(ctx, ana, cho) }()
	go func() { same <- f.svc.Block(ctx, ana, ben) }()
	select {
	case err := <-other:
		t.Fatalf("a block finished while another of the same member was in flight: %v", err)
	case err := <-same:
		t.Fatalf("a repeated block finished while the first was in flight: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for name, done := range map[string]chan error{"other": other, "same": same} {
		select {
		case err := <-done:
			switch name {
			case "other":
				if got := fieldsOf(t, err); !slices.Equal(got, []string{"blocks: too_many"}) {
					t.Errorf("the block that came second: fields = %v, want blocks: too_many", got)
				}
			case "same":
				if err != nil {
					t.Errorf("the repeated block: %v, want nil", err)
				}
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("the %s block did not finish after the first committed", name)
		}
	}
	if n := f.count(t); n != MaxBlocks {
		t.Errorf("blocks = %d, want %d", n, MaxBlocks)
	}
	if f.has(t, ana, cho) {
		t.Error("the refused block is stored")
	}
	if n := f.logged("block: added"); n != 0 {
		t.Errorf("%d blocks logged, want 0: neither call stored anything", n)
	}
}

// Two members blocking each other at the same moment each lock their own
// users row and reference the other's: both must finish, with both blocks.
func TestConcurrentMutualBlocksBothSucceed(t *testing.T) {
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	// A deadlock would be ended by PostgreSQL with an error after its
	// deadlock_timeout; anything slower than this is a failure too.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f.warm(t, 2)

	for round := range 25 {
		errs := make([]error, 2)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			errs[0] = f.svc.Block(ctx, ana, ben)
		})
		wg.Go(func() {
			<-start
			errs[1] = f.svc.Block(ctx, ben, ana)
		})
		close(start)
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d, block %d: %v", round, i, err)
			}
		}
		if !f.has(t, ana, ben) || !f.has(t, ben, ana) {
			t.Fatalf("round %d: both blocks must exist", round)
		}
		if _, err := f.pool.Exec(ctx, `DELETE FROM blocks`); err != nil {
			t.Fatal(err)
		}
	}
}

// The reason two mutual blocks can't deadlock: while one member's block
// holds their own users row, another member's block of them, which only
// references that row, is not made to wait.
func TestABlockDoesNotWaitForTheBlockedMembersOwnBlock(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")

	// What ana's block holds while it runs.
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
	if err := f.svc.Block(short, ben, ana); err != nil {
		t.Fatalf("ben's block of ana while ana's users row is held: %v", err)
	}
	if !f.has(t, ben, ana) {
		t.Error("ben's block of ana is not stored")
	}
}

// A block locks the blocker's users row first, like the auth transactions
// that hold it FOR UPDATE (a password reset, a verification): it waits for
// one and then completes, instead of deadlocking or writing around it.
func TestBlockWaitsForATransactionHoldingTheBlocker(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, ana); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- f.svc.Block(ctx, ana, ben) }()
	select {
	case err := <-done:
		t.Fatalf("Block finished while the blocker's row was locked: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if n := f.count(t); n != 0 {
		t.Errorf("blocks = %d while the block waits, want 0", n)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Block after the lock was released: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Block did not finish after the lock was released")
	}
	if !f.has(t, ana, ben) {
		t.Error("ana's block of ben is not stored")
	}
}

// A block and an unblock of the same pair at once: neither fails, and the
// member ends up blocked or not, with at most one row.
func TestConcurrentBlockAndUnblockBothSucceed(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	f.warm(t, 2)

	for round := range 25 {
		// Alternate between a pair that is blocked already and one that isn't.
		if round%2 == 0 {
			if err := f.svc.Block(ctx, ana, ben); err != nil {
				t.Fatal(err)
			}
		}
		errs := make([]error, 2)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			errs[0] = f.svc.Block(ctx, ana, ben)
		})
		wg.Go(func() {
			<-start
			errs[1] = f.svc.Unblock(ctx, ana, ben)
		})
		close(start)
		wg.Wait()

		if errs[0] != nil {
			t.Fatalf("round %d, block: %v", round, errs[0])
		}
		if errs[1] != nil {
			t.Fatalf("round %d, unblock: %v", round, errs[1])
		}
		if n := f.count(t); n > 1 {
			t.Fatalf("round %d: blocks = %d, want 0 or 1", round, n)
		}
		blocked, err := f.svc.Blocked(ctx, ana, ben)
		if err != nil {
			t.Fatal(err)
		}
		if blocked != f.has(t, ana, ben) {
			t.Fatalf("round %d: Blocked = %v, the row says %v", round, blocked, !blocked)
		}
		if err := f.svc.Unblock(ctx, ana, ben); err != nil {
			t.Fatal(err)
		}
	}
}

// Who was blocked never reaches the logs: a block and an unblock each log
// the member who made it and nothing else, only when a row changed, and
// reads and refused requests log nothing.
func TestLogsNameTheBlockerAndNeverTheBlocked(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	cho := f.user(t, "cho@example.com")
	gone := f.user(t, "gone@example.com")
	f.deleteUser(t, gone)

	// Nothing here changes a row.
	if err := f.svc.Unblock(ctx, ana, ben); err != nil {
		t.Fatal(err)
	}
	_ = f.svc.Block(ctx, ana, ana)
	_ = f.svc.Unblock(ctx, ana, ana)
	if err := f.svc.Block(ctx, ana, gone); err != nil {
		t.Fatal(err)
	}
	_ = f.svc.Block(ctx, gone, ben)
	if f.logs.Len() != 0 {
		t.Fatalf("logged without a row changing: %s", f.logs)
	}

	// Two changes, each with repeats and reads around it.
	if err := f.svc.Block(ctx, ana, ben); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Block(ctx, ana, ben); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Blocked(ctx, ben, ana); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Blocked(ctx, ana, cho); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ListBlocked(ctx, ana); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Unblock(ctx, ana, ben); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Unblock(ctx, ana, ben); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(f.logs.String()), "\n")
	want := []string{"block: added", "block: removed"}
	if len(lines) != len(want) {
		t.Fatalf("%d log lines, want one per change: %s", len(lines), f.logs)
	}
	for i, raw := range lines {
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatal(err)
		}
		if line["msg"] != want[i] || line["user_id"] != ana {
			t.Errorf("log line %d = %v, want %q by the blocker", i, line, want[i])
		}
		for key := range line {
			if !slices.Contains([]string{"time", "level", "msg", "user_id"}, key) {
				t.Errorf("log line %d carries %q: %s", i, key, fmt.Sprint(line[key]))
			}
		}
	}
	for name, id := range map[string]string{"the blocked member": ben, "a member asked about": cho, "a deleted member": gone} {
		if strings.Contains(f.logs.String(), id) {
			t.Errorf("the logs hold the user id of %s", name)
		}
	}
}

// At the limit a refused block logs nothing either, and neither does the
// limit itself.
func TestARefusedBlockIsNotLogged(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	f.fill(t, ana, MaxBlocks)

	if err := f.svc.Block(ctx, ana, ben); err == nil {
		t.Fatal("a block over the limit was accepted")
	}
	if f.logs.Len() != 0 {
		t.Errorf("a refused block was logged: %s", f.logs)
	}
}

// mustParse returns the content of a valid report.
func mustParse(t *testing.T, reason, details string) ReportContent {
	t.Helper()
	c, err := ParseReport(reason, details)
	if err != nil {
		t.Fatalf("ParseReport(%q): %v", reason, err)
	}
	return c
}

// reports returns how many reports are stored, by anyone.
func (f fixture) reports(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM reports`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// storedReport is the one report of reported by reporter, as stored. version
// changes whenever the row is rewritten, even with the same values.
type storedReport struct {
	reason, details      string
	createdAt, updatedAt time.Time
	version              string
}

func (f fixture) report(t *testing.T, reporter, reported string) storedReport {
	t.Helper()
	var r storedReport
	err := f.pool.QueryRow(context.Background(),
		`SELECT reason, details, created_at, updated_at, xmin::text || '/' || ctid::text
		 FROM reports WHERE reporter_id = $1 AND reported_id = $2`, reporter, reported).
		Scan(&r.reason, &r.details, &r.createdAt, &r.updatedAt, &r.version)
	if err != nil {
		t.Fatalf("the report: %v", err)
	}
	return r
}

func TestReport(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")

	if err := f.svc.Report(ctx, ana, ben, mustParse(t, "spam", "")); err != nil {
		t.Fatal(err)
	}
	got := f.report(t, ana, ben)
	if got.reason != "spam" || got.details != "" {
		t.Errorf("stored = %q, %q; want spam and no details", got.reason, got.details)
	}
	if got.createdAt.IsZero() || !got.updatedAt.Equal(got.createdAt) {
		t.Errorf("created_at %v, updated_at %v; want one time", got.createdAt, got.updatedAt)
	}
	if n := f.reports(t); n != 1 {
		t.Errorf("reports = %d, want 1: a report is one row, in one direction", n)
	}
	if n := f.count(t); n != 0 {
		t.Errorf("blocks = %d: a report blocks nobody", n)
	}

	// With details, about someone else: stored as parsed.
	cho := f.user(t, "cho@example.com")
	if err := f.svc.Report(ctx, ana, cho, mustParse(t, "harassment", " one\r\n\ttwo ")); err != nil {
		t.Fatal(err)
	}
	if got := f.report(t, ana, cho); got.reason != "harassment" || got.details != "one\n two" {
		t.Errorf("stored = %q, %q; want harassment and the normalized text", got.reason, got.details)
	}
}

// Sending what is already stored changes nothing: no row version is written,
// updated_at stays, and nothing is logged.
func TestReportIsIdempotent(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	for _, c := range []ReportContent{mustParse(t, "spam", ""), mustParse(t, "other", "what happened")} {
		if err := f.svc.Report(ctx, ana, ben, c); err != nil {
			t.Fatal(err)
		}
		before := f.report(t, ana, ben)
		logged := f.logged("report: saved")

		for range 3 {
			if err := f.svc.Report(ctx, ana, ben, c); err != nil {
				t.Fatalf("the same report again: %v", err)
			}
		}
		if after := f.report(t, ana, ben); after != before {
			t.Errorf("a repeat rewrote the report: %+v, was %+v", after.version, before.version)
		}
		if n := f.logged("report: saved"); n != logged {
			t.Errorf("%d reports logged, want %d: a repeat logs nothing", n, logged)
		}
	}
	if n := f.reports(t); n != 1 {
		t.Errorf("reports = %d, want 1", n)
	}
}

// A different reason or different details replace the content of the pair's
// one report and move updated_at; created_at and the row's id stay.
func TestReportReplacesTheEarlierOne(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	if err := f.svc.Report(ctx, ana, ben, mustParse(t, "spam", "first")); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := f.pool.QueryRow(ctx, `SELECT id::text FROM reports`).Scan(&id); err != nil {
		t.Fatal(err)
	}

	for _, step := range []struct{ what, reason, details string }{
		{"another reason", "harassment", "first"},
		{"other details", "harassment", "second"},
		{"both", "other", "third"},
		{"details removed", "other", ""},
	} {
		before := f.report(t, ana, ben)
		logged := f.logged("report: saved")
		if err := f.svc.Report(ctx, ana, ben, mustParse(t, step.reason, step.details)); err != nil {
			t.Fatalf("%s: %v", step.what, err)
		}
		after := f.report(t, ana, ben)
		if after.reason != step.reason || after.details != step.details {
			t.Errorf("%s: stored = %q, %q; want %q, %q", step.what, after.reason, after.details, step.reason, step.details)
		}
		if !after.createdAt.Equal(before.createdAt) {
			t.Errorf("%s: created_at moved", step.what)
		}
		if !after.updatedAt.After(before.updatedAt) {
			t.Errorf("%s: updated_at = %v, was %v; want it later", step.what, after.updatedAt, before.updatedAt)
		}
		if n := f.logged("report: saved"); n != logged+1 {
			t.Errorf("%s: %d reports logged, want %d", step.what, n, logged+1)
		}
	}
	var now string
	if err := f.pool.QueryRow(ctx, `SELECT id::text FROM reports`).Scan(&now); err != nil {
		t.Fatalf("one report expected: %v", err)
	}
	if now != id {
		t.Error("the report was replaced by a new row, not changed")
	}
}

// Each member's report of one member is their own, and so is each direction
// of a pair.
func TestReportsByDifferentMembersAreEachStored(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	cho := f.user(t, "cho@example.com")
	if err := f.svc.Report(ctx, ana, ben, mustParse(t, "spam", "from ana")); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Report(ctx, cho, ben, mustParse(t, "impersonation", "from cho")); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Report(ctx, ben, ana, mustParse(t, "other", "")); err != nil {
		t.Fatal(err)
	}

	if got := f.report(t, ana, ben); got.reason != "spam" || got.details != "from ana" {
		t.Errorf("ana's report = %q, %q", got.reason, got.details)
	}
	if got := f.report(t, cho, ben); got.reason != "impersonation" || got.details != "from cho" {
		t.Errorf("cho's report = %q, %q", got.reason, got.details)
	}
	if got := f.report(t, ben, ana); got.reason != "other" {
		t.Errorf("ben's report of ana = %q", got.reason)
	}
	if n := f.reports(t); n != 3 {
		t.Errorf("reports = %d, want 3", n)
	}
}

// A member can't report themselves, and that is known without asking the
// database. Neither is a content that ParseReport did not make ever sent to
// it.
func TestReportOfOneselfIsRefusedBeforeAnyQuery(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	closed, err := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	svc := NewService(closed, slog.New(slog.NewJSONHandler(f.logs, nil)))
	valid := mustParse(t, "spam", "details")

	err = svc.Report(ctx, ana, ana, valid)
	if got := fieldsOf(t, err); !slices.Equal(got, []string{"member: self"}) {
		t.Errorf("a report of oneself: fields = %v, want member: self", got)
	}
	err = svc.Report(ctx, ana, ben, ReportContent{})
	if got := fieldsOf(t, err); !slices.Equal(got, []string{"reason: required"}) {
		t.Errorf("a report with no content: fields = %v, want reason: required", got)
	}
	// The same call for someone else does need the database.
	if err := svc.Report(ctx, ana, ben, valid); err == nil {
		t.Error("Report reached a closed pool without an error")
	}
	if n := f.reports(t); n != 0 {
		t.Errorf("reports = %d, want 0", n)
	}
	if f.logs.Len() != 0 {
		t.Errorf("a refused report was logged: %s", f.logs)
	}
}

func TestReportByADeletedUser(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	// An earlier report of ana's stays, with no reporter: it must neither
	// stop the next statement nor be taken for theirs.
	if err := f.svc.Report(ctx, ana, ben, mustParse(t, "spam", "earlier")); err != nil {
		t.Fatal(err)
	}
	f.deleteUser(t, ana)
	f.logs.Reset()

	err := f.svc.Report(ctx, ana, ben, mustParse(t, "other", "later"))
	if !errors.Is(err, ErrUserGone) {
		t.Errorf("Report err = %v, want ErrUserGone", err)
	}
	var reason, details string
	if err := f.pool.QueryRow(ctx, `SELECT reason, details FROM reports WHERE reporter_id IS NULL`).
		Scan(&reason, &details); err != nil {
		t.Fatalf("the earlier report: %v", err)
	}
	if reason != "spam" || details != "earlier" {
		t.Errorf("the earlier report = %q, %q; want it unchanged", reason, details)
	}
	if n := f.reports(t); n != 1 {
		t.Errorf("reports = %d, want the earlier one", n)
	}
	if f.logs.Len() != 0 {
		t.Errorf("a report by a deleted user was logged: %s", f.logs)
	}
}

// The member to report was deleted after the caller resolved them: there is
// nobody to report, which is not an error, and nothing is stored or logged.
func TestReportOfADeletedUser(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	cho := f.user(t, "cho@example.com")
	f.deleteUser(t, ben)

	if err := f.svc.Report(ctx, ana, ben, mustParse(t, "spam", "details")); err != nil {
		t.Errorf("Report of a deleted user: %v, want nil", err)
	}
	if n := f.reports(t); n != 0 {
		t.Errorf("reports = %d, want 0", n)
	}
	if f.logs.Len() != 0 {
		t.Errorf("a report that stored nothing was logged: %s", f.logs)
	}

	// The failed insert left nothing behind: the reporter can still report.
	if err := f.svc.Report(ctx, ana, cho, mustParse(t, "spam", "")); err != nil {
		t.Fatal(err)
	}
	if n := f.reports(t); n != 1 {
		t.Errorf("reports = %d, want 1", n)
	}
}

func TestReportWithAnEndedContextWritesNothing(t *testing.T) {
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := f.svc.Report(ctx, ana, ben, mustParse(t, "spam", "")); !errors.Is(err, context.Canceled) {
		t.Errorf("Report err = %v, want context.Canceled", err)
	}
	if n := f.reports(t); n != 0 {
		t.Errorf("reports = %d, want 0", n)
	}
}

// Several different reports of one member by one member at once: all
// succeed, one row, and its reason and details are those of one request
// together, never the reason of one with the details of another.
func TestConcurrentReportsOfOnePairLeaveOneWholeReport(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")

	const n = 16
	f.warm(t, n)
	contents := make([]ReportContent, n)
	for i := range contents {
		contents[i] = mustParse(t, Reasons[i%len(Reasons)], fmt.Sprintf("request %d", i))
	}
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			errs[i] = f.svc.Report(ctx, ana, ben, contents[i])
		})
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("report %d: %v", i, err)
		}
	}
	if got := f.reports(t); got != 1 {
		t.Fatalf("reports = %d, want 1", got)
	}
	got := f.report(t, ana, ben)
	if !slices.Contains(contents, ReportContent{reason: got.reason, details: got.details}) {
		t.Errorf("stored = %q, %q: not the content of any one request", got.reason, got.details)
	}
	// Every request differed from the others, so each one wrote.
	if got := f.logged("report: saved"); got != n {
		t.Errorf("%d reports logged, want %d", got, n)
	}
}

// A report and the reported member's own report of the reporter, at once:
// each takes only shared locks on the other's row, so neither waits.
func TestConcurrentMutualReportsBothSucceed(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	f.warm(t, 2)

	for round := range 20 {
		c := mustParse(t, "other", fmt.Sprintf("round %d", round))
		var errA, errB error
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() { <-start; errA = f.svc.Report(ctx, ana, ben, c) })
		wg.Go(func() { <-start; errB = f.svc.Report(ctx, ben, ana, c) })
		close(start)
		wg.Wait()
		if errA != nil || errB != nil {
			t.Fatalf("round %d: %v, %v", round, errA, errB)
		}
	}
	if n := f.reports(t); n != 2 {
		t.Errorf("reports = %d, want one each way", n)
	}
}

// What a report says and whom it is about never reach the logs: a stored or
// changed report logs the member who sent it and nothing else, and repeats
// and refused reports log nothing.
func TestReportLogsNameTheReporterAndNothingElse(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	ben := f.user(t, "ben@example.com")
	gone := f.user(t, "gone@example.com")
	f.deleteUser(t, gone)
	const detailsMark = "DETAILSMARK"

	// Nothing here stores a report.
	_ = f.svc.Report(ctx, ana, ana, mustParse(t, "impersonation", detailsMark))
	_ = f.svc.Report(ctx, ana, ben, ReportContent{})
	if err := f.svc.Report(ctx, ana, gone, mustParse(t, "impersonation", detailsMark)); err != nil {
		t.Fatal(err)
	}
	_ = f.svc.Report(ctx, gone, ben, mustParse(t, "impersonation", detailsMark))
	if f.logs.Len() != 0 {
		t.Fatalf("logged without a report stored: %s", f.logs)
	}

	// Two writes, each followed by a repeat.
	for _, c := range []ReportContent{
		mustParse(t, "impersonation", detailsMark),
		mustParse(t, "inappropriate_content", detailsMark+" again"),
	} {
		for range 2 {
			if err := f.svc.Report(ctx, ana, ben, c); err != nil {
				t.Fatal(err)
			}
		}
	}

	lines := strings.Split(strings.TrimSpace(f.logs.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d log lines, want one per write: %s", len(lines), f.logs)
	}
	for i, raw := range lines {
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatal(err)
		}
		if line["msg"] != "report: saved" || line["user_id"] != ana {
			t.Errorf("log line %d = %v, want report: saved by the reporter", i, line)
		}
		for key := range line {
			if !slices.Contains([]string{"time", "level", "msg", "user_id"}, key) {
				t.Errorf("log line %d carries %q: %s", i, key, fmt.Sprint(line[key]))
			}
		}
	}
	for what, text := range map[string]string{
		"the reported member's user id": ben,
		"a deleted member's user id":    gone,
		"the details":                   detailsMark,
		"a reason":                      "impersonation",
		"the other reason":              "inappropriate_content",
	} {
		if strings.Contains(f.logs.String(), text) {
			t.Errorf("the logs hold %s", what)
		}
	}
}
