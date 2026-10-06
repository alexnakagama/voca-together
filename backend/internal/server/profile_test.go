package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"vocatogether/backend/internal/auth"
	"vocatogether/backend/internal/profile"
	"vocatogether/backend/internal/ratelimit"
)

const (
	profilePath     = "/v1/me/profile"
	profileNotFound = `{"error":{"code":"profile_not_found"}}`
)

// profileWith sends method to target (profilePath plus any query) with body
// and the given Authorization header values (none if empty).
func (a testAPI) profileWith(method, target, body string, authorization ...string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, v := range authorization {
		req.Header.Add("Authorization", v)
	}
	a.handler.ServeHTTP(rec, req)
	return rec
}

func (a testAPI) getProfile(accessToken string) *httptest.ResponseRecorder {
	return a.profileWith(http.MethodGet, profilePath, "", "Bearer "+accessToken)
}

func (a testAPI) putProfile(accessToken, body string) *httptest.ResponseRecorder {
	return a.profileWith(http.MethodPut, profilePath, body, "Bearer "+accessToken)
}

// profileJSON is a PUT body, encoded so any text is valid JSON.
func profileJSON(t *testing.T, displayName, bio string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"display_name": displayName, "bio": bio})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type profileBody struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Bio         string    `json:"bio"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// requireProfile checks a successful profile response and returns its body.
// It must have exactly the profile's fields: its public id, no account id, no
// email, nothing else.
func requireProfile(t *testing.T, rec *httptest.ResponseRecorder) profileBody {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	var fields map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	if keys := slices.Sorted(maps.Keys(fields)); !slices.Equal(keys, []string{"bio", "created_at", "display_name", "id", "updated_at"}) {
		t.Errorf("fields = %v", keys)
	}
	var body profileBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if id, ok := profile.ParsePublicID(body.ID); !ok || id != body.ID {
		t.Errorf("id = %q, want a canonical public identifier", body.ID)
	}
	if body.CreatedAt.IsZero() || body.UpdatedAt.Before(body.CreatedAt) {
		t.Errorf("timestamps = %v, %v", body.CreatedAt, body.UpdatedAt)
	}
	return body
}

func (a testAPI) profileCount(t *testing.T) int {
	t.Helper()
	return a.count(t, "profiles")
}

// storedProfile reads addr's profile row straight from the database.
func (a testAPI) storedProfile(t *testing.T, addr string) (displayName, bio string) {
	t.Helper()
	err := a.pool.QueryRow(context.Background(),
		`SELECT p.display_name, p.bio FROM profiles p JOIN users u ON u.id = p.user_id WHERE u.email = $1`,
		addr).Scan(&displayName, &bio)
	if err != nil {
		t.Fatalf("profile of %s: %v", addr, err)
	}
	return displayName, bio
}

func TestGetProfileBeforeSavingOne(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	requireLoginResponse(t, api.getProfile(tokens.AccessToken), http.StatusNotFound, profileNotFound)
}

func TestPutProfileCreatesItAndGetReturnsIt(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	put := api.putProfile(tokens.AccessToken, profileJSON(t, "  Ana   López ", "Learning Japanese.\r\n"))
	saved := requireProfile(t, put)
	if saved.DisplayName != "Ana López" || saved.Bio != "Learning Japanese." {
		t.Errorf("saved = %+v, want the normalized text", saved)
	}

	get := api.getProfile(tokens.AccessToken)
	requireProfile(t, get)
	if get.Body.String() != put.Body.String() {
		t.Errorf("GET %s differs from PUT %s", get.Body, put.Body)
	}
}

// The profile's id is its public identifier (decision 031): assigned by the
// first save, the same in every later answer whatever was saved, and never
// the account's id that /v1/me returns.
func TestProfileIDIsStableAndNotTheAccountID(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com")
	ben := api.loggedIn(t, "ben@example.com")

	id := requireProfile(t, api.putProfile(ana.AccessToken, profileJSON(t, "Ana", "Hi"))).ID
	if got := requireProfile(t, api.getProfile(ana.AccessToken)).ID; got != id {
		t.Errorf("GET after the first save: id %s, want %s", got, id)
	}
	for name, body := range map[string]string{
		"an unchanged save": profileJSON(t, "Ana", "Hi"),
		"an edit":           profileJSON(t, "Ana L.", "Learning Japanese."),
		"clearing the bio":  `{"display_name":"Ana L."}`,
	} {
		if got := requireProfile(t, api.putProfile(ana.AccessToken, body)).ID; got != id {
			t.Errorf("PUT, %s: id %s, want %s", name, got, id)
		}
		if got := requireProfile(t, api.getProfile(ana.AccessToken)).ID; got != id {
			t.Errorf("GET after %s: id %s, want %s", name, got, id)
		}
	}
	// A second session of the same member reads the same one.
	second := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	if got := requireProfile(t, api.getProfile(second.AccessToken)).ID; got != id {
		t.Errorf("another session: id %s, want %s", got, id)
	}

	accountID := api.meID(t, ana.AccessToken)
	if accountID != api.userID(t, "ana@example.com") {
		t.Fatalf("/v1/me id = %s, not the user's", accountID)
	}
	if id == accountID {
		t.Errorf("the profile's id is the account's id %s", accountID)
	}
	if benID := requireProfile(t, api.putProfile(ben.AccessToken, profileJSON(t, "Ben", ""))).ID; benID == id {
		t.Errorf("two profiles share the id %s", id)
	}

	// The id is the server's: a body that carries one, even the profile's
	// own, is refused and saves nothing.
	for _, body := range []string{
		`{"id":"` + id + `","display_name":"Mallory","bio":""}`,
		`{"id":"` + accountID + `","display_name":"Mallory","bio":""}`,
		`{"public_id":"` + id + `","display_name":"Mallory","bio":""}`,
	} {
		requireLoginResponse(t, api.putProfile(ana.AccessToken, body), http.StatusBadRequest, invalidRequest)
	}
	if name, _ := api.storedProfile(t, "ana@example.com"); name != "Ana L." {
		t.Errorf("a refused save changed the name to %q", name)
	}
	if got := requireProfile(t, api.getProfile(ana.AccessToken)).ID; got != id {
		t.Errorf("after the refused saves: id %s, want %s", got, id)
	}
}

// PUT replaces the whole profile: a bio that is absent or null is cleared.
func TestPutProfileReplacesTheWholeProfile(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	first := requireProfile(t, api.putProfile(tokens.AccessToken, profileJSON(t, "Ana", "First")))

	for _, body := range []string{`{"display_name":"Ana L."}`, `{"display_name":"Ana L.","bio":null}`} {
		got := requireProfile(t, api.putProfile(tokens.AccessToken, body))
		if got.DisplayName != "Ana L." || got.Bio != "" {
			t.Errorf("%s: got %+v", body, got)
		}
		if !got.CreatedAt.Equal(first.CreatedAt) {
			t.Errorf("%s: created_at changed", body)
		}
	}
	if n := api.profileCount(t); n != 1 {
		t.Errorf("profiles = %d, want 1", n)
	}
}

// A repeated PUT (a client retrying after a timeout or a 503) answers exactly
// as the first one did.
func TestPutProfileIsIdempotent(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	body := profileJSON(t, "Ana", "Hi")

	first := api.putProfile(tokens.AccessToken, body)
	requireProfile(t, first)
	second := api.putProfile(tokens.AccessToken, body)
	requireProfile(t, second)
	if second.Body.String() != first.Body.String() {
		t.Errorf("retry answered %s, first answered %s", second.Body, first.Body)
	}
	api.svc.Wait()
	if n := strings.Count(api.logs.String(), "profile: saved"); n != 1 {
		t.Errorf("%d saves logged, want 1: the retry changed nothing", n)
	}
}

// The caller's session is the only thing that selects a profile: nothing in
// the URL or the body can point a request at someone else's.
func TestProfileBelongsToTheCallerOnly(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com")
	ben := api.loggedIn(t, "ben@example.com")
	benID := api.userID(t, "ben@example.com")
	requireProfile(t, api.putProfile(ana.AccessToken, profileJSON(t, "Ana", "Ana's bio")))

	// Ben has none, although Ana does.
	requireLoginResponse(t, api.getProfile(ben.AccessToken), http.StatusNotFound, profileNotFound)
	requireProfile(t, api.putProfile(ben.AccessToken, profileJSON(t, "Ben", "Ben's bio")))

	// An id in the query is ignored: Ana reads and writes her own.
	got := requireProfile(t, api.profileWith(http.MethodGet, profilePath+"?user_id="+benID, "", "Bearer "+ana.AccessToken))
	if got.DisplayName != "Ana" {
		t.Errorf("GET with ben's id in the query returned %+v", got)
	}
	requireProfile(t, api.profileWith(http.MethodPut, profilePath+"?user_id="+benID+"&id="+benID,
		profileJSON(t, "Ana 2", ""), "Bearer "+ana.AccessToken))

	// An id in the body is not a writable field: the request is refused.
	for _, body := range []string{
		`{"user_id":"` + benID + `","display_name":"Mallory","bio":""}`,
		`{"id":"` + benID + `","display_name":"Mallory","bio":""}`,
	} {
		requireLoginResponse(t, api.putProfile(ana.AccessToken, body), http.StatusBadRequest, invalidRequest)
	}

	if name, bio := api.storedProfile(t, "ben@example.com"); name != "Ben" || bio != "Ben's bio" {
		t.Errorf("ben's profile = %q, %q", name, bio)
	}
	if name, bio := api.storedProfile(t, "ana@example.com"); name != "Ana 2" || bio != "" {
		t.Errorf("ana's profile = %q, %q", name, bio)
	}
	if got := requireProfile(t, api.getProfile(ben.AccessToken)); got.DisplayName != "Ben" {
		t.Errorf("ben reads %+v", got)
	}
}

func TestProfileRejectsMissingOrUnusableCredentials(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	revoked := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	requireLoggedOut(t, api.logout(revoked.AccessToken))
	expired := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	api.exec(t, `UPDATE sessions SET access_expires_at = now() - interval '1 second' WHERE access_token_hash = $1`,
		auth.HashToken(expired.AccessToken))
	body := profileJSON(t, "Ana", "")

	for name, tc := range map[string]struct {
		target string
		header []string
	}{
		"no header":           {},
		"basic scheme":        {header: []string{"Basic " + tokens.AccessToken}},
		"refresh token":       {header: []string{"Bearer " + tokens.RefreshToken}},
		"garbage":             {header: []string{"Bearer " + auth.AccessTokenPrefix + "garbage"}},
		"unknown":             {header: []string{"Bearer " + auth.NewToken(auth.AccessTokenPrefix).Raw}},
		"revoked":             {header: []string{"Bearer " + revoked.AccessToken}},
		"expired":             {header: []string{"Bearer " + expired.AccessToken}},
		"two headers":         {header: []string{"Bearer " + tokens.AccessToken, "Bearer " + tokens.AccessToken}},
		"token only in query": {target: profilePath + "?access_token=" + tokens.AccessToken},
	} {
		t.Run(name, func(t *testing.T) {
			target := tc.target
			if target == "" {
				target = profilePath
			}
			requireUnauthorized(t, api.profileWith(http.MethodGet, target, "", tc.header...))
			requireUnauthorized(t, api.profileWith(http.MethodPut, target, body, tc.header...))
		})
	}
	if n := api.profileCount(t); n != 0 {
		t.Errorf("profiles = %d after only unauthenticated requests, want 0", n)
	}
}

func TestPutProfileRejectsMalformedRequests(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	for name, body := range map[string]string{
		"empty body":        ``,
		"not JSON":          `display_name=Ana`,
		"truncated":         `{"display_name":"Ana"`,
		"array":             `[{"display_name":"Ana"}]`,
		"string":            `"Ana"`,
		"number for a name": `{"display_name":5}`,
		"object for a bio":  `{"display_name":"Ana","bio":{}}`,
		"unknown field":     `{"display_name":"Ana","avatar_url":"https://example.com/a.png"}`,
		"created_at":        `{"display_name":"Ana","created_at":"2020-01-01T00:00:00Z"}`,
		"updated_at":        `{"display_name":"Ana","updated_at":"2020-01-01T00:00:00Z"}`,
		"email":             `{"display_name":"Ana","email":"mallory@example.com"}`,
		"trailing data":     `{"display_name":"Ana"}{"display_name":"Ben"}`,
		"oversized":         `{"display_name":"Ana","bio":"` + strings.Repeat("a", maxProfileBodyBytes) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			requireLoginResponse(t, api.putProfile(tokens.AccessToken, body), http.StatusBadRequest, invalidRequest)
		})
	}
	if n := api.profileCount(t); n != 0 {
		t.Errorf("profiles = %d, want 0", n)
	}
}

func TestPutProfileValidation(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	bidiOverride := string(rune(0x202E))

	for name, tc := range map[string]struct{ body, want string }{
		"no name":       {`{}`, `{"field":"display_name","code":"required"}`},
		"null name":     {`{"display_name":null}`, `{"field":"display_name","code":"required"}`},
		"blank name":    {profileJSON(t, "   ", ""), `{"field":"display_name","code":"required"}`},
		"long name":     {profileJSON(t, strings.Repeat("a", 51), ""), `{"field":"display_name","code":"too_long"}`},
		"disguised":     {profileJSON(t, "Ana"+bidiOverride, ""), `{"field":"display_name","code":"invalid"}`},
		"long bio":      {profileJSON(t, "Ana", strings.Repeat("a", 501)), `{"field":"bio","code":"too_long"}`},
		"disguised bio": {profileJSON(t, "Ana", "Hi"+bidiOverride), `{"field":"bio","code":"invalid"}`},
		"both fields": {profileJSON(t, "", strings.Repeat("a", 501)),
			`{"field":"display_name","code":"required"},{"field":"bio","code":"too_long"}`},
	} {
		t.Run(name, func(t *testing.T) {
			requireLoginResponse(t, api.putProfile(tokens.AccessToken, tc.body), http.StatusUnprocessableEntity,
				`{"error":{"code":"validation_failed","fields":[`+tc.want+`]}}`)
		})
	}
	if n := api.profileCount(t); n != 0 {
		t.Errorf("profiles = %d, want 0", n)
	}
}

// The longest valid profile fits in the body limit even when a client
// escapes every character as a surrogate pair (12 bytes each).
func TestPutProfileAcceptsTheLongestProfileFullyEscaped(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	backslash := string(rune(92))
	escaped := backslash + "ud840" + backslash + "udc00" // U+20000, a CJK ideograph
	body := `{"display_name":"` + strings.Repeat(escaped, profile.DisplayNameMaxLength) +
		`","bio":"` + strings.Repeat(escaped, profile.BioMaxLength) + `"}`
	if len(body) > maxProfileBodyBytes {
		t.Fatalf("body is %d bytes, over the %d limit", len(body), maxProfileBodyBytes)
	}

	got := requireProfile(t, api.putProfile(tokens.AccessToken, body))
	ideograph := string(rune(0x20000))
	if got.DisplayName != strings.Repeat(ideograph, 50) || got.Bio != strings.Repeat(ideograph, 500) {
		t.Errorf("stored %d and %d bytes", len(got.DisplayName), len(got.Bio))
	}
}

func TestProfileAllowsOnlyGetAndPut(t *testing.T) {
	// Nil services: the mux must answer before authentication runs.
	h := New(slog.New(slog.DiscardHandler), nil, nil, nil, Options{})
	for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, profilePath, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
	}
}

// Mounted without requireAccessToken by mistake, the handlers fail closed.
func TestProfileHandlersWithoutMiddlewareFailClosed(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	for method, h := range map[string]http.Handler{
		http.MethodGet: handleGetProfile(logger, nil),
		http.MethodPut: handlePutProfile(logger, nil),
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, profilePath, strings.NewReader(`{"display_name":"Ana"}`)))
		requireResponse(t, rec, http.StatusInternalServerError, internalError)
	}
}

// A user deleted after authentication but before the write: their sessions
// are gone with them, so the answer is the 401 of a dead credential (016).
func TestPutProfileForAUserDeletedMeanwhile(t *testing.T) {
	api := newTestAPI(t)
	api.loggedIn(t, "ana@example.com")
	id := api.userID(t, "ana@example.com")
	api.exec(t, `DELETE FROM users WHERE id = $1`, id)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, profilePath, strings.NewReader(`{"display_name":"Ana"}`))
	req = req.WithContext(context.WithValue(req.Context(), identityKey{}, auth.Identity{UserID: id}))
	handlePutProfile(slog.New(slog.DiscardHandler), profile.NewService(api.pool, slog.New(slog.DiscardHandler))).
		ServeHTTP(rec, req)

	requireResponse(t, rec, http.StatusUnauthorized, invalidAccessToken)
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", got)
	}
	if n := api.profileCount(t); n != 0 {
		t.Errorf("profiles = %d, want 0", n)
	}
}

func TestProfileInternalErrorIsOpaque(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	// Break both queries by hiding a column they name.
	api.exec(t, `ALTER TABLE profiles RENAME COLUMN bio TO test_hidden`)
	t.Cleanup(func() {
		_, _ = api.pool.Exec(context.Background(), `ALTER TABLE profiles RENAME COLUMN test_hidden TO bio`)
	})

	requireLoginResponse(t, api.getProfile(tokens.AccessToken), http.StatusInternalServerError, internalError)
	requireLoginResponse(t, api.putProfile(tokens.AccessToken, profileJSON(t, "MARKERNAME", "MARKERBIO")),
		http.StatusInternalServerError, internalError)

	api.svc.Wait()
	logs := api.logs.String()
	if !strings.Contains(logs, "request failed") || !strings.Contains(logs, profilePath) {
		t.Errorf("failure not logged with its route: %s", logs)
	}
	if strings.Contains(logs, "MARKER") || strings.Contains(logs, tokens.AccessToken) {
		t.Errorf("logs contain profile text or the token: %s", logs)
	}
}

// What a member writes never reaches the logs, on success or on rejection.
func TestProfileTextIsNeverLogged(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	requireProfile(t, api.putProfile(tokens.AccessToken, profileJSON(t, "MARKERNAME", "MARKERBIO")))
	requireProfile(t, api.getProfile(tokens.AccessToken))
	api.putProfile(tokens.AccessToken, profileJSON(t, "MARKERNAME"+string(rune(0x202E)), "MARKERBIO"))
	api.putProfile(tokens.AccessToken, `{"display_name":"MARKERNAME","marker_field":"MARKERVALUE"}`)

	api.svc.Wait()
	logs := api.logs.String()
	if !strings.Contains(logs, "profile: saved") {
		t.Errorf("the save was not logged: %s", logs)
	}
	if strings.Contains(logs, "MARKER") || strings.Contains(logs, "marker_field") || strings.Contains(logs, tokens.AccessToken) {
		t.Errorf("logs contain profile text, a body or the token: %s", logs)
	}
}

// ---- Per-user write limit ----

func tightUserLimiter() *ratelimit.Limiter[string] {
	return ratelimit.New[string]("test", 1, time.Hour, 100, slog.New(slog.DiscardHandler))
}

func TestPutProfileIsLimitedPerUser(t *testing.T) {
	api := newTestAPIWith(t, Options{UserLimits: UserLimits{ProfileWrite: tightUserLimiter()}})
	ana := api.loggedIn(t, "ana@example.com")
	ben := api.loggedIn(t, "ben@example.com")

	// Requests that don't authenticate never spend anyone's allowance.
	requireUnauthorized(t, api.profileWith(http.MethodPut, profilePath, profileJSON(t, "Ana", "")))
	requireUnauthorized(t, api.putProfile(auth.NewToken(auth.AccessTokenPrefix).Raw, profileJSON(t, "Ana", "")))

	requireProfile(t, api.putProfile(ana.AccessToken, profileJSON(t, "Ana", "")))
	rec := api.putProfile(ana.AccessToken, profileJSON(t, "Ana changed", ""))
	requireLoginResponse(t, rec, http.StatusTooManyRequests, rateLimited)
	if got := rec.Header().Get("Retry-After"); got == "" || got == "0" {
		t.Errorf("Retry-After = %q", got)
	}
	// A 429 did nothing.
	if name, _ := api.storedProfile(t, "ana@example.com"); name != "Ana" {
		t.Errorf("a limited request changed the name to %q", name)
	}

	// The limit is the user's: a second session of Ana's shares it, Ben has his own.
	second := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	requireLoginResponse(t, api.putProfile(second.AccessToken, profileJSON(t, "Ana", "")), http.StatusTooManyRequests, rateLimited)
	requireProfile(t, api.putProfile(ben.AccessToken, profileJSON(t, "Ben", "")))

	// Reading is not limited.
	requireProfile(t, api.getProfile(ana.AccessToken))
}

// Junk costs a token too: the limit is checked before the body is read.
func TestPutProfileLimitCountsMalformedRequests(t *testing.T) {
	api := newTestAPIWith(t, Options{UserLimits: UserLimits{ProfileWrite: tightUserLimiter()}})
	ana := api.loggedIn(t, "ana@example.com")

	requireLoginResponse(t, api.putProfile(ana.AccessToken, `not json`), http.StatusBadRequest, invalidRequest)
	requireLoginResponse(t, api.putProfile(ana.AccessToken, profileJSON(t, "Ana", "")), http.StatusTooManyRequests, rateLimited)
}

func TestNewUserLimitsMatchesTheDecisionLog(t *testing.T) {
	l := NewUserLimits(slog.New(slog.DiscardHandler))
	const burst = 10
	for i := range burst {
		if ok, _ := l.ProfileWrite.Allow("user"); !ok {
			t.Fatalf("denied at %d, burst is %d", i+1, burst)
		}
	}
	ok, retryAfter := l.ProfileWrite.Allow("user")
	if ok {
		t.Errorf("allowed past burst %d", burst)
	}
	if retryAfter <= 0 || retryAfter > 6*time.Second {
		t.Errorf("retry after %v, want within the 6 s refill", retryAfter)
	}
}

// ---- Text rules over HTTP ----

// cp is the one-character string for a code point, so tests can name
// characters that are invisible in source.
func cp(r rune) string { return string(r) }

func invalidField(field, code string) string {
	return `{"error":{"code":"validation_failed","fields":[{"field":"` + field + `","code":"` + code + `"}]}}`
}

// Spaces of any kind, tabs and line breaks in a name are word separators and
// are stored as one space: a name pasted over two lines is one name.
func TestPutProfileTurnsNameWhitespaceAndLineBreaksIntoOneSpace(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	for name, sep := range map[string]string{
		"line feed": "\n", "CRLF": "\r\n", "carriage return": "\r", "tab": "\t",
		"no-break space": cp(0x00A0), "ideographic space": cp(0x3000), "a mix": " \n\t" + cp(0x00A0) + "\r\n ",
	} {
		t.Run(name, func(t *testing.T) {
			got := requireProfile(t, api.putProfile(tokens.AccessToken, profileJSON(t, sep+"Ana"+sep+"López"+sep, "")))
			if got.DisplayName != "Ana López" {
				t.Errorf("display_name = %q, want %q", got.DisplayName, "Ana López")
			}
		})
	}
	if name, _ := api.storedProfile(t, "ana@example.com"); name != "Ana López" {
		t.Errorf("stored %q", name)
	}
}

// Every other control or separator character is refused in both fields,
// wherever it stands: never dropped, never turned into something else.
func TestPutProfileRefusesControlCharacters(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	for name, r := range map[string]rune{
		"null": 0x0000, "bell": 0x0007, "vertical tab": 0x000B, "form feed": 0x000C, "escape": 0x001B,
		"delete": 0x007F, "next line": 0x0085, "line separator": 0x2028, "paragraph separator": 0x2029,
		"bidi override": 0x202E, "bidi isolate": 0x2066, "zero-width space": 0x200B, "byte order mark": 0xFEFF,
	} {
		t.Run(name, func(t *testing.T) {
			for _, text := range []string{"Ana" + cp(r) + "López", cp(r) + "Ana", "Ana" + cp(r)} {
				requireLoginResponse(t, api.putProfile(tokens.AccessToken, profileJSON(t, text, "")),
					http.StatusUnprocessableEntity, invalidField("display_name", "invalid"))
				requireLoginResponse(t, api.putProfile(tokens.AccessToken, profileJSON(t, "Ana", text)),
					http.StatusUnprocessableEntity, invalidField("bio", "invalid"))
			}
		})
	}
	if n := api.profileCount(t); n != 0 {
		t.Errorf("profiles = %d, want 0", n)
	}
}

// A name nobody can see is not a name: blank characters that Unicode counts
// as letters or symbols are refused, alone or as padding.
func TestPutProfileRefusesInvisibleNames(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	for name, r := range map[string]rune{
		"hangul filler": 0x3164, "hangul choseong filler": 0x115F, "hangul jungseong filler": 0x1160,
		"halfwidth hangul filler": 0xFFA0, "braille blank": 0x2800, "combining grapheme joiner": 0x034F,
		"khmer inherent aq": 0x17B4, "khmer inherent aa": 0x17B5,
	} {
		t.Run(name, func(t *testing.T) {
			for _, text := range []string{cp(r), cp(r) + cp(r) + cp(r), "Ana" + cp(r), cp(r) + "Ana"} {
				requireLoginResponse(t, api.putProfile(tokens.AccessToken, profileJSON(t, text, "")),
					http.StatusUnprocessableEntity, invalidField("display_name", "invalid"))
			}
			requireLoginResponse(t, api.putProfile(tokens.AccessToken, profileJSON(t, "Ana", "Hi"+cp(r))),
				http.StatusUnprocessableEntity, invalidField("bio", "invalid"))
		})
	}
	// Joiners can't be used to stack marks past the limit or to pad a name.
	marks := strings.Repeat(cp(0x0335), 8)
	for _, text := range []string{"a" + marks + cp(0x200D) + marks, "a" + strings.Repeat(cp(0x200D), 9)} {
		requireLoginResponse(t, api.putProfile(tokens.AccessToken, profileJSON(t, text, "")),
			http.StatusUnprocessableEntity, invalidField("display_name", "invalid"))
	}
	if n := api.profileCount(t); n != 0 {
		t.Errorf("profiles = %d, want 0", n)
	}
}

func TestPutProfileNormalizesBioWhitespace(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	got := requireProfile(t, api.putProfile(tokens.AccessToken,
		profileJSON(t, "Ana", " \r\nOne  \r\n \t \n"+cp(0x3000)+"\n\n  Two\tx \n\n")))
	if want := "One\n\n  Two x"; got.Bio != want {
		t.Errorf("bio = %q, want %q", got.Bio, want)
	}
}

// Text over a field's limit is a validation error naming the field, however
// far over it is: the body limit is far above anything a person pastes, so
// the rule that answers is the field's own.
func TestPutProfileOverlongTextIsAValidationErrorNotABadRequest(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	for name, bio := range map[string]string{
		"one over":                strings.Repeat("a", profile.BioMaxLength+1),
		"9 000 characters":        strings.Repeat("a", 9000),
		"3 000 three-byte":        strings.Repeat("あ", 3000),
		"5 000 four-byte":         strings.Repeat(cp(0x20000), 5000),
		"20 000 with line breaks": strings.Repeat("a line of text\n", 20000/15),
	} {
		t.Run("bio "+name, func(t *testing.T) {
			requireLoginResponse(t, api.putProfile(tokens.AccessToken, profileJSON(t, "Ana", bio)),
				http.StatusUnprocessableEntity, invalidField("bio", "too_long"))
		})
	}
	for name, displayName := range map[string]string{
		"one over":         strings.Repeat("a", profile.DisplayNameMaxLength+1),
		"5 000 characters": strings.Repeat("a", 5000),
	} {
		t.Run("name "+name, func(t *testing.T) {
			requireLoginResponse(t, api.putProfile(tokens.AccessToken, profileJSON(t, displayName, "")),
				http.StatusUnprocessableEntity, invalidField("display_name", "too_long"))
		})
	}
	if n := api.profileCount(t); n != 0 {
		t.Errorf("profiles = %d, want 0", n)
	}

	// At the limits the text is stored.
	got := requireProfile(t, api.putProfile(tokens.AccessToken, profileJSON(t,
		strings.Repeat("あ", profile.DisplayNameMaxLength), strings.Repeat("あ", profile.BioMaxLength))))
	if len([]rune(got.DisplayName)) != 50 || len([]rune(got.Bio)) != 500 {
		t.Errorf("stored %d and %d characters", len([]rune(got.DisplayName)), len([]rune(got.Bio)))
	}
}

// The body limit itself: a body of exactly the limit is read and judged by
// the field rules; one byte more is refused unread.
func TestPutProfileBodyLimit(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	body := func(size int) string {
		const open, end = `{"display_name":"Ana","bio":"`, `"}`
		return open + strings.Repeat("a", size-len(open)-len(end)) + end
	}

	atLimit := body(maxProfileBodyBytes)
	if len(atLimit) != maxProfileBodyBytes {
		t.Fatalf("body is %d bytes", len(atLimit))
	}
	requireLoginResponse(t, api.putProfile(tokens.AccessToken, atLimit),
		http.StatusUnprocessableEntity, invalidField("bio", "too_long"))
	requireLoginResponse(t, api.putProfile(tokens.AccessToken, body(maxProfileBodyBytes+1)),
		http.StatusBadRequest, invalidRequest)
	requireLoginResponse(t, api.putProfile(tokens.AccessToken, body(10*maxProfileBodyBytes)),
		http.StatusBadRequest, invalidRequest)
	if n := api.profileCount(t); n != 0 {
		t.Errorf("profiles = %d, want 0", n)
	}
}

// ---- Fail-closed and race paths ----

// Mounted without requireAccessToken by mistake, the limiter refuses the
// request instead of limiting nobody.
func TestLimitByUserWithoutAuthenticationFailsClosed(t *testing.T) {
	reached := false
	h := limitByUser(slog.New(slog.DiscardHandler), tightUserLimiter())(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, profilePath, strings.NewReader(`{"display_name":"Ana"}`)))
	requireResponse(t, rec, http.StatusInternalServerError, internalError)
	if reached {
		t.Error("the handler ran without an identity")
	}
}

// A user deleted after authentication but before the read has no profile
// row any more (it went with the user): the answer is the ordinary 404, and
// their next request gets the 401 of a dead session.
func TestGetProfileForAUserDeletedMeanwhile(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	requireProfile(t, api.putProfile(tokens.AccessToken, profileJSON(t, "Ana", "")))
	id := api.userID(t, "ana@example.com")
	api.exec(t, `DELETE FROM users WHERE id = $1`, id)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, profilePath, nil)
	req = req.WithContext(context.WithValue(req.Context(), identityKey{}, auth.Identity{UserID: id}))
	handleGetProfile(slog.New(slog.DiscardHandler), profile.NewService(api.pool, slog.New(slog.DiscardHandler))).
		ServeHTTP(rec, req)
	requireResponse(t, rec, http.StatusNotFound, profileNotFound)

	requireUnauthorized(t, api.getProfile(tokens.AccessToken))
	requireUnauthorized(t, api.putProfile(tokens.AccessToken, profileJSON(t, "Ana", "")))
}
