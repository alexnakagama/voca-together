package avatar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image/color"
	"log/slog"
	"maps"
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
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM avatars`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// row reads the stored row of userID straight from the database.
func (f fixture) row(t *testing.T, userID string) (image, source []byte, createdAt, updatedAt time.Time) {
	t.Helper()
	err := f.pool.QueryRow(context.Background(),
		`SELECT image, source_sha256, created_at, updated_at FROM avatars WHERE user_id = $1`, userID).
		Scan(&image, &source, &createdAt, &updatedAt)
	if err != nil {
		t.Fatalf("avatar row: %v", err)
	}
	return image, source, createdAt, updatedAt
}

// rowVersion identifies the stored row version: it changes whenever the row
// is rewritten, even with the same values.
func (f fixture) rowVersion(t *testing.T, userID string) string {
	t.Helper()
	var v string
	err := f.pool.QueryRow(context.Background(),
		`SELECT xmin::text || '/' || ctid::text FROM avatars WHERE user_id = $1`, userID).Scan(&v)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (f fixture) logged(what string) int { return strings.Count(f.logs.String(), what) }

// mustSave saves raw for userID and checks the answer is a stored picture.
func (f fixture) mustSave(t *testing.T, userID string, raw []byte) []byte {
	t.Helper()
	stored, err := f.svc.Save(context.Background(), userID, raw)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	decodeStored(t, stored)
	return stored
}

func (f fixture) requireStored(t *testing.T, userID string, want []byte) {
	t.Helper()
	got, err := f.svc.Get(context.Background(), userID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("the stored picture is not the one expected (%d bytes, want %d)", len(got), len(want))
	}
}

func TestGetBeforeAnythingWasSaved(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")

	if image, err := f.svc.Get(ctx, ana); !errors.Is(err, ErrNotFound) || image != nil {
		t.Errorf("Get = %d bytes, %v; want ErrNotFound", len(image), err)
	}
	if exists, err := f.svc.Exists(ctx, ana); err != nil || exists {
		t.Errorf("Exists = %v, %v; want false", exists, err)
	}
}

// The first save: the answer is the picture as stored, which is the
// normalized one and never the upload, beside the hash of the upload.
func TestSaveStoresTheNormalizedPicture(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	raw := encodePNG(t, scene(300, 200))

	stored := f.mustSave(t, ana, raw)
	requireQuadrants(t, decodeStored(t, stored), "stored")
	if !bytes.Equal(stored, mustNormalize(t, raw)) {
		t.Error("Save did not return what the pipeline makes of the upload")
	}
	f.requireStored(t, ana, stored)
	if exists, err := f.svc.Exists(ctx, ana); err != nil || !exists {
		t.Errorf("Exists = %v, %v; want true", exists, err)
	}

	image, source, createdAt, updatedAt := f.row(t, ana)
	if !bytes.Equal(image, stored) || bytes.Equal(image, raw) {
		t.Error("the row does not hold the normalized picture")
	}
	if want := sha256.Sum256(raw); !bytes.Equal(source, want[:]) {
		t.Error("the row does not hold the hash of the upload")
	}
	if createdAt.IsZero() || !updatedAt.Equal(createdAt) {
		t.Errorf("timestamps = %v, %v", createdAt, updatedAt)
	}
	if n := f.logged("avatar: saved"); n != 1 {
		t.Errorf("%d saves logged, want 1", n)
	}
}

// A different upload replaces the picture whole: one row, the new picture,
// and nothing of the earlier one left to read.
func TestSaveReplacesThePicture(t *testing.T) {
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	first := f.mustSave(t, ana, encodePNG(t, solid(64, 64, red)))
	_, firstSource, createdAt, firstUpdate := f.row(t, ana)

	second := f.mustSave(t, ana, encodeJPEG(t, solid(64, 64, blue)))
	if bytes.Equal(first, second) {
		t.Fatal("two different uploads gave the same picture")
	}
	f.requireStored(t, ana, second)
	requireColour(t, decodeStored(t, second), Size/2, Size/2, blue, "replaced")
	if n := f.count(t); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
	_, source, created, updated := f.row(t, ana)
	if bytes.Equal(source, firstSource) {
		t.Error("the hash is still the first upload's")
	}
	if !created.Equal(createdAt) || !updated.After(firstUpdate) {
		t.Errorf("after a replace: created %v (was %v), updated %v (was %v)", created, createdAt, updated, firstUpdate)
	}
	if n := f.logged("avatar: saved"); n != 2 {
		t.Errorf("%d saves logged, want 2", n)
	}
}

// A retried upload (a lost response, a 503, the resend after a 401) is a
// no-op: the same answer, no decoding, no new row version and no log line.
func TestSaveIsIdempotent(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	raw := orientedJPEG(t, scene(120, 80), 6)
	first := f.mustSave(t, ana, raw)
	version := f.rowVersion(t, ana)

	f.svc.normalizer.render = func(format, []byte) ([]byte, error) {
		t.Error("the same upload was decoded again")
		return nil, errors.New("decoded")
	}
	for range 3 {
		again, err := f.svc.Save(ctx, ana, raw)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(again, first) {
			t.Error("the replay answered with another picture")
		}
	}
	if got := f.rowVersion(t, ana); got != version {
		t.Errorf("an unchanged save rewrote the row: %s → %s", version, got)
	}
	if n := f.logged("avatar: saved"); n != 1 {
		t.Errorf("%d saves logged, want 1: the replays changed nothing", n)
	}

	// A real change still decodes, writes and is logged; and the first
	// upload sent after it is a change again, not a replay.
	f.svc.normalizer.render = render
	other := f.mustSave(t, ana, encodePNG(t, solid(32, 32, green)))
	if f.rowVersion(t, ana) == version {
		t.Error("a changed save did not write")
	}
	back := f.mustSave(t, ana, raw)
	if !bytes.Equal(back, first) || bytes.Equal(back, other) {
		t.Error("the first upload sent again did not give the first picture")
	}
	f.requireStored(t, ana, first)
	if n := f.logged("avatar: saved"); n != 3 {
		t.Errorf("%d saves logged, want 3", n)
	}
}

// The upload is recognised by its bytes: the same picture in another file is
// another upload, and a member's hash is never matched against another's.
func TestSaveRecognisesAnUploadByItsBytesAndItsOwner(t *testing.T) {
	f := newFixture(t)
	ana, ben := f.user(t, "ana@example.com"), f.user(t, "ben@example.com")
	raw := encodePNG(t, scene(64, 64))
	f.mustSave(t, ana, raw)
	version := f.rowVersion(t, ana)

	// One byte more after the end of the image: other bytes, the same pixels.
	f.mustSave(t, ana, append(append([]byte{}, raw...), 0))
	if f.rowVersion(t, ana) == version {
		t.Error("an upload with other bytes was taken for a replay")
	}

	// Ben uploading what Ana uploaded is Ben's first save.
	f.mustSave(t, ben, raw)
	if n := f.count(t); n != 2 {
		t.Errorf("rows = %d, want 2", n)
	}
	if n := f.logged("avatar: saved"); n != 3 {
		t.Errorf("%d saves logged, want 3", n)
	}
}

func TestDelete(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana, ben := f.user(t, "ana@example.com"), f.user(t, "ben@example.com")
	raw := encodePNG(t, scene(64, 64))
	bens := f.mustSave(t, ben, encodePNG(t, solid(64, 64, yellow)))

	// Without a picture: nothing to do, and nothing is logged.
	if err := f.svc.Delete(ctx, ana); err != nil {
		t.Fatalf("delete without a picture: %v", err)
	}
	if n := f.logged("avatar: removed"); n != 0 {
		t.Errorf("%d removals logged for a member with no picture", n)
	}

	f.mustSave(t, ana, raw)
	for range 2 {
		if err := f.svc.Delete(ctx, ana); err != nil {
			t.Fatalf("delete: %v", err)
		}
	}
	if _, err := f.svc.Get(ctx, ana); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete: err = %v, want ErrNotFound", err)
	}
	if exists, err := f.svc.Exists(ctx, ana); err != nil || exists {
		t.Errorf("Exists after delete = %v, %v", exists, err)
	}
	if n := f.logged("avatar: removed"); n != 1 {
		t.Errorf("%d removals logged, want 1: the second removed nothing", n)
	}
	// Only Ana's went.
	f.requireStored(t, ben, bens)
	if n := f.count(t); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}

	// The same upload after a removal is a save again, not a replay.
	f.requireStored(t, ana, f.mustSave(t, ana, raw))
}

func TestPicturesAreSeparatePerUser(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana, ben := f.user(t, "ana@example.com"), f.user(t, "ben@example.com")
	anas := f.mustSave(t, ana, encodePNG(t, solid(64, 64, red)))

	if _, err := f.svc.Get(ctx, ben); !errors.Is(err, ErrNotFound) {
		t.Errorf("ben, who has none: err = %v, want ErrNotFound", err)
	}
	bens := f.mustSave(t, ben, encodePNG(t, solid(64, 64, blue)))
	f.requireStored(t, ana, anas)
	f.requireStored(t, ben, bens)
	if bytes.Equal(anas, bens) {
		t.Error("two members' pictures are the same bytes")
	}
}

func TestSaveForADeletedUser(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	if _, err := f.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ana); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.Save(ctx, ana, encodePNG(t, scene(64, 64))); !errors.Is(err, ErrUserGone) {
		t.Errorf("err = %v, want ErrUserGone", err)
	}
	if n := f.count(t); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
	if n := f.logged("avatar: saved"); n != 0 {
		t.Errorf("%d saves logged for a user that is gone", n)
	}
	// Removing is still not an error: there is nothing, as asked.
	if err := f.svc.Delete(ctx, ana); err != nil {
		t.Errorf("delete for a deleted user: %v", err)
	}
}

func TestDeletingAUserRemovesTheirPicture(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	f.mustSave(t, ana, encodePNG(t, scene(64, 64)))

	if _, err := f.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, ana); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t); n != 0 {
		t.Errorf("rows = %d after the user was deleted, want 0", n)
	}
	if _, err := f.svc.Get(ctx, ana); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get: err = %v, want ErrNotFound", err)
	}
}

// Whatever makes a save fail, the picture the member had is still theirs,
// whole, and nothing is logged as saved.
func TestAFailedSaveLeavesTheEarlierPicture(t *testing.T) {
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	earlier := f.mustSave(t, ana, encodePNG(t, scene(64, 64)))
	version := f.rowVersion(t, ana)
	pngFile := encodePNG(t, solid(64, 64, green))

	for reason, raw := range map[*FieldError][]byte{
		ErrRequired:           nil,
		ErrTooLarge:           make([]byte, MaxUploadBytes+1),
		ErrUnsupportedType:    []byte("GIF89a, and then some"),
		ErrInvalidImage:       pngFile[:len(pngFile)-30],
		ErrDimensionsTooLarge: pngHeaderOnly(MaxDimension+1, 1),
	} {
		stored, err := f.svc.Save(context.Background(), ana, raw)
		requireRefused(t, err, reason)
		if stored != nil {
			t.Errorf("%v: a refused upload returned a picture", reason)
		}
	}

	ended, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.svc.Save(ended, ana, pngFile); !errors.Is(err, context.Canceled) {
		t.Errorf("ended context: err = %v, want context.Canceled", err)
	}

	f.svc.normalizer.render = func(format, []byte) ([]byte, error) { return nil, errors.New("the encoder failed") }
	if _, err := f.svc.Save(context.Background(), ana, pngFile); err == nil {
		t.Error("a save whose picture could not be produced succeeded")
	}

	f.requireStored(t, ana, earlier)
	if got := f.rowVersion(t, ana); got != version {
		t.Errorf("a failed save rewrote the row: %s → %s", version, got)
	}
	if n := f.logged("avatar: saved"); n != 1 {
		t.Errorf("%d saves logged, want 1", n)
	}
}

// A refused upload is refused before the database is asked anything: the
// service here has no pool, so any query would panic.
func TestSaveRefusesBeforeAnyQuery(t *testing.T) {
	svc := NewService(nil, slog.New(slog.DiscardHandler))
	for reason, raw := range map[*FieldError][]byte{
		ErrRequired:           {},
		ErrTooLarge:           make([]byte, MaxUploadBytes+1),
		ErrUnsupportedType:    []byte("%PDF-1.7"),
		ErrInvalidImage:       pngSignature,
		ErrDimensionsTooLarge: pngHeaderOnly(1, MaxDimension+1),
	} {
		_, err := svc.Save(context.Background(), unknownUser, raw)
		requireRefused(t, err, reason)
	}
}

const unknownUser = "00000000-0000-4000-8000-000000000000"

// With every decode slot held, a save waits the queue timeout and then
// answers ErrOverloaded, having written nothing. What needs no decoding
// still works meanwhile: reads, a removal, and the replay of an upload.
func TestSaveIsOverloadedWhenEverySlotIsHeld(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana, ben := f.user(t, "ana@example.com"), f.user(t, "ben@example.com")
	raw := encodePNG(t, scene(64, 64))
	earlier := f.mustSave(t, ana, raw)
	version := f.rowVersion(t, ana)

	const wait = 60 * time.Millisecond
	f.svc.normalizer = held(decodeSlots, wait)

	for name, userID := range map[string]string{"a member with a picture": ana, "a member without one": ben} {
		start := time.Now()
		stored, err := f.svc.Save(ctx, userID, encodePNG(t, solid(64, 64, blue)))
		waited := time.Since(start)
		if !errors.Is(err, ErrOverloaded) || stored != nil {
			t.Fatalf("%s: Save = %d bytes, %v; want ErrOverloaded", name, len(stored), err)
		}
		var verr *ValidationError
		if errors.As(err, &verr) {
			t.Errorf("%s: ErrOverloaded is a validation error", name)
		}
		if waited < wait || waited > 5*time.Second {
			t.Errorf("%s: gave up after %v, want after the %v queue timeout", name, waited, wait)
		}
	}
	if n := f.count(t); n != 1 {
		t.Errorf("rows = %d, want 1: an overloaded save wrote", n)
	}
	if got := f.rowVersion(t, ana); got != version {
		t.Errorf("an overloaded save rewrote the row: %s → %s", version, got)
	}
	if n := f.logged("avatar: saved"); n != 1 {
		t.Errorf("%d saves logged, want 1", n)
	}
	if len(f.svc.normalizer.slots) != decodeSlots {
		t.Errorf("%d slots held, want %d", len(f.svc.normalizer.slots), decodeSlots)
	}

	// A request whose context ends while it waits gives up at once.
	f.svc.normalizer = held(decodeSlots, time.Hour)
	deadline, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err := f.svc.Save(deadline, ben, raw); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("deadline while queued: err = %v, want context.DeadlineExceeded", err)
	}

	// No slot is needed to read, to replay or to remove.
	f.requireStored(t, ana, earlier)
	again, err := f.svc.Save(ctx, ana, raw)
	if err != nil || !bytes.Equal(again, earlier) {
		t.Errorf("replay while overloaded: %d bytes, %v", len(again), err)
	}
	if err := f.svc.Delete(ctx, ana); err != nil {
		t.Errorf("delete while overloaded: %v", err)
	}
	if n := f.count(t); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

func TestAnEndedContextReadsAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	stored := f.mustSave(t, ana, encodePNG(t, scene(64, 64)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := f.svc.Get(ctx, ana); !errors.Is(err, context.Canceled) {
		t.Errorf("Get err = %v, want context.Canceled", err)
	}
	if _, err := f.svc.Exists(ctx, ana); !errors.Is(err, context.Canceled) {
		t.Errorf("Exists err = %v, want context.Canceled", err)
	}
	if err := f.svc.Delete(ctx, ana); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete err = %v, want context.Canceled", err)
	}
	if _, err := f.svc.Save(ctx, ana, encodePNG(t, solid(64, 64, red))); !errors.Is(err, context.Canceled) {
		t.Errorf("Save err = %v, want context.Canceled", err)
	}
	f.requireStored(t, ana, stored)
}

// shades are n uploads that differ from one another, each a plain colour.
func shades(t *testing.T, n int) [][]byte {
	t.Helper()
	uploads := make([][]byte, n)
	for i := range n {
		uploads[i] = encodePNG(t, solid(48, 48, color.NRGBA{R: uint8(i * 15), G: uint8(255 - i*15), B: 128, A: 255}))
	}
	return uploads
}

// Concurrent uploads of different pictures, the very first ones included,
// all succeed, each answered with its own picture, and leave one row holding
// one upload's picture together with that upload's hash, never a mix of two.
func TestConcurrentSavesLeaveOneWholePicture(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")

	const n = 16
	uploads := shades(t, n)
	results := make([][]byte, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			results[i], errs[i] = f.svc.Save(ctx, ana, uploads[i])
		})
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
		if !bytes.Equal(results[i], mustNormalize(t, uploads[i])) {
			t.Errorf("save %d was answered with another upload's picture", i)
		}
	}
	if got := f.count(t); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}
	image, source, createdAt, updatedAt := f.row(t, ana)
	decodeStored(t, image)
	winner := -1
	for i, raw := range uploads {
		if sum := sha256.Sum256(raw); bytes.Equal(source, sum[:]) {
			winner = i
		}
	}
	if winner < 0 {
		t.Fatal("the stored hash is of none of the uploads")
	}
	if !bytes.Equal(image, results[winner]) {
		t.Errorf("the row mixes two saves: the hash of upload %d beside another picture", winner)
	}
	if updatedAt.Before(createdAt) {
		t.Errorf("updated_at %v before created_at %v", updatedAt, createdAt)
	}
	if got := f.logged("avatar: saved"); got != n {
		t.Errorf("%d saves logged, want %d: each one changed the picture", got, n)
	}
}

// Several identical first uploads at once (a client retrying while its first
// request is still running): all succeed with the same picture, one row,
// written once.
func TestConcurrentIdenticalSavesAllSucceed(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	raw := encodePNG(t, scene(64, 64))

	const n = 16
	results := make([][]byte, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			results[i], errs[i] = f.svc.Save(ctx, ana, raw)
		})
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
		if !bytes.Equal(results[i], results[0]) {
			t.Errorf("save %d returned another picture than save 0", i)
		}
	}
	if got := f.count(t); got != 1 {
		t.Errorf("rows = %d, want 1", got)
	}
	f.requireStored(t, ana, results[0])
	if got := f.logged("avatar: saved"); got != 1 {
		t.Errorf("%d saves logged, want 1: only one of them changed anything", got)
	}
}

// The logs say whose picture changed and nothing about the image: no byte of
// the upload or of the picture, no size, no dimensions, no format, no hash.
// Only a save and a removal that changed something are logged at all.
func TestLogsNameTheUserAndNothingAboutTheImage(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	ana := f.user(t, "ana@example.com")
	const marker = "MARKERCOMMENT"
	raw := withPNGChunks(encodePNG(t, scene(300, 200)), pngChunk("tEXt", []byte("Comment\x00"+marker)))

	f.mustSave(t, ana, raw)
	f.mustSave(t, ana, raw)
	_, _ = f.svc.Save(ctx, ana, []byte(marker))
	_, _ = f.svc.Save(ctx, ana, raw[:len(raw)-30])
	if _, err := f.svc.Get(ctx, ana); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Exists(ctx, ana); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Delete(ctx, ana); err != nil {
		t.Fatal(err)
	}

	logs := f.logs.String()
	lines := strings.Split(strings.TrimSpace(logs), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "avatar: saved") || !strings.Contains(lines[1], "avatar: removed") {
		t.Fatalf("logs = %s, want one save and one removal", logs)
	}
	for _, line := range lines {
		var fields map[string]any
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			t.Fatalf("log line %s: %v", line, err)
		}
		// Whose picture changed, and nothing else: no size, no dimensions,
		// no format, no hash.
		if keys := slices.Sorted(maps.Keys(fields)); !slices.Equal(keys, []string{"level", "msg", "time", "user_id"}) {
			t.Errorf("the line records %v: %s", keys, line)
		}
		if fields["user_id"] != ana {
			t.Errorf("the line does not name the user: %s", line)
		}
	}
	sum := sha256.Sum256(raw)
	for what, private := range map[string]string{
		"text of the upload": marker,
		"the format":         "png",
		"the stored format":  "jpeg",
		"the hash":           hex.EncodeToString(sum[:]),
	} {
		if strings.Contains(strings.ToLower(logs), strings.ToLower(private)) {
			t.Errorf("logs contain %s (%q): %s", what, private, logs)
		}
	}
}
