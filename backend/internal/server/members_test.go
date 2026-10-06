package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"vocatogether/backend/internal/auth"
	"vocatogether/backend/internal/avatar"
	"vocatogether/backend/internal/language"
	"vocatogether/backend/internal/profile"
	"vocatogether/backend/internal/ratelimit"
)

const (
	membersPath = "/v1/profiles/"
	// unknownMemberID is well formed and names no profile.
	unknownMemberID = "00000000-0000-4000-8000-000000000000"
)

func (a testAPI) getMember(accessToken, id string) *httptest.ResponseRecorder {
	return a.profileWith(http.MethodGet, membersPath+id, "", "Bearer "+accessToken)
}

// memberWithProfile creates a verified, logged-in account for addr, saves a
// profile for it and returns its tokens and the profile's public id.
func (a testAPI) memberWithProfile(t *testing.T, addr, displayName, bio string) (loginTokens, string) {
	t.Helper()
	tokens := a.loggedIn(t, addr)
	return tokens, requireProfile(t, a.putProfile(tokens.AccessToken, profileJSON(t, displayName, bio))).ID
}

// memberJSON is a whole member profile response, from its parts.
func memberJSON(t *testing.T, id, displayName, bio, languages string) string {
	t.Helper()
	name, err := json.Marshal(displayName)
	if err != nil {
		t.Fatal(err)
	}
	text, err := json.Marshal(bio)
	if err != nil {
		t.Fatal(err)
	}
	return `{"id":"` + id + `","display_name":` + string(name) + `,"bio":` + string(text) +
		`,"has_avatar":false,"languages":` + languages + `}`
}

// requireMember checks a successful member profile response: exactly want,
// never cached, and with exactly the public keys, whatever want says.
func requireMember(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	requireLoginResponse(t, rec, http.StatusOK, want)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	if keys := slices.Sorted(maps.Keys(fields)); !slices.Equal(keys, []string{"bio", "display_name", "has_avatar", "id", "languages"}) {
		t.Errorf("fields = %v, want exactly the five public ones", keys)
	}
	var lists map[string]json.RawMessage
	if err := json.Unmarshal(fields["languages"], &lists); err != nil {
		t.Fatalf("decode languages %s: %v", fields["languages"], err)
	}
	if keys := slices.Sorted(maps.Keys(lists)); !slices.Equal(keys, []string{"learning", "spoken"}) {
		t.Errorf("languages fields = %v", keys)
	}
}

// requireMemberNotFound checks the one answer for everything that is not the
// public id of a profile.
func requireMemberNotFound(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	requireLoginResponse(t, rec, http.StatusNotFound, profileNotFound)
}

// ---- Reading a member's public profile ----

func TestGetMemberProfile(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "Ana's bio")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben López", "Learning Japanese.\nSay hi!")
	benLanguages := languagesJSON(entriesJSON("es", "native", "en", "c1"), entriesJSON("ja", "a2", "yue", "b1"))
	requireLanguages(t, api.putLanguages(ben.AccessToken, benLanguages), benLanguages)

	// Another member's profile: the name, the text and the languages with
	// their levels, in the owner's order.
	want := memberJSON(t, benID, "Ben López", "Learning Japanese.\nSay hi!", benLanguages)
	requireMember(t, api.getMember(ana.AccessToken, benID), want)

	// One's own profile by its public id: what any other member gets.
	requireMember(t, api.getMember(ben.AccessToken, benID), want)

	// Each id names its own profile.
	requireMember(t, api.getMember(ben.AccessToken, anaID), memberJSON(t, anaID, "Ana", "Ana's bio", noLanguages))

	// As the owner last saved it: an edit and a new order show at once.
	requireProfile(t, api.putProfile(ben.AccessToken, profileJSON(t, "Ben", "")))
	reordered := languagesJSON(entriesJSON("en", "c2"), entriesJSON("yue", "b1", "ja", "a2"))
	requireLanguages(t, api.putLanguages(ben.AccessToken, reordered), reordered)
	requireMember(t, api.getMember(ana.AccessToken, benID), memberJSON(t, benID, "Ben", "", reordered))

	// Reading changed nothing.
	if name, bio := api.storedProfile(t, "ben@example.com"); name != "Ben" || bio != "" {
		t.Errorf("ben's profile = %q, %q", name, bio)
	}
	if n := api.profileCount(t); n != 2 {
		t.Errorf("profiles = %d, want 2", n)
	}
}

// A member who has chosen no language has two empty lists: arrays, not nulls
// and not a missing key.
func TestGetMemberProfileWithoutLanguages(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")

	rec := api.getMember(ana.AccessToken, benID)
	requireMember(t, rec, memberJSON(t, benID, "Ben", "", noLanguages))
	if !strings.Contains(rec.Body.String(), `"languages":{"spoken":[],"learning":[]}`) {
		t.Errorf("body = %s, want two empty arrays", rec.Body)
	}
}

// A reader needs no profile of their own, and a query string names nobody.
func TestGetMemberProfileIsSelectedByThePathOnly(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com") // no profile
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	_, choID := api.memberWithProfile(t, "cho@example.com", "Cho", "")

	want := memberJSON(t, benID, "Ben", "", noLanguages)
	requireMember(t, api.getMember(ana.AccessToken, benID), want)
	requireMember(t, api.getMember(ana.AccessToken, benID+"?id="+choID+"&user_id="+api.userID(t, "cho@example.com")), want)
}

// ---- Unknown and unavailable profiles are indistinguishable ----

func TestGetMemberProfileNotFoundIsTheSameForEveryMiss(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	api.loggedIn(t, "cho@example.com") // an account that has saved no profile

	unknown := api.getMember(ana.AccessToken, unknownMemberID)
	requireMemberNotFound(t, unknown)

	for name, id := range map[string]string{
		"not an id":                        "not-an-id",
		"upper case":                       strings.ToUpper(benID),
		"own id in upper case":             strings.ToUpper(anaID),
		"braces":                           "%7B" + benID + "%7D",
		"no hyphens":                       strings.ReplaceAll(benID, "-", ""),
		"one character short":              benID[:35],
		"one character long":               benID + "0",
		"trailing space":                   benID + "%20",
		"leading space":                    "%20" + benID,
		"trailing line break":              benID + "%0A",
		"a number":                         "1",
		"the owner's account id":           api.userID(t, "ben@example.com"),
		"the reader's account id":          api.userID(t, "ana@example.com"),
		"an account id without a profile":  api.userID(t, "cho@example.com"),
		"an email address":                 "ben@example.com",
		"an email address, percent-coded":  "ben%40example.com",
		"me":                               "me",
		"a well-formed id nobody has":      "ffffffff-ffff-4fff-bfff-ffffffffffff",
		"a well-formed id, one digit away": benID[:35] + map[bool]string{true: "1", false: "0"}[benID[35] == '0'],
	} {
		t.Run(name, func(t *testing.T) {
			rec := api.getMember(ana.AccessToken, id)
			requireMemberNotFound(t, rec)
			if rec.Body.String() != unknown.Body.String() || fmt.Sprint(rec.Header()) != fmt.Sprint(unknown.Header()) {
				t.Errorf("response differs from the unknown id's:\n%v %s\nvs\n%v %s",
					rec.Header(), rec.Body, unknown.Header(), unknown.Body)
			}
		})
	}
}

// A profile whose account no longer exists went with it.
func TestGetMemberProfileAfterTheOwnerWasDeleted(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com")
	_, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")
	requireMember(t, api.getMember(ana.AccessToken, benID), memberJSON(t, benID, "Ben", "", noLanguages))

	api.exec(t, `DELETE FROM users WHERE email = 'ben@example.com'`)
	requireMemberNotFound(t, api.getMember(ana.AccessToken, benID))
}

// memberHandlerRequest is a request for id straight to the handler, as the
// mux and requireAccessToken would leave it.
func memberHandlerRequest(id, readerID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, membersPath+"x", nil)
	req.SetPathValue("id", id)
	return as(req, readerID)
}

// A malformed id is answered without asking the database: the services here
// have no pool, so any query would panic.
func TestGetMemberProfileDoesNotQueryForAMalformedID(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	h := handleGetMemberProfile(logger, profile.NewService(nil, logger), language.NewService(nil, logger),
		avatar.NewService(nil, logger))

	const lettered = "abcdefab-cdef-4bcd-8fab-cdefabcdefab"
	for _, id := range []string{"", "not-an-id", strings.ToUpper(lettered), lettered + " ", "{" + lettered + "}", lettered[:35]} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, memberHandlerRequest(id, unknownMemberID))
		requireResponse(t, rec, http.StatusNotFound, profileNotFound)
	}
}

// ---- Only public fields are disclosed ----

// Nothing private about the owner or the reader is in any answer, found or
// not: no email address, no account id.
func TestGetMemberProfileDisclosesNothingPrivate(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "markerreader@example.com", "Ana", "")
	ben, benID := api.memberWithProfile(t, "markerowner@example.com", "Ben", "Hi")
	languages := languagesJSON(entriesJSON("es", "native"), entriesJSON("ja", "a2"))
	requireLanguages(t, api.putLanguages(ben.AccessToken, languages), languages)
	readerAccount, ownerAccount := api.userID(t, "markerreader@example.com"), api.userID(t, "markerowner@example.com")

	found := api.getMember(ana.AccessToken, benID)
	requireMember(t, found, memberJSON(t, benID, "Ben", "Hi", languages))
	own := api.getMember(ana.AccessToken, anaID)
	requireMember(t, own, memberJSON(t, anaID, "Ana", "", noLanguages))

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"another member's profile": found,
		"one's own profile":        own,
		"unknown id":               api.getMember(ana.AccessToken, unknownMemberID),
		"the owner's account id":   api.getMember(ana.AccessToken, ownerAccount),
		"the reader's account id":  api.getMember(ana.AccessToken, readerAccount),
		"no token":                 api.profileWith(http.MethodGet, membersPath+benID, ""),
	} {
		answer := rec.Body.String() + fmt.Sprint(rec.Header())
		for what, private := range map[string]string{
			"an email address":        "marker",
			"the email domain":        "example.com",
			"the owner's account id":  ownerAccount,
			"the reader's account id": readerAccount,
			"a timestamp":             "_at",
			"a token":                 "vt_",
			"the word email":          "email",
			"the word verified":       "verified",
			"the word google":         "google",
			"the word session":        "session",
		} {
			if strings.Contains(strings.ToLower(answer), private) {
				t.Errorf("%s: the response holds %s: %s", name, what, answer)
			}
		}
	}
}

// ---- Member profiles are for signed-in members only ----

// Without a usable access token the answer is the same 401 whether the id
// names a profile, names none or is not an id: it reveals nothing.
func TestGetMemberProfileRejectsMissingOrUnusableCredentials(t *testing.T) {
	api := newTestAPI(t)
	tokens, id := api.memberWithProfile(t, "ana@example.com", "Ana", "")
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
			existing := api.profileWith(http.MethodGet, membersPath+id+tc.query, "", tc.header...)
			requireUnauthorized(t, existing)
			for _, other := range []string{unknownMemberID, "not-an-id", strings.ToUpper(id), api.userID(t, "ana@example.com")} {
				rec := api.profileWith(http.MethodGet, membersPath+other+tc.query, "", tc.header...)
				requireUnauthorized(t, rec)
				if rec.Body.String() != existing.Body.String() || fmt.Sprint(rec.Header()) != fmt.Sprint(existing.Header()) {
					t.Errorf("%s answers differently from an existing id:\n%v %s\nvs\n%v %s",
						other, rec.Header(), rec.Body, existing.Header(), existing.Body)
				}
			}
		})
	}
}

// ---- A member route cannot change anything ----

func TestMemberProfileRouteIsReadOnly(t *testing.T) {
	api := newTestAPI(t)
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "Ana's bio")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "Ben's bio")
	languages := languagesJSON(entriesJSON("es", "native"), entriesJSON("ja", "a2"))
	requireLanguages(t, api.putLanguages(ben.AccessToken, languages), languages)
	requireLanguages(t, api.putLanguages(ana.AccessToken, languages), languages)
	stored := api.storedLanguages(t, "ben@example.com")

	for _, method := range []string{http.MethodPut, http.MethodPost, http.MethodPatch, http.MethodDelete} {
		for owner, id := range map[string]string{"another member's id": benID, "one's own id": anaID, "an unknown id": unknownMemberID} {
			for _, body := range []string{"", profileJSON(t, "Mallory", "Changed"), `{"spoken":[],"learning":[]}`} {
				rec := api.profileWith(method, membersPath+id, body, "Bearer "+ana.AccessToken)
				if rec.Code != http.StatusMethodNotAllowed {
					t.Errorf("%s with %s: status = %d, want 405", method, owner, rec.Code)
				}
			}
		}
	}
	// Without a token the method is refused the same way: nothing to learn.
	if rec := api.profileWith(http.MethodDelete, membersPath+benID, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE without a token: status = %d, want 405", rec.Code)
	}

	// A public id in an owner route names nobody: Ana's save, with Ben's id
	// in the query, is Ana's.
	saved := requireProfile(t, api.profileWith(http.MethodPut, profilePath+"?id="+benID+"&public_id="+benID,
		profileJSON(t, "Ana 2", "Ana's bio"), "Bearer "+ana.AccessToken))
	if saved.ID != anaID || saved.DisplayName != "Ana 2" {
		t.Errorf("a save with ben's id in the query returned %+v", saved)
	}

	for addr, want := range map[string][2]string{"ana@example.com": {"Ana 2", "Ana's bio"}, "ben@example.com": {"Ben", "Ben's bio"}} {
		if name, bio := api.storedProfile(t, addr); name != want[0] || bio != want[1] {
			t.Errorf("%s's profile = %q, %q", addr, name, bio)
		}
	}
	if got := api.storedLanguages(t, "ben@example.com"); !slices.Equal(got, stored) {
		t.Errorf("ben's languages = %v, want %v", got, stored)
	}
	if got := api.storedLanguages(t, "ana@example.com"); !slices.Equal(got, stored) {
		t.Errorf("ana's languages = %v, want %v", got, stored)
	}
	if n := api.profileCount(t); n != 2 {
		t.Errorf("profiles = %d, want 2", n)
	}
	if got := requireProfile(t, api.getProfile(ben.AccessToken)).ID; got != benID {
		t.Errorf("ben's id = %s, want %s", got, benID)
	}
}

// The mux answers a wrong method before authentication runs, with no service.
func TestMemberProfileRouteAllowsOnlyGet(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler), nil, nil, nil, nil, Options{})
	for _, method := range []string{http.MethodPut, http.MethodPost, http.MethodPatch, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, membersPath+unknownMemberID, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
	}
}

// Mounted without requireAccessToken by mistake, the handler fails closed:
// nobody reads a profile without an identity.
func TestMemberProfileHandlerWithoutMiddlewareFailsClosed(t *testing.T) {
	api := newTestAPI(t)
	_, id := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	logger := slog.New(slog.DiscardHandler)
	h := handleGetMemberProfile(logger, profile.NewService(api.pool, logger), api.languageService(), api.avatarService())

	for _, target := range []string{id, unknownMemberID, "not-an-id"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, membersPath+target, nil)
		req.SetPathValue("id", target)
		h.ServeHTTP(rec, req)
		requireResponse(t, rec, http.StatusInternalServerError, internalError)
	}
}

// ---- Member reads are limited per reader ----

func TestGetMemberProfileIsLimitedPerReader(t *testing.T) {
	api := newTestAPIWith(t, Options{UserLimits: UserLimits{MemberRead: tightUserLimiter()}})
	ana, anaID := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "Ben", "")

	// Requests that don't authenticate never spend anyone's allowance.
	requireUnauthorized(t, api.profileWith(http.MethodGet, membersPath+benID, ""))
	requireUnauthorized(t, api.getMember(auth.NewToken(auth.AccessTokenPrefix).Raw, benID))

	requireMember(t, api.getMember(ana.AccessToken, benID), memberJSON(t, benID, "Ben", "", noLanguages))
	rec := api.getMember(ana.AccessToken, benID)
	requireLoginResponse(t, rec, http.StatusTooManyRequests, rateLimited)
	if got := rec.Header().Get("Retry-After"); got == "" || got == "0" {
		t.Errorf("Retry-After = %q", got)
	}
	// Whatever is asked for next, found or not, is refused the same way.
	for _, id := range []string{anaID, unknownMemberID, "not-an-id"} {
		limited := api.getMember(ana.AccessToken, id)
		requireLoginResponse(t, limited, http.StatusTooManyRequests, rateLimited)
		if limited.Body.String() != rec.Body.String() || fmt.Sprint(limited.Header()) != fmt.Sprint(rec.Header()) {
			t.Errorf("%s: limited response differs", id)
		}
	}

	// The allowance is the reader's: a second session of Ana's shares it,
	// and Ben has his own, also for reading Ana.
	second := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	requireLoginResponse(t, api.getMember(second.AccessToken, benID), http.StatusTooManyRequests, rateLimited)
	requireMember(t, api.getMember(ben.AccessToken, anaID), memberJSON(t, anaID, "Ana", "", noLanguages))

	// Ana's own profile and languages are untouched by it.
	requireProfile(t, api.getProfile(ana.AccessToken))
	requireProfile(t, api.putProfile(ana.AccessToken, profileJSON(t, "Ana L.", "")))
	requireLanguages(t, api.getLanguages(ana.AccessToken), noLanguages)
	requireLanguages(t, api.putLanguages(ana.AccessToken, anasLanguages), anasLanguages)
	requireMe(t, api.me(ana.AccessToken))
}

// A read that finds nothing costs a token too: walking ids is what the limit
// bounds.
func TestMemberReadLimitCountsMisses(t *testing.T) {
	for _, id := range []string{unknownMemberID, "not-an-id"} {
		t.Run(id, func(t *testing.T) {
			api := newTestAPIWith(t, Options{UserLimits: UserLimits{MemberRead: tightUserLimiter()}})
			ana := api.loggedIn(t, "ana@example.com")
			requireMemberNotFound(t, api.getMember(ana.AccessToken, id))
			requireLoginResponse(t, api.getMember(ana.AccessToken, id), http.StatusTooManyRequests, rateLimited)
		})
	}
}

// The member-read limit and the write limits are separate buckets.
func TestMemberReadAndWriteLimitsAreSeparate(t *testing.T) {
	api := newTestAPIWith(t, Options{UserLimits: UserLimits{
		ProfileWrite: tightUserLimiter(), LanguagesWrite: tightUserLimiter(), MemberRead: tightUserLimiter()}})
	ana, id := api.memberWithProfile(t, "ana@example.com", "Ana", "")
	requireLoginResponse(t, api.putProfile(ana.AccessToken, profileJSON(t, "Ana", "")), http.StatusTooManyRequests, rateLimited)

	requireMember(t, api.getMember(ana.AccessToken, id), memberJSON(t, id, "Ana", "", noLanguages))
	requireLoginResponse(t, api.getMember(ana.AccessToken, id), http.StatusTooManyRequests, rateLimited)
	requireLanguages(t, api.putLanguages(ana.AccessToken, anasLanguages), anasLanguages)
}

func TestNewUserLimitsForMemberReadsMatchTheDecisionLog(t *testing.T) {
	l := NewUserLimits(slog.New(slog.DiscardHandler))
	const burst = 60
	for i := range burst {
		if ok, _ := l.MemberRead.Allow("reader"); !ok {
			t.Fatalf("denied at %d, burst is %d", i+1, burst)
		}
	}
	ok, retryAfter := l.MemberRead.Allow("reader")
	if ok {
		t.Errorf("allowed past burst %d", burst)
	}
	if retryAfter <= 0 || retryAfter > time.Second {
		t.Errorf("retry after %v, want within the 1 s refill", retryAfter)
	}
	// Another reader has their own, and the write buckets are untouched.
	if ok, _ := l.MemberRead.Allow("other"); !ok {
		t.Error("another reader was refused")
	}
	if ok, _ := l.ProfileWrite.Allow("reader"); !ok {
		t.Error("the reader's profile write was refused")
	}
}

// ---- Member profile data is never logged ----

// Reading a member's profile writes no log line at all, found, missing,
// malformed or over the limit: so no id, name, text or language can be in one.
func TestMemberReadsAreNotLogged(t *testing.T) {
	logs := &bytes.Buffer{}
	// Five reads for each reader, so the sixth of Ana's is refused. The
	// limiter logs to the same place a production one would.
	api := newTestAPIWith(t, Options{UserLimits: UserLimits{
		MemberRead: ratelimit.New[string]("test", 5, time.Hour, 100, slog.New(slog.NewTextHandler(logs, nil)))}})
	ana := api.loggedIn(t, "ana@example.com")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "MARKERNAME", "MARKERBIO")
	languages := languagesJSON(entriesJSON("yue", "native"), entriesJSON("haw", "a2"))
	requireLanguages(t, api.putLanguages(ben.AccessToken, languages), languages)
	api.svc.Wait()
	before := api.logs.String()

	requireMember(t, api.getMember(ana.AccessToken, benID), memberJSON(t, benID, "MARKERNAME", "MARKERBIO", languages))
	requireMember(t, api.getMember(ben.AccessToken, benID), memberJSON(t, benID, "MARKERNAME", "MARKERBIO", languages))
	requireMemberNotFound(t, api.getMember(ana.AccessToken, unknownMemberID))
	requireMemberNotFound(t, api.getMember(ana.AccessToken, "MARKERID"))
	requireMemberNotFound(t, api.getMember(ana.AccessToken, strings.ToUpper(benID)))
	requireMemberNotFound(t, api.getMember(ana.AccessToken, api.userID(t, "ben@example.com")))
	requireLoginResponse(t, api.getMember(ana.AccessToken, benID), http.StatusTooManyRequests, rateLimited)

	api.svc.Wait()
	if added := strings.TrimPrefix(api.logs.String(), before); added != "" {
		t.Errorf("member reads were logged: %s", added)
	}

	// A request without a token is logged by authentication, as on every
	// protected route, and that line says nothing of what was asked for.
	requireUnauthorized(t, api.profileWith(http.MethodGet, membersPath+benID, ""))
	api.svc.Wait()
	after := api.logs.String()
	for _, private := range []string{benID, strings.ToUpper(benID), unknownMemberID, "MARKER", "yue", "haw", "native"} {
		if strings.Contains(after, private) || strings.Contains(logs.String(), private) {
			t.Errorf("logs contain %q: %s%s", private, after, logs)
		}
	}
}

// A read that fails inside is an opaque 500, logged with the route's pattern
// and never the id asked for or anything of the profile.
func TestGetMemberProfileInternalErrorIsOpaque(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com")
	ben, benID := api.memberWithProfile(t, "ben@example.com", "MARKERNAME", "MARKERBIO")
	languages := languagesJSON(entriesJSON("yue", "native"), "[]")
	requireLanguages(t, api.putLanguages(ben.AccessToken, languages), languages)
	t.Cleanup(func() {
		_, _ = api.pool.Exec(context.Background(), `ALTER TABLE profiles RENAME COLUMN test_hidden TO bio`)
		_, _ = api.pool.Exec(context.Background(), `ALTER TABLE user_languages RENAME COLUMN test_hidden TO level`)
	})

	// The languages read fails after the profile was found.
	api.exec(t, `ALTER TABLE user_languages RENAME COLUMN level TO test_hidden`)
	requireLoginResponse(t, api.getMember(ana.AccessToken, benID), http.StatusInternalServerError, internalError)
	// The profile read fails.
	api.exec(t, `ALTER TABLE profiles RENAME COLUMN bio TO test_hidden`)
	requireLoginResponse(t, api.getMember(ana.AccessToken, benID), http.StatusInternalServerError, internalError)
	// A malformed id still never reaches the broken query.
	requireMemberNotFound(t, api.getMember(ana.AccessToken, "not-an-id"))

	api.svc.Wait()
	logs := api.logs.String()
	if n := strings.Count(logs, "request failed"); n != 2 {
		t.Errorf("%d failures logged, want 2: %s", n, logs)
	}
	if !strings.Contains(logs, "GET /v1/profiles/{id}") {
		t.Errorf("failure not logged with its route pattern: %s", logs)
	}
	for _, private := range []string{benID, "MARKER", "yue", "native", ana.AccessToken} {
		if strings.Contains(logs, private) {
			t.Errorf("logs contain %q: %s", private, logs)
		}
	}
}
