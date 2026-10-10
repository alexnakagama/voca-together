package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"vocatogether/backend/internal/auth"
	"vocatogether/backend/internal/profile"
	"vocatogether/backend/internal/ratelimit"
	"vocatogether/backend/internal/safety"
)

const (
	blocksPath = "/v1/me/blocks"
	noBlocks   = `{"blocks":[]}`
)

var (
	memberSelf    = invalidField("member", "self")
	blocksTooMany = invalidField("blocks", "too_many")
)

func (a testAPI) putBlock(accessToken, id string) *httptest.ResponseRecorder {
	return a.profileWith(http.MethodPut, blocksPath+"/"+id, "", "Bearer "+accessToken)
}

func (a testAPI) deleteBlock(accessToken, id string) *httptest.ResponseRecorder {
	return a.profileWith(http.MethodDelete, blocksPath+"/"+id, "", "Bearer "+accessToken)
}

func (a testAPI) getBlocks(accessToken string) *httptest.ResponseRecorder {
	return a.profileWith(http.MethodGet, blocksPath, "", "Bearer "+accessToken)
}

func (a testAPI) safetyService() *safety.Service {
	return safety.NewService(a.pool, slog.New(slog.DiscardHandler))
}

func (a testAPI) blockCount(t *testing.T) int {
	t.Helper()
	return a.count(t, "blocks")
}

// hasBlock reports whether the account blocker has a stored block of the
// account blocked, read straight from the database.
func (a testAPI) hasBlock(t *testing.T, blocker, blocked string) bool {
	t.Helper()
	var found bool
	err := a.pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM blocks b JOIN users r ON r.id = b.blocker_id JOIN users d ON d.id = b.blocked_id
		                WHERE r.email = $1 AND d.email = $2)`, blocker, blocked).Scan(&found)
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// blocksInvolving counts the blocks addr made or was the target of.
func (a testAPI) blocksInvolving(t *testing.T, addr string) int {
	t.Helper()
	var n int
	err := a.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM blocks b JOIN users u ON u.id IN (b.blocker_id, b.blocked_id) WHERE u.email = $1`,
		addr).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// blockFillers makes blocker block n new accounts that have no profile, with
// no request: for reaching the limit.
func (a testAPI) blockFillers(t *testing.T, blocker string, n int) {
	t.Helper()
	a.exec(t,
		`WITH filler AS (
		     INSERT INTO users (email, password_hash, email_verified_at)
		     SELECT 'filler' || i || '@example.com', 'x', now() FROM generate_series(1, $2::int) AS i
		     RETURNING id)
		 INSERT INTO blocks (blocker_id, blocked_id)
		 SELECT u.id, filler.id FROM filler, users u WHERE u.email = $1`, blocker, n)
}

// userLimiter is a per-user limiter that allows burst requests and then, for
// the length of a test, no more.
func userLimiter(burst int) *ratelimit.Limiter[string] {
	return ratelimit.New[string]("test", burst, time.Hour, 100, slog.New(slog.DiscardHandler))
}

// requireBlockDone checks the one answer of a block or an unblock that was
// not refused: 204, no body, and no header beyond the ones every protected
// response has.
func requireBlockDone(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	requireLoggedOut(t, rec)
}

// blocksJSON is a whole list response, from pairs of a public id and a name.
func blocksJSON(t *testing.T, pairs ...string) string {
	t.Helper()
	entries := make([]string, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		name, err := json.Marshal(pairs[i+1])
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, `{"id":"`+pairs[i]+`","display_name":`+string(name)+`}`)
	}
	return `{"blocks":[` + strings.Join(entries, ",") + `]}`
}

// requireBlocks checks a successful list response: exactly want, never
// cached, with one key, and exactly the two public keys in every entry.
func requireBlocks(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	requireLoginResponse(t, rec, http.StatusOK, want)
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	if keys := slices.Sorted(maps.Keys(body)); !slices.Equal(keys, []string{"blocks"}) {
		t.Errorf("fields = %v, want only blocks", keys)
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(body["blocks"], &entries); err != nil || entries == nil {
		t.Fatalf("blocks = %s, want an array (%v)", body["blocks"], err)
	}
	for _, entry := range entries {
		if keys := slices.Sorted(maps.Keys(entry)); !slices.Equal(keys, []string{"display_name", "id"}) {
			t.Errorf("entry fields = %v, want exactly id and display_name", keys)
		}
	}
}

// requireSameResponse checks that got is byte for byte the answer want is:
// status, body and headers.
func requireSameResponse(t *testing.T, what string, got, want *httptest.ResponseRecorder) {
	t.Helper()
	if got.Code != want.Code || got.Body.String() != want.Body.String() || fmt.Sprint(got.Header()) != fmt.Sprint(want.Header()) {
		t.Errorf("%s answers differently:\n%d %v %q\nvs\n%d %v %q",
			what, got.Code, got.Header(), got.Body, want.Code, want.Header(), want.Body)
	}
}

// blockHandlerRequest is a block request for id straight to a handler, as
// the mux and requireAccessToken would leave it.
func blockHandlerRequest(method, id, callerID string) *http.Request {
	req := httptest.NewRequest(method, blocksPath+"/x", nil)
	req.SetPathValue("id", id)
	return as(req, callerID)
}

// ---- Blocking a member ----

func TestPutBlockBlocksTheMember(t *testing.T) {
	api := newTestAPI(t)
	ana, _ := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	requireBlocks(t, api.getBlocks(ana.AccessToken), noBlocks)

	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))

	requireBlocks(t, api.getBlocks(ana.AccessToken), blocksJSON(t, benID, "Ben"))
	if !api.hasBlock(t, "ana@example.com", "ben@example.com") || api.blockCount(t) != 1 {
		t.Errorf("stored blocks = %d, want only ana's of ben", api.blockCount(t))
	}
}

// Blocking needs a session, not a profile: a member who has saved none can
// block one who has.
func TestPutBlockByAMemberWithoutAProfile(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com") // no profile
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")

	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	requireBlocks(t, api.getBlocks(ana.AccessToken), blocksJSON(t, benID, "Ben"))
	if !api.hasBlock(t, "ana@example.com", "ben@example.com") {
		t.Error("ben is not blocked by ana")
	}
}

// Who blocks is the session and whom is the path: another member's
// identifiers in the query string or in a body name nobody, for a block and
// for an unblock.
func TestBlockBelongsToTheCallerOnly(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	cho, choID := api.memberWithProfile(t, "cho@example.com", "Cho", "")
	choAccount := api.userID(t, "cho@example.com")
	query := "?id=" + choID + "&blocker_id=" + choAccount + "&blocked_id=" + choID + "&user_id=" + choAccount
	body := `{"id":"` + choID + `","blocker_id":"` + choAccount + `","blocked_id":"` + choID + `","user_id":"` + choAccount + `"}`

	requireBlockDone(t, api.profileWith(http.MethodPut, blocksPath+"/"+benID+query, body, "Bearer "+ana.AccessToken))
	if !api.hasBlock(t, "ana@example.com", "ben@example.com") || api.blockCount(t) != 1 {
		t.Errorf("stored blocks = %d, want only ana's of ben", api.blockCount(t))
	}
	if n := api.blocksInvolving(t, "cho@example.com"); n != 0 {
		t.Errorf("cho is in %d blocks, want none", n)
	}
	requireBlocks(t, api.getBlocks(cho.AccessToken), noBlocks)

	// The body is ignored whatever it is: nothing in it is read.
	for _, junk := range []string{`not json`, `{"unknown":true}`, `[]`, strings.Repeat("x", 128<<10)} {
		requireBlockDone(t, api.profileWith(http.MethodPut, blocksPath+"/"+benID, junk, "Bearer "+ana.AccessToken))
	}

	// Cho blocks Ana; Ana's unblock of Ben, naming Cho, removes only Ana's.
	requireBlockDone(t, api.putBlock(cho.AccessToken, anaID))
	requireBlockDone(t, api.profileWith(http.MethodDelete, blocksPath+"/"+benID+query, body, "Bearer "+ana.AccessToken))
	if api.hasBlock(t, "ana@example.com", "ben@example.com") {
		t.Error("ana's block of ben was not removed")
	}
	if !api.hasBlock(t, "cho@example.com", "ana@example.com") || api.blockCount(t) != 1 {
		t.Errorf("stored blocks = %d, want only cho's of ana", api.blockCount(t))
	}
}

// ---- Unblocking a member ----

func TestDeleteBlockUnblocks(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	_, choID := api.memberWithProfile(t, "cho@example.com", "Cho", "")
	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	requireMemberNotFound(t, api.getMember(ben.AccessToken, anaID))

	requireBlockDone(t, api.deleteBlock(ana.AccessToken, benID))

	requireBlocks(t, api.getBlocks(ana.AccessToken), noBlocks)
	requireMember(t, api.getMember(ana.AccessToken, benID), memberJSON(t, benID, "Ben", "", noLanguages))
	requireMember(t, api.getMember(ben.AccessToken, anaID), memberJSON(t, anaID, "Ana", "", noLanguages))
	if n := api.blockCount(t); n != 0 {
		t.Errorf("blocks = %d, want 0", n)
	}

	// Unblocking someone who was never blocked is the same answer and
	// changes nothing.
	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	requireBlockDone(t, api.deleteBlock(ana.AccessToken, choID))
	requireBlocks(t, api.getBlocks(ana.AccessToken), blocksJSON(t, benID, "Ben"))
	if n := api.blockCount(t); n != 1 {
		t.Errorf("blocks = %d, want 1", n)
	}
}

// An unblock removes the caller's block and never the one the other member
// made: the two stay hidden from each other.
func TestDeleteBlockLeavesTheOtherMembersBlock(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	requireBlockDone(t, api.putBlock(ben.AccessToken, anaID))

	requireBlockDone(t, api.deleteBlock(ana.AccessToken, benID))

	if api.hasBlock(t, "ana@example.com", "ben@example.com") || !api.hasBlock(t, "ben@example.com", "ana@example.com") {
		t.Error("want ana's block gone and ben's still stored")
	}
	requireBlocks(t, api.getBlocks(ana.AccessToken), noBlocks)
	requireBlocks(t, api.getBlocks(ben.AccessToken), blocksJSON(t, anaID, "Ana"))
	requireMemberNotFound(t, api.getMember(ana.AccessToken, benID))
	requireMemberNotFound(t, api.getMember(ben.AccessToken, anaID))
}

// ---- Blocking is idempotent ----

func TestPutBlockIsIdempotent(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	_, choID := api.memberWithProfile(t, "cho@example.com", "Cho", "")

	first := api.putBlock(ana.AccessToken, benID)
	requireBlockDone(t, first)
	requireBlockDone(t, api.putBlock(ana.AccessToken, choID))
	want := blocksJSON(t, choID, "Cho", benID, "Ben")
	requireBlocks(t, api.getBlocks(ana.AccessToken), want)
	var stored time.Time
	storedAt := `SELECT b.created_at FROM blocks b JOIN users d ON d.id = b.blocked_id WHERE d.email = 'ben@example.com'`
	if err := api.pool.QueryRow(context.Background(), storedAt).Scan(&stored); err != nil {
		t.Fatal(err)
	}

	// The same block again: the same answer, Ben once, and in the same place.
	for range 3 {
		again := api.putBlock(ana.AccessToken, benID)
		requireBlockDone(t, again)
		requireSameResponse(t, "a repeated block", again, first)
	}
	requireBlocks(t, api.getBlocks(ana.AccessToken), want)
	var after time.Time
	if err := api.pool.QueryRow(context.Background(), storedAt).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !after.Equal(stored) || api.blockCount(t) != 2 {
		t.Errorf("a repeat rewrote the block: %v → %v, %d blocks", stored, after, api.blockCount(t))
	}

	// The same unblock again: the same answer too.
	removed := api.deleteBlock(ana.AccessToken, benID)
	requireBlockDone(t, removed)
	for range 3 {
		again := api.deleteBlock(ana.AccessToken, benID)
		requireBlockDone(t, again)
		requireSameResponse(t, "a repeated unblock", again, removed)
	}
	requireBlocks(t, api.getBlocks(ana.AccessToken), blocksJSON(t, choID, "Cho"))
}

// The same block sent many times at once, from two of the member's sessions,
// is answered 204 every time and stored once.
func TestConcurrentIdenticalBlocksOverHTTP(t *testing.T) {
	api := newTestAPI(t)
	first := api.loggedIn(t, "ana@example.com")
	second := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")

	const n = 16
	recs := make([]*httptest.ResponseRecorder, n)
	var wg sync.WaitGroup
	for i := range n {
		token := first.AccessToken
		if i%2 == 1 {
			token = second.AccessToken
		}
		wg.Go(func() { recs[i] = api.putBlock(token, benID) })
	}
	wg.Wait()

	for _, rec := range recs {
		requireBlockDone(t, rec)
	}
	if n := api.blockCount(t); n != 1 {
		t.Errorf("blocks = %d, want 1", n)
	}
	requireBlocks(t, api.getBlocks(second.AccessToken), blocksJSON(t, benID, "Ben"))
}

// Two members blocking each other at the same moment both succeed.
func TestConcurrentMutualBlocksOverHTTP(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")

	for range 8 {
		var a, b *httptest.ResponseRecorder
		var wg sync.WaitGroup
		wg.Go(func() { a = api.putBlock(ana.AccessToken, benID) })
		wg.Go(func() { b = api.putBlock(ben.AccessToken, anaID) })
		wg.Wait()
		requireBlockDone(t, a)
		requireBlockDone(t, b)
		if !api.hasBlock(t, "ana@example.com", "ben@example.com") || !api.hasBlock(t, "ben@example.com", "ana@example.com") {
			t.Fatalf("blocks = %d, want both", api.blockCount(t))
		}
		requireBlockDone(t, api.deleteBlock(ana.AccessToken, benID))
		requireBlockDone(t, api.deleteBlock(ben.AccessToken, anaID))
	}
}

// A block and an unblock of one member at the same moment both answer 204,
// and the member is then either blocked or not: one row or none.
func TestConcurrentBlockAndUnblockOverHTTP(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")

	for range 8 {
		var put, del *httptest.ResponseRecorder
		var wg sync.WaitGroup
		wg.Go(func() { put = api.putBlock(ana.AccessToken, benID) })
		wg.Go(func() { del = api.deleteBlock(ana.AccessToken, benID) })
		wg.Wait()
		requireBlockDone(t, put)
		requireBlockDone(t, del)

		want := noBlocks
		if api.hasBlock(t, "ana@example.com", "ben@example.com") {
			want = blocksJSON(t, benID, "Ben")
		}
		requireBlocks(t, api.getBlocks(ana.AccessToken), want)
		if n := api.blockCount(t); n > 1 {
			t.Fatalf("blocks = %d, want at most 1", n)
		}
	}
}

// ---- A member cannot block themselves ----

func TestBlockingOneselfIsRefused(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))

	requireLoginResponse(t, api.putBlock(ana.AccessToken, anaID), http.StatusUnprocessableEntity, memberSelf)
	requireLoginResponse(t, api.deleteBlock(ana.AccessToken, anaID), http.StatusUnprocessableEntity, memberSelf)

	requireBlocks(t, api.getBlocks(ana.AccessToken), blocksJSON(t, benID, "Ben"))
	if n := api.blockCount(t); n != 1 {
		t.Errorf("blocks = %d, want 1", n)
	}
	// Their own profile is as readable as before.
	requireMember(t, api.getMember(ana.AccessToken, anaID), memberJSON(t, anaID, "Ana", "", noLanguages))
}

// ---- A block write does not reveal whether a profile exists ----

// A block and an unblock answer the same 204, in status, body and headers,
// whether the identifier names a member, names nobody or is not an
// identifier, and only the first stores anything.
func TestBlockWritesAnswerTheSameForEveryIdentifier(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	api.loggedIn(t, "cho@example.com") // an account that has saved no profile

	existing := api.putBlock(ana.AccessToken, benID)
	requireBlockDone(t, existing)
	misses := map[string]string{
		"a well-formed id nobody has":     unknownMemberID,
		"not an id":                       "not-an-id",
		"upper case":                      strings.ToUpper(benID),
		"own id in upper case":            strings.ToUpper(anaID),
		"braces":                          "%7B" + benID + "%7D",
		"no hyphens":                      strings.ReplaceAll(benID, "-", ""),
		"one character short":             benID[:35],
		"one character long":              benID + "0",
		"trailing space":                  benID + "%20",
		"another member's account id":     api.userID(t, "ben@example.com"),
		"the caller's account id":         api.userID(t, "ana@example.com"),
		"an account id without a profile": api.userID(t, "cho@example.com"),
		"an email address":                "ben%40example.com",
		"me":                              "me",
	}
	for name, id := range misses {
		t.Run("block "+name, func(t *testing.T) {
			rec := api.putBlock(ana.AccessToken, id)
			requireBlockDone(t, rec)
			requireSameResponse(t, name, rec, existing)
		})
	}
	if !api.hasBlock(t, "ana@example.com", "ben@example.com") || api.blockCount(t) != 1 {
		t.Errorf("blocks = %d, want only ana's of ben", api.blockCount(t))
	}

	for name, id := range misses {
		t.Run("unblock "+name, func(t *testing.T) {
			rec := api.deleteBlock(ana.AccessToken, id)
			requireBlockDone(t, rec)
			requireSameResponse(t, name, rec, existing)
		})
	}
	if n := api.blockCount(t); n != 1 {
		t.Errorf("an unblock of nobody removed a block: %d left", n)
	}
	removed := api.deleteBlock(ana.AccessToken, benID)
	requireBlockDone(t, removed)
	requireSameResponse(t, "the unblock of an existing member", removed, existing)
	if n := api.blockCount(t); n != 0 {
		t.Errorf("blocks = %d, want 0", n)
	}
}

// A member can block a member who has blocked them, and the answer is the
// one an unknown identifier gets: a write is no way to confirm a block.
func TestBlockingAMemberWhoBlockedYou(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	requireBlockDone(t, api.putBlock(ben.AccessToken, anaID))
	requireMemberNotFound(t, api.getMember(ana.AccessToken, benID))

	unknown := api.putBlock(ana.AccessToken, unknownMemberID)
	rec := api.putBlock(ana.AccessToken, benID)
	requireBlockDone(t, rec)
	requireSameResponse(t, "blocking a member who blocked the caller", rec, unknown)

	if !api.hasBlock(t, "ana@example.com", "ben@example.com") || !api.hasBlock(t, "ben@example.com", "ana@example.com") {
		t.Errorf("blocks = %d, want one each way", api.blockCount(t))
	}
}

// A malformed id is answered without asking the database: the services here
// have no pool, so any query would panic.
func TestBlockWritesDoNotQueryForAMalformedID(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	profiles, safetySvc := profile.NewService(nil, logger), safety.NewService(nil, logger)

	const lettered = "abcdefab-cdef-4bcd-8fab-cdefabcdefab"
	for method, h := range map[string]http.Handler{
		http.MethodPut:    handlePutBlock(logger, profiles, safetySvc),
		http.MethodDelete: handleDeleteBlock(logger, profiles, safetySvc),
	} {
		for _, id := range []string{"", "not-an-id", strings.ToUpper(lettered), lettered + " ", "{" + lettered + "}", lettered[:35]} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, blockHandlerRequest(method, id, unknownMemberID))
			if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
				t.Errorf("%s %q: %d %q, want 204 and no body", method, id, rec.Code, rec.Body)
			}
		}
	}
}

// ---- Listing the members one has blocked ----

func TestGetBlocksListsTheCallersBlocksNewestFirst(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben López", "Ben's bio")
	cho, choID := api.memberWithProfile(t, "cho@example.com", "Cho", "")
	dee, _ := api.memberWithProfile(t, "dee@example.com", "Dee", "")

	// Nobody blocked: an empty array, not null and not a missing key.
	empty := api.getBlocks(ana.AccessToken)
	requireBlocks(t, empty, noBlocks)
	if empty.Body.String() != noBlocks+"\n" {
		t.Errorf("body = %q", empty.Body)
	}

	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	requireBlockDone(t, api.putBlock(ana.AccessToken, choID))
	// Blocks others made, of Ana and of somebody else, are theirs.
	requireBlockDone(t, api.putBlock(dee.AccessToken, anaID))
	requireBlockDone(t, api.putBlock(cho.AccessToken, benID))

	requireBlocks(t, api.getBlocks(ana.AccessToken), blocksJSON(t, choID, "Cho", benID, "Ben López"))
	requireBlocks(t, api.getBlocks(dee.AccessToken), blocksJSON(t, anaID, "Ana"))
	requireBlocks(t, api.getBlocks(cho.AccessToken), blocksJSON(t, benID, "Ben López"))
	// Ben is blocked by two members and has blocked nobody: he sees nothing.
	requireBlocks(t, api.getBlocks(ben.AccessToken), noBlocks)

	// A query string names nobody: the list is the session's.
	requireBlocks(t, api.profileWith(http.MethodGet, blocksPath+"?id="+anaID+"&user_id="+api.userID(t, "ana@example.com"), "",
		"Bearer "+ben.AccessToken), noBlocks)

	// The name is the member's current one. Ben can't be read by Ana, and
	// his edits are his own business; the list still names him as he is now.
	requireProfile(t, api.putProfile(ben.AccessToken, profileJSON(t, "Benjamín", "")))
	requireBlocks(t, api.getBlocks(ana.AccessToken), blocksJSON(t, choID, "Cho", benID, "Benjamín"))

	// Reading changed nothing.
	if n := api.blockCount(t); n != 4 {
		t.Errorf("blocks = %d, want 4", n)
	}
}

// The list holds a public id and a name and nothing else about anybody: no
// account id, email address or timestamp, and nothing of the profile's text.
func TestGetBlocksDisclosesNothingPrivate(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "markerblocker@example.com")
	ben, benID := api.memberWithProfile(t, "markerblocked@example.com", "Ben", "MARKERBIO")
	languages := languagesJSON(entriesJSON("yue", "native"), "[]")
	requireLanguages(t, api.putLanguages(ben.AccessToken, languages), languages)
	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))

	rec := api.getBlocks(ana.AccessToken)
	requireBlocks(t, rec, blocksJSON(t, benID, "Ben"))
	answer := strings.ToLower(rec.Body.String() + fmt.Sprint(rec.Header()))
	for what, private := range map[string]string{
		"an email address":         "marker",
		"the email domain":         "example.com",
		"the blocked account's id": api.userID(t, "markerblocked@example.com"),
		"the caller's account id":  api.userID(t, "markerblocker@example.com"),
		"a timestamp":              "_at",
		"a token":                  "vt_",
		"a language":               "yue",
		"the picture flag":         "avatar",
	} {
		if strings.Contains(answer, private) {
			t.Errorf("the list holds %s: %s", what, answer)
		}
	}
}

// A blocked member whose profile no longer exists has nothing to be named
// by: they leave the list, the block holds, and a profile saved again shows
// them under its new identifier.
func TestGetBlocksOmitsAMemberWithoutAProfile(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	_, choID := api.memberWithProfile(t, "cho@example.com", "Cho", "")
	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	requireBlockDone(t, api.putBlock(ana.AccessToken, choID))

	api.exec(t, `DELETE FROM profiles p USING users u WHERE u.id = p.user_id AND u.email = 'ben@example.com'`)

	requireBlocks(t, api.getBlocks(ana.AccessToken), blocksJSON(t, choID, "Cho"))
	if !api.hasBlock(t, "ana@example.com", "ben@example.com") {
		t.Fatal("the block went with the profile")
	}

	saved := requireProfile(t, api.putProfile(ben.AccessToken, profileJSON(t, "Ben again", "")))
	if saved.ID == benID {
		t.Fatal("the new profile has the old public id")
	}
	requireBlocks(t, api.getBlocks(ana.AccessToken), blocksJSON(t, choID, "Cho", saved.ID, "Ben again"))
	requireMemberNotFound(t, api.getMember(ana.AccessToken, saved.ID))
	requireMemberNotFound(t, api.getMember(ben.AccessToken, anaID))
}

// A full list, of members whose names are the longest there can be, is one
// response under the 64 KiB a client reads: the reason there is a limit and
// no paging. The longest are 50 characters of four bytes, and names of the
// characters encoding/json would by default write as six-byte escapes, which
// this response writes as they are.
func TestGetBlocksAtTheLimitIsOneSmallResponse(t *testing.T) {
	for what, name := range map[string]string{
		"four bytes a character": strings.Repeat("𠮷", profile.DisplayNameMaxLength),
		"ampersands":             "a" + strings.Repeat("&", profile.DisplayNameMaxLength-1),
		"angle brackets":         "a" + strings.Repeat("<>", (profile.DisplayNameMaxLength-1)/2),
	} {
		t.Run(what, func(t *testing.T) { requireFullListIsSmall(t, name) })
	}
}

func requireFullListIsSmall(t *testing.T, name string) {
	t.Helper()
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com")
	if saved := requireProfile(t, api.putProfile(ana.AccessToken, profileJSON(t, name, ""))); saved.DisplayName != name {
		t.Fatalf("%q is not a name a member can save as it is: stored %q", name, saved.DisplayName)
	}
	api.exec(t,
		`WITH member AS (
		     INSERT INTO users (email, password_hash, email_verified_at)
		     SELECT 'member' || i || '@example.com', 'x', now() FROM generate_series(1, $2::int) AS i
		     RETURNING id),
		 named AS (
		     INSERT INTO profiles (user_id, display_name) SELECT id, $3 FROM member RETURNING user_id)
		 INSERT INTO blocks (blocker_id, blocked_id, created_at)
		 SELECT u.id, named.user_id, now() - make_interval(secs => row_number() OVER ())
		 FROM named, users u WHERE u.email = $1`, "ana@example.com", safety.MaxBlocks, name)

	rec := api.getBlocks(ana.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body %s", rec.Code, rec.Body)
	}
	var body struct {
		Blocks []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Blocks) != safety.MaxBlocks {
		t.Fatalf("entries = %d, want %d", len(body.Blocks), safety.MaxBlocks)
	}
	if size := rec.Body.Len(); size >= 64<<10 {
		t.Errorf("a full list is %d bytes, want under 64 KiB", size)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	// Every entry is a different member, in the order of the blocks.
	var want []string
	rows, err := api.pool.Query(context.Background(),
		`SELECT p.public_id::text FROM blocks b JOIN profiles p ON p.user_id = b.blocked_id ORDER BY b.created_at DESC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		want = append(want, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(body.Blocks))
	for i, b := range body.Blocks {
		got[i] = b.ID
		if b.DisplayName != name {
			t.Fatalf("entry %d has the name %q", i, b.DisplayName)
		}
	}
	if !slices.Equal(got, want) {
		t.Error("the list is not in the order of the blocks, newest first")
	}
}

// A member who has blocked the caller is not in the caller's list, whoever
// blocked first: to the caller that member does not exist, and the list must
// not say otherwise. The caller's own block stays, and can still be removed.
func TestGetBlocksOmitsAMemberWhoBlockedTheCaller(t *testing.T) {
	for order, anaFirst := range map[string]bool{"the caller blocked first": false, "the other member blocked first": true} {
		t.Run(order, func(t *testing.T) {
			api := newTestAPI(t)
			ana, anaID := api.memberWithProfile(t, "ana@example.com", "MARKERNAME", "")
			ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
			_, choID := api.memberWithProfile(t, "cho@example.com", "Cho", "")
			requireBlockDone(t, api.putBlock(ben.AccessToken, choID))
			// What Ben's list is when Ana's profile does not exist for him.
			without := api.getBlocks(ben.AccessToken)
			requireBlocks(t, without, blocksJSON(t, choID, "Cho"))

			if anaFirst {
				requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
				requireBlockDone(t, api.putBlock(ben.AccessToken, anaID))
			} else {
				requireBlockDone(t, api.putBlock(ben.AccessToken, anaID))
				requireBlocks(t, api.getBlocks(ben.AccessToken), blocksJSON(t, anaID, "MARKERNAME", choID, "Cho"))
				requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
			}

			// Both blocks are stored, and neither list names the other.
			if !api.hasBlock(t, "ben@example.com", "ana@example.com") || !api.hasBlock(t, "ana@example.com", "ben@example.com") {
				t.Fatalf("blocks = %d, want one each way", api.blockCount(t))
			}
			hidden := api.getBlocks(ben.AccessToken)
			requireSameResponse(t, "ben's list while ana blocks him", hidden, without)
			requireBlocks(t, api.getBlocks(ana.AccessToken), noBlocks)
			// A rename of Ana's does not reach Ben through the list either.
			requireProfile(t, api.putProfile(ana.AccessToken, profileJSON(t, "MARKERRENAMED", "")))
			requireSameResponse(t, "ben's list after ana's rename", api.getBlocks(ben.AccessToken), without)

			// Ana unblocks: Ben's own block of her shows again, and he can
			// still remove it.
			requireBlockDone(t, api.deleteBlock(ana.AccessToken, benID))
			requireBlocks(t, api.getBlocks(ben.AccessToken), blocksJSON(t, anaID, "MARKERRENAMED", choID, "Cho"))
			requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
			requireBlockDone(t, api.deleteBlock(ben.AccessToken, anaID))
			if api.hasBlock(t, "ben@example.com", "ana@example.com") || !api.hasBlock(t, "ana@example.com", "ben@example.com") {
				t.Error("want ben's hidden block removed and ana's still stored")
			}
			requireSameResponse(t, "ben's list after removing the hidden block", api.getBlocks(ben.AccessToken), without)
		})
	}
}

// ---- A limit on how many members one may block ----

func TestBlockLimitOverHTTP(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	_, choID := api.memberWithProfile(t, "cho@example.com", "Cho", "")
	api.blockFillers(t, "ana@example.com", safety.MaxBlocks-1)
	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	if n := api.blockCount(t); n != safety.MaxBlocks {
		t.Fatalf("blocks = %d, want %d", n, safety.MaxBlocks)
	}

	// At the limit: one more is refused and stores nothing.
	requireLoginResponse(t, api.putBlock(ana.AccessToken, choID), http.StatusUnprocessableEntity, blocksTooMany)
	if api.hasBlock(t, "ana@example.com", "cho@example.com") || api.blockCount(t) != safety.MaxBlocks {
		t.Errorf("a refused block was stored: %d blocks", api.blockCount(t))
	}
	// Repeating a block that exists is not one more.
	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	// An id that names nobody is answered as ever.
	requireBlockDone(t, api.putBlock(ana.AccessToken, unknownMemberID))
	// The limit is the member's own: Ben is nowhere near his.
	ben := decodeTokens(t, api.login(loginBody("ben@example.com", loginPassword)))
	requireBlockDone(t, api.putBlock(ben.AccessToken, choID))
	requireBlockDone(t, api.deleteBlock(ben.AccessToken, choID))

	// Room again after an unblock.
	requireBlockDone(t, api.deleteBlock(ana.AccessToken, benID))
	requireBlockDone(t, api.putBlock(ana.AccessToken, choID))
	requireLoginResponse(t, api.putBlock(ana.AccessToken, benID), http.StatusUnprocessableEntity, blocksTooMany)
	requireBlocks(t, api.getBlocks(ana.AccessToken), blocksJSON(t, choID, "Cho"))
}

// One place left and two blocks at once: one takes it, the other is refused.
func TestConcurrentBlocksAtTheLimitOverHTTP(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	_, choID := api.memberWithProfile(t, "cho@example.com", "Cho", "")
	api.blockFillers(t, "ana@example.com", safety.MaxBlocks-1)

	var forBen, forCho *httptest.ResponseRecorder
	var wg sync.WaitGroup
	wg.Go(func() { forBen = api.putBlock(ana.AccessToken, benID) })
	wg.Go(func() { forCho = api.putBlock(ana.AccessToken, choID) })
	wg.Wait()

	done, refused := forBen, forCho
	if forBen.Code != http.StatusNoContent {
		done, refused = forCho, forBen
	}
	requireBlockDone(t, done)
	requireLoginResponse(t, refused, http.StatusUnprocessableEntity, blocksTooMany)
	if n := api.blockCount(t); n != safety.MaxBlocks {
		t.Errorf("blocks = %d, want %d", n, safety.MaxBlocks)
	}
	if api.hasBlock(t, "ana@example.com", "ben@example.com") == api.hasBlock(t, "ana@example.com", "cho@example.com") {
		t.Error("want exactly one of the two blocked")
	}
}

// ---- Blocking changes nothing of the target ----

// A block is the blocker's: the blocked member's profile, languages and
// picture are stored as before, and a third member reads them unchanged.
func TestABlockChangesNothingOfTheBlockedMember(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "Ben's bio")
	cho := api.loggedIn(t, "cho@example.com")
	languages := languagesJSON(entriesJSON("es", "native"), entriesJSON("ja", "a2"))
	requireLanguages(t, api.putLanguages(ben.AccessToken, languages), languages)
	picture := requireImage(t, api.putAvatar(ben.AccessToken, pngUpload(t, 64, 64, avatarBlue)))
	version := api.avatarRowVersion(t, "ben@example.com")
	storedLanguages := api.storedLanguages(t, "ben@example.com")
	before := api.getMember(cho.AccessToken, benID)
	own := requireProfile(t, api.getProfile(ben.AccessToken))

	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))

	requireSameResponse(t, "a third member's read after the block", api.getMember(cho.AccessToken, benID), before)
	if got := requireImage(t, api.getMemberAvatar(cho.AccessToken, benID)); string(got) != string(picture) {
		t.Error("a third member reads another picture")
	}
	if got := requireProfile(t, api.getProfile(ben.AccessToken)); got != own {
		t.Errorf("ben's own profile = %+v, want %+v", got, own)
	}
	if name, bio := api.storedProfile(t, "ben@example.com"); name != "Ben" || bio != "Ben's bio" {
		t.Errorf("ben's profile = %q, %q", name, bio)
	}
	if got := api.storedLanguages(t, "ben@example.com"); !slices.Equal(got, storedLanguages) {
		t.Errorf("ben's languages = %v, want %v", got, storedLanguages)
	}
	if got := api.avatarRowVersion(t, "ben@example.com"); got != version {
		t.Errorf("ben's picture row was rewritten: %s → %s", version, got)
	}
	// Ben's own routes answer him as before: nothing tells him.
	requireLanguages(t, api.getLanguages(ben.AccessToken), languages)
	requireImage(t, api.getAvatar(ben.AccessToken))
	requireBlocks(t, api.getBlocks(ben.AccessToken), noBlocks)
	requireMe(t, api.me(ben.AccessToken))
}

// ---- Block routes are for signed-in members only ----

// Without a usable access token every block route answers the same 401,
// whatever the identifier names, and stores, removes and returns nothing.
func TestBlockRoutesRejectMissingOrUnusableCredentials(t *testing.T) {
	api := newTestAPI(t)
	tokens, _ := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	_, choID := api.memberWithProfile(t, "cho@example.com", "Cho", "")
	requireBlockDone(t, api.putBlock(tokens.AccessToken, benID))
	revoked := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	requireLoggedOut(t, api.logout(revoked.AccessToken))
	expired := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	api.exec(t, `UPDATE sessions SET access_expires_at = now() - interval '1 second' WHERE access_token_hash = $1`,
		auth.HashToken(expired.AccessToken))

	for name, tc := range map[string]struct {
		query  string
		header []string
	}{
		"no header":           {},
		"basic scheme":        {header: []string{"Basic " + tokens.AccessToken}},
		"no scheme":           {header: []string{tokens.AccessToken}},
		"refresh token":       {header: []string{"Bearer " + tokens.RefreshToken}},
		"malformed":           {header: []string{"Bearer " + auth.AccessTokenPrefix + "garbage"}},
		"unknown":             {header: []string{"Bearer " + auth.NewToken(auth.AccessTokenPrefix).Raw}},
		"revoked":             {header: []string{"Bearer " + revoked.AccessToken}},
		"expired":             {header: []string{"Bearer " + expired.AccessToken}},
		"two headers":         {header: []string{"Bearer " + tokens.AccessToken, "Bearer " + tokens.AccessToken}},
		"token only in query": {query: "?access_token=" + tokens.AccessToken},
	} {
		t.Run(name, func(t *testing.T) {
			list := api.profileWith(http.MethodGet, blocksPath+tc.query, "", tc.header...)
			requireUnauthorized(t, list)
			// Cho is not blocked and Ben is: a block of one and an unblock
			// of the other would each change something.
			existing := api.profileWith(http.MethodPut, blocksPath+"/"+choID+tc.query, "", tc.header...)
			requireUnauthorized(t, existing)
			requireSameResponse(t, "the list", list, existing)
			for _, method := range []string{http.MethodPut, http.MethodDelete} {
				for _, id := range []string{benID, choID, unknownMemberID, "not-an-id", strings.ToUpper(benID), api.userID(t, "ben@example.com")} {
					rec := api.profileWith(method, blocksPath+"/"+id+tc.query, "", tc.header...)
					requireUnauthorized(t, rec)
					requireSameResponse(t, method+" "+id, rec, existing)
				}
			}
		})
	}

	if !api.hasBlock(t, "ana@example.com", "ben@example.com") || api.blockCount(t) != 1 {
		t.Errorf("an unauthenticated request changed the blocks: %d stored", api.blockCount(t))
	}
	requireBlocks(t, api.getBlocks(tokens.AccessToken), blocksJSON(t, benID, "Ben"))
}

// Every answer of a block route is marked no-store, whatever it says.
func TestBlockRoutesNeverAllowCaching(t *testing.T) {
	api := newTestAPIWith(t, Options{UserLimits: UserLimits{BlockWrite: tightUserLimiter()}})
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"a block":              api.putBlock(ana.AccessToken, benID),
		"over the limit":       api.deleteBlock(ana.AccessToken, benID),
		"oneself":              api.putBlock(ben.AccessToken, benID),
		"the list":             api.getBlocks(ana.AccessToken),
		"an empty list":        api.getBlocks(ben.AccessToken),
		"no token, a block":    api.profileWith(http.MethodPut, blocksPath+"/"+anaID, ""),
		"no token, an unblock": api.profileWith(http.MethodDelete, blocksPath+"/"+anaID, ""),
		"no token, the list":   api.profileWith(http.MethodGet, blocksPath, ""),
	} {
		if got := rec.Header().Values("Cache-Control"); !slices.Equal(got, []string{"no-store"}) {
			t.Errorf("%s (%d): Cache-Control = %q, want no-store", name, rec.Code, got)
		}
	}
}

// The mux answers a wrong method before authentication runs, with no
// service, and nothing changes.
func TestBlockRoutesAllowOnlyTheirMethods(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler), nil, nil, nil, nil, nil, Options{})
	for path, methods := range map[string][]string{
		blocksPath:                         {http.MethodPut, http.MethodPost, http.MethodPatch, http.MethodDelete},
		blocksPath + "/" + unknownMemberID: {http.MethodGet, http.MethodPost, http.MethodPatch},
	} {
		for _, method := range methods {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: status = %d, want 405", method, path, rec.Code)
			}
		}
	}

	api := newTestAPI(t)
	ana, _ := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	_, choID := api.memberWithProfile(t, "cho@example.com", "Cho", "")
	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		for _, id := range []string{benID, choID} {
			if rec := api.profileWith(method, blocksPath+"/"+id, "", "Bearer "+ana.AccessToken); rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s: status = %d, want 405", method, rec.Code)
			}
		}
	}
	for _, method := range []string{http.MethodPut, http.MethodPost, http.MethodDelete} {
		if rec := api.profileWith(method, blocksPath, `{"id":"`+choID+`"}`, "Bearer "+ana.AccessToken); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s on the list: status = %d, want 405", method, rec.Code)
		}
	}
	requireBlocks(t, api.getBlocks(ana.AccessToken), blocksJSON(t, benID, "Ben"))
	if n := api.blockCount(t); n != 1 {
		t.Errorf("blocks = %d, want 1", n)
	}
}

// Mounted without requireAccessToken by mistake, the handlers fail closed:
// nobody blocks, unblocks or reads a list without an identity.
func TestBlockHandlersWithoutMiddlewareFailClosed(t *testing.T) {
	api := newTestAPI(t)
	ana, _ := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	_, choID := api.memberWithProfile(t, "cho@example.com", "Cho", "")
	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	logger := slog.New(slog.DiscardHandler)
	profiles, safetySvc := profile.NewService(api.pool, logger), api.safetyService()

	for name, tc := range map[string]struct {
		method string
		h      http.Handler
	}{
		"put":    {http.MethodPut, handlePutBlock(logger, profiles, safetySvc)},
		"delete": {http.MethodDelete, handleDeleteBlock(logger, profiles, safetySvc)},
		"list":   {http.MethodGet, handleGetBlocks(logger, profiles, safetySvc)},
	} {
		for _, id := range []string{benID, choID, unknownMemberID, "not-an-id"} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, blocksPath, nil)
			req.SetPathValue("id", id)
			tc.h.ServeHTTP(rec, req)
			if rec.Code != http.StatusInternalServerError || strings.TrimSpace(rec.Body.String()) != internalError {
				t.Errorf("%s %s: %d %s, want the opaque 500", name, id, rec.Code, rec.Body)
			}
		}
	}
	if !api.hasBlock(t, "ana@example.com", "ben@example.com") || api.blockCount(t) != 1 {
		t.Errorf("a handler without an identity changed the blocks: %d stored", api.blockCount(t))
	}
}

// ---- Block writes are limited ----

func TestBlockWritesAreLimitedPerUser(t *testing.T) {
	api := newTestAPIWith(t, Options{UserLimits: UserLimits{BlockWrite: tightUserLimiter()}})
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")

	// Requests that don't authenticate never spend anyone's allowance.
	requireUnauthorized(t, api.profileWith(http.MethodPut, blocksPath+"/"+benID, ""))
	requireUnauthorized(t, api.profileWith(http.MethodDelete, blocksPath+"/"+benID, ""))
	requireUnauthorized(t, api.putBlock(auth.NewToken(auth.AccessTokenPrefix).Raw, benID))

	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	// One bucket for both: the unblock is refused, and did nothing.
	rec := api.deleteBlock(ana.AccessToken, benID)
	requireLoginResponse(t, rec, http.StatusTooManyRequests, rateLimited)
	if got := rec.Header().Get("Retry-After"); got == "" || got == "0" {
		t.Errorf("Retry-After = %q", got)
	}
	if !api.hasBlock(t, "ana@example.com", "ben@example.com") {
		t.Error("a limited unblock removed the block")
	}
	// Whatever is asked for next is refused the same way.
	for name, limited := range map[string]*httptest.ResponseRecorder{
		"a repeat":       api.putBlock(ana.AccessToken, benID),
		"an unknown id":  api.putBlock(ana.AccessToken, unknownMemberID),
		"a malformed id": api.deleteBlock(ana.AccessToken, "not-an-id"),
		"oneself":        api.putBlock(ana.AccessToken, anaID),
	} {
		requireLoginResponse(t, limited, http.StatusTooManyRequests, rateLimited)
		requireSameResponse(t, name, limited, rec)
	}

	// The allowance is the member's: a second session of Ana's shares it,
	// and Ben has his own.
	second := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	requireLoginResponse(t, api.putBlock(second.AccessToken, benID), http.StatusTooManyRequests, rateLimited)
	requireBlockDone(t, api.putBlock(ben.AccessToken, unknownMemberID))
	requireLoginResponse(t, api.putBlock(ben.AccessToken, anaID), http.StatusTooManyRequests, rateLimited)
	if n := api.blockCount(t); n != 1 {
		t.Errorf("blocks = %d, want 1", n)
	}

	// Reading the list is not limited, and nothing else of Ana's is touched.
	for range 3 {
		requireBlocks(t, api.getBlocks(ana.AccessToken), blocksJSON(t, benID, "Ben"))
	}
	requireProfile(t, api.putProfile(ana.AccessToken, profileJSON(t, "Ana L.", "")))
	requireLanguages(t, api.putLanguages(ana.AccessToken, anasLanguages), anasLanguages)
	requireMe(t, api.me(ana.AccessToken))
}

// Every write costs a token, whatever comes of it: one for an id that names
// nobody, a repeat, a refusal.
func TestBlockWriteLimitCountsEveryWrite(t *testing.T) {
	for name, first := range map[string]func(api testAPI, token, own, other string) *httptest.ResponseRecorder{
		"an unknown id": func(api testAPI, token, _, _ string) *httptest.ResponseRecorder {
			return api.putBlock(token, unknownMemberID)
		},
		"a malformed id": func(api testAPI, token, _, _ string) *httptest.ResponseRecorder {
			return api.putBlock(token, "not-an-id")
		},
		"an unblock of nobody": func(api testAPI, token, _, other string) *httptest.ResponseRecorder {
			return api.deleteBlock(token, other)
		},
		"oneself":               func(api testAPI, token, own, _ string) *httptest.ResponseRecorder { return api.putBlock(token, own) },
		"an unblock of oneself": func(api testAPI, token, own, _ string) *httptest.ResponseRecorder { return api.deleteBlock(token, own) },
	} {
		t.Run(name, func(t *testing.T) {
			api := newTestAPIWith(t, Options{UserLimits: UserLimits{BlockWrite: tightUserLimiter()}})
			ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
			_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")

			if rec := first(api, ana.AccessToken, anaID, benID); rec.Code == http.StatusTooManyRequests {
				t.Fatalf("the first write was limited")
			}
			requireLoginResponse(t, api.putBlock(ana.AccessToken, benID), http.StatusTooManyRequests, rateLimited)
			if n := api.blockCount(t); n != 0 {
				t.Errorf("blocks = %d, want 0", n)
			}
		})
	}

	// A repeat of a stored block costs one as well.
	api := newTestAPIWith(t, Options{UserLimits: UserLimits{BlockWrite: userLimiter(2)}})
	ana := api.loggedIn(t, "ana@example.com")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	requireLoginResponse(t, api.deleteBlock(ana.AccessToken, benID), http.StatusTooManyRequests, rateLimited)
}

// The block limit is a bucket of its own: spending it leaves the profile,
// languages, picture and member-read allowances alone, and spending those
// leaves it.
func TestBlockWriteLimitIsSeparateFromTheOthers(t *testing.T) {
	api := newTestAPIWith(t, Options{UserLimits: UserLimits{
		ProfileWrite: userLimiter(2), LanguagesWrite: tightUserLimiter(), AvatarWrite: tightUserLimiter(),
		MemberRead: tightUserLimiter(), BlockWrite: tightUserLimiter()}})
	// Each member's profile save here is one of their two profile writes.
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	cho, _ := api.memberWithProfile(t, "cho@example.com", "Cho", "")

	// Ana spends the block allowance; everything else of hers still works.
	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	requireLoginResponse(t, api.deleteBlock(ana.AccessToken, benID), http.StatusTooManyRequests, rateLimited)
	requireProfile(t, api.putProfile(ana.AccessToken, profileJSON(t, "Ana L.", "")))
	requireLanguages(t, api.putLanguages(ana.AccessToken, anasLanguages), anasLanguages)
	requireImage(t, api.putAvatar(ana.AccessToken, pngUpload(t, 64, 64, avatarRed)))
	requireMember(t, api.getMember(ana.AccessToken, anaID), `{"id":"`+anaID+`","display_name":"Ana L.","bio":"","has_avatar":true,"languages":`+anasLanguages+`}`)

	// Cho spends every other allowance; the block one is untouched.
	requireProfile(t, api.putProfile(cho.AccessToken, profileJSON(t, "Cho 2", "")))
	requireLoginResponse(t, api.putProfile(cho.AccessToken, profileJSON(t, "Cho 3", "")), http.StatusTooManyRequests, rateLimited)
	requireLanguages(t, api.putLanguages(cho.AccessToken, anasLanguages), anasLanguages)
	requireLoginResponse(t, api.putLanguages(cho.AccessToken, anasLanguages), http.StatusTooManyRequests, rateLimited)
	requireImage(t, api.putAvatar(cho.AccessToken, pngUpload(t, 64, 64, avatarRed)))
	requireLoginResponse(t, api.deleteAvatar(cho.AccessToken), http.StatusTooManyRequests, rateLimited)
	requireMember(t, api.getMember(cho.AccessToken, benID), memberJSON(t, benID, "Ben", "", noLanguages))
	requireLoginResponse(t, api.getMember(cho.AccessToken, benID), http.StatusTooManyRequests, rateLimited)
	requireBlockDone(t, api.putBlock(cho.AccessToken, benID))
}

func TestNewUserLimitsForBlocksMatchTheDecisionLog(t *testing.T) {
	l := NewUserLimits(slog.New(slog.DiscardHandler))
	const burst = 10
	for i := range burst {
		if ok, _ := l.BlockWrite.Allow("member"); !ok {
			t.Fatalf("denied at %d, burst is %d", i+1, burst)
		}
	}
	ok, retryAfter := l.BlockWrite.Allow("member")
	if ok {
		t.Errorf("allowed past burst %d", burst)
	}
	if retryAfter <= 0 || retryAfter > 6*time.Second {
		t.Errorf("retry after %v, want within the 6 s refill", retryAfter)
	}
	// Another member has their own, and the other buckets are untouched.
	if ok, _ := l.BlockWrite.Allow("other"); !ok {
		t.Error("another member was refused")
	}
	if ok, _ := l.ProfileWrite.Allow("member"); !ok {
		t.Error("the member's profile write was refused")
	}
	if ok, _ := l.MemberRead.Allow("member"); !ok {
		t.Error("the member's read was refused")
	}
}

// ---- Blocks and account deletion ----

// A caller deleted after authentication but before the write: their sessions
// are gone with them, so the answer is the 401 of a dead credential (016),
// and nothing is stored.
func TestBlockRoutesForAUserDeletedMeanwhile(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	id := api.userID(t, "ana@example.com")
	api.exec(t, `DELETE FROM users WHERE id = $1`, id)
	logger := slog.New(slog.DiscardHandler)
	profiles, safetySvc := profile.NewService(api.pool, logger), api.safetyService()

	rec := httptest.NewRecorder()
	handlePutBlock(logger, profiles, safetySvc).ServeHTTP(rec, blockHandlerRequest(http.MethodPut, benID, id))
	requireResponse(t, rec, http.StatusUnauthorized, invalidAccessToken)
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", got)
	}
	if n := api.blockCount(t); n != 0 {
		t.Errorf("blocks = %d, want 0", n)
	}

	// Their blocks went with them: an unblock has nothing to do, the list
	// is empty, and their next request is the 401 of a dead session.
	rec = httptest.NewRecorder()
	handleDeleteBlock(logger, profiles, safetySvc).ServeHTTP(rec, blockHandlerRequest(http.MethodDelete, benID, id))
	if rec.Code != http.StatusNoContent {
		t.Errorf("DELETE: status = %d, want 204", rec.Code)
	}
	rec = httptest.NewRecorder()
	handleGetBlocks(logger, profiles, safetySvc).ServeHTTP(rec, as(httptest.NewRequest(http.MethodGet, blocksPath, nil), id))
	requireResponse(t, rec, http.StatusOK, noBlocks)
	for _, rec := range []*httptest.ResponseRecorder{
		api.putBlock(tokens.AccessToken, benID), api.deleteBlock(tokens.AccessToken, benID), api.getBlocks(tokens.AccessToken),
	} {
		requireUnauthorized(t, rec)
	}
}

// Deleting an account deletes its blocks, made and received: a deleted
// blocked member leaves the list and no longer counts toward the limit.
func TestDeletingAnAccountRemovesItsBlocksOverHTTP(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	cho, choID := api.memberWithProfile(t, "cho@example.com", "Cho", "")
	_, deeID := api.memberWithProfile(t, "dee@example.com", "Dee", "")
	requireBlockDone(t, api.putBlock(ben.AccessToken, anaID))
	requireBlockDone(t, api.putBlock(ben.AccessToken, choID))
	requireBlockDone(t, api.putBlock(cho.AccessToken, benID))
	api.blockFillers(t, "ana@example.com", safety.MaxBlocks-1)
	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	requireLoginResponse(t, api.putBlock(ana.AccessToken, deeID), http.StatusUnprocessableEntity, blocksTooMany)

	api.exec(t, `DELETE FROM users WHERE email = 'ben@example.com'`)

	// No block involving Ben is left, made by him or of him.
	if n := api.blockCount(t); n != safety.MaxBlocks-1 {
		t.Errorf("blocks = %d, want only ana's %d fillers", n, safety.MaxBlocks-1)
	}
	requireBlocks(t, api.getBlocks(ana.AccessToken), noBlocks)
	requireBlocks(t, api.getBlocks(cho.AccessToken), noBlocks)
	// The place he took is free again.
	requireBlockDone(t, api.putBlock(ana.AccessToken, deeID))
	requireBlocks(t, api.getBlocks(ana.AccessToken), blocksJSON(t, deeID, "Dee"))
	// Blocking or unblocking the identifier he had is the answer for nobody.
	requireBlockDone(t, api.putBlock(cho.AccessToken, benID))
	requireBlockDone(t, api.deleteBlock(cho.AccessToken, benID))
	requireBlocks(t, api.getBlocks(cho.AccessToken), noBlocks)
}

// ---- 503 ----

// A request whose time is already up does no work at all and answers 503;
// the retry it invites then succeeds.
func TestBlockRoutesWithAnExpiredDeadlineAreUnavailable(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	id := api.userID(t, "ana@example.com")
	logger := slog.New(slog.DiscardHandler)
	profiles, safetySvc := profile.NewService(api.pool, logger), api.safetyService()

	for name, tc := range map[string]struct {
		method string
		h      http.Handler
	}{
		"put":    {http.MethodPut, handlePutBlock(logger, profiles, safetySvc)},
		"delete": {http.MethodDelete, handleDeleteBlock(logger, profiles, safetySvc)},
		"list":   {http.MethodGet, handleGetBlocks(logger, profiles, safetySvc)},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel()
			req := httptest.NewRequest(tc.method, blocksPath+"/x", nil).WithContext(ctx)
			req.SetPathValue("id", benID)
			rec := httptest.NewRecorder()
			rec.Header().Set("Cache-Control", "no-store") // as requireAccessToken leaves it
			tc.h.ServeHTTP(rec, as(req, id))
			requireLoginResponse(t, rec, http.StatusServiceUnavailable, serviceUnavailable)
			if got := rec.Header().Get("Retry-After"); got == "" || got == "0" {
				t.Errorf("Retry-After = %q", got)
			}
		})
	}
	if n := api.blockCount(t); n != 0 {
		t.Errorf("blocks = %d, want 0", n)
	}
	requireBlockDone(t, api.putBlock(tokens.AccessToken, benID))
}

// ---- Blocks are never disclosed or logged ----

var blockLogLine = regexp.MustCompile(`^time=\S+ level=INFO msg="block: (added|removed)" user_id=([0-9a-f-]{36})$`)

// The logs record that a member's blocks changed, with that member's user id
// and nothing else: not whom, by account id, public id or name, and nothing
// for a request that changed no row or for a read hidden by a block.
func TestBlockLogsHoldTheBlockersUserIDOnly(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "MARKERNAME", "MARKERBIO")
	api.svc.Wait()
	before := api.logs.String()

	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))           // a repeat: not logged
	requireBlockDone(t, api.putBlock(ana.AccessToken, unknownMemberID)) // nobody: not logged
	requireBlockDone(t, api.putBlock(ana.AccessToken, "MARKERID"))
	requireBlockDone(t, api.deleteBlock(ana.AccessToken, strings.ToUpper(benID)))
	requireLoginResponse(t, api.putBlock(ana.AccessToken, anaID), http.StatusUnprocessableEntity, memberSelf)
	requireBlocks(t, api.getBlocks(ana.AccessToken), blocksJSON(t, benID, "MARKERNAME"))
	requireBlocks(t, api.getBlocks(ben.AccessToken), noBlocks)
	// What the block hides is not logged either, for either of them.
	requireMemberNotFound(t, api.getMember(ben.AccessToken, anaID))
	requireMemberNotFound(t, api.getMember(ana.AccessToken, benID))
	requireMemberNotFound(t, api.getMemberAvatar(ben.AccessToken, anaID))
	requireBlockDone(t, api.deleteBlock(ana.AccessToken, benID))
	requireBlockDone(t, api.deleteBlock(ana.AccessToken, benID)) // nothing to remove: not logged

	api.svc.Wait()
	added := strings.TrimPrefix(api.logs.String(), before)
	lines := strings.Split(strings.TrimSpace(added), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d lines logged, want a block and an unblock:\n%s", len(lines), added)
	}
	account := api.userID(t, "ana@example.com")
	for i, what := range []string{"added", "removed"} {
		m := blockLogLine.FindStringSubmatch(lines[i])
		if m == nil || m[1] != what || m[2] != account {
			t.Errorf("line %d = %q, want only that ana's block was %s", i+1, lines[i], what)
		}
	}
	for _, private := range []string{
		api.userID(t, "ben@example.com"), benID, anaID, unknownMemberID, "MARKER", ana.AccessToken, ben.AccessToken, "example.com",
	} {
		if strings.Contains(strings.ToLower(added), strings.ToLower(private)) {
			t.Errorf("logs contain %q: %s", private, added)
		}
	}
}

// A failure inside is an opaque 500, logged with the route's pattern and
// never the identifier asked for or anybody's account.
func TestBlockInternalErrorIsOpaque(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	requireBlockDone(t, api.putBlock(ana.AccessToken, benID))
	requireBlockDone(t, api.deleteBlock(ana.AccessToken, benID))
	api.svc.Wait()
	before := api.logs.String()
	// Break every query on the table by hiding a column each of them names.
	api.exec(t, `ALTER TABLE blocks RENAME COLUMN blocker_id TO test_hidden`)
	t.Cleanup(func() {
		_, _ = api.pool.Exec(context.Background(), `ALTER TABLE blocks RENAME COLUMN test_hidden TO blocker_id`)
	})

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"put":            api.putBlock(ana.AccessToken, benID),
		"delete":         api.deleteBlock(ana.AccessToken, benID),
		"list":           api.getBlocks(ana.AccessToken),
		"member profile": api.getMember(ana.AccessToken, benID),
		"member picture": api.getMemberAvatar(ana.AccessToken, benID),
	} {
		t.Run(name, func(t *testing.T) {
			requireLoginResponse(t, rec, http.StatusInternalServerError, internalError)
			if len(rec.Header()["Retry-After"]) != 0 || len(rec.Header()["Www-Authenticate"]) != 0 {
				t.Errorf("headers = %v", rec.Header())
			}
		})
	}
	// What needs no block query still never reaches the broken table: an id
	// that names nobody, oneself, and one's own profile.
	requireBlockDone(t, api.putBlock(ana.AccessToken, unknownMemberID))
	requireBlockDone(t, api.deleteBlock(ana.AccessToken, "not-an-id"))
	requireLoginResponse(t, api.putBlock(ana.AccessToken, anaID), http.StatusUnprocessableEntity, memberSelf)
	requireMember(t, api.getMember(ana.AccessToken, anaID), memberJSON(t, anaID, "Ana", "", noLanguages))
	requireMemberNotFound(t, api.getMember(ana.AccessToken, unknownMemberID))

	api.svc.Wait()
	logs := strings.TrimPrefix(api.logs.String(), before)
	for _, route := range []string{
		"PUT " + blocksPath + "/{id}", "DELETE " + blocksPath + "/{id}", "GET " + blocksPath,
		"GET /v1/profiles/{id}", "GET /v1/profiles/{id}/avatar",
	} {
		if !strings.Contains(logs, `route="`+route+`"`) {
			t.Errorf("the failure of %s was not logged with its route: %s", route, logs)
		}
	}
	if n := strings.Count(logs, "request failed"); n != 5 {
		t.Errorf("%d failures logged, want 5", n)
	}
	for _, private := range []string{
		benID, anaID, api.userID(t, "ana@example.com"), api.userID(t, "ben@example.com"), ana.AccessToken, "example.com",
	} {
		if strings.Contains(logs, private) {
			t.Errorf("logs contain %q: %s", private, logs)
		}
	}
}
