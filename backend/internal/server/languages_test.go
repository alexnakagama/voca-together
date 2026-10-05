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
	"sync"
	"testing"
	"time"

	"vocatogether/backend/internal/auth"
	"vocatogether/backend/internal/language"
)

const (
	catalogPath     = "/v1/languages"
	myLanguagesPath = "/v1/me/languages"
	noLanguages     = `{"spoken":[],"learning":[]}`
)

func (a testAPI) getCatalog(accessToken string) *httptest.ResponseRecorder {
	return a.profileWith(http.MethodGet, catalogPath, "", "Bearer "+accessToken)
}

func (a testAPI) getLanguages(accessToken string) *httptest.ResponseRecorder {
	return a.profileWith(http.MethodGet, myLanguagesPath, "", "Bearer "+accessToken)
}

func (a testAPI) putLanguages(accessToken, body string) *httptest.ResponseRecorder {
	return a.profileWith(http.MethodPut, myLanguagesPath, body, "Bearer "+accessToken)
}

// entriesJSON is a list of languages as the API writes it, from pairs of
// language and level.
func entriesJSON(pairs ...string) string {
	items := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		items = append(items, `{"language":"`+pairs[i]+`","level":"`+pairs[i+1]+`"}`)
	}
	return "[" + strings.Join(items, ",") + "]"
}

// languagesJSON is a whole body, request or response, from two entriesJSON
// lists.
func languagesJSON(spoken, learning string) string {
	return `{"spoken":` + spoken + `,"learning":` + learning + `}`
}

// anasLanguages is a valid selection with both lists filled.
var anasLanguages = languagesJSON(entriesJSON("es", "native", "en", "c1"), entriesJSON("ja", "a2"))

// requireLanguages checks a successful languages response: exactly want,
// which also means exactly its fields, and never cached.
func requireLanguages(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	requireLoginResponse(t, rec, http.StatusOK, want)
}

func invalidLanguages(fields ...string) string {
	items := make([]string, 0, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		items = append(items, `{"field":"`+fields[i]+`","code":"`+fields[i+1]+`"}`)
	}
	return `{"error":{"code":"validation_failed","fields":[` + strings.Join(items, ",") + `]}}`
}

func (a testAPI) languageCount(t *testing.T) int {
	t.Helper()
	return a.count(t, "user_languages")
}

// storedLanguages reads addr's rows straight from the database, as
// "kind position code level" in the stored order, spoken first.
func (a testAPI) storedLanguages(t *testing.T, addr string) []string {
	t.Helper()
	var rows []string
	err := a.pool.QueryRow(context.Background(),
		`SELECT coalesce(array_agg(l.kind || ' ' || l.position || ' ' || l.language_code || ' ' || l.level
		                           ORDER BY l.kind DESC, l.position), '{}')
		   FROM user_languages l JOIN users u ON u.id = l.user_id WHERE u.email = $1`, addr).Scan(&rows)
	if err != nil {
		t.Fatalf("languages of %s: %v", addr, err)
	}
	return rows
}

// as returns r with identity id in its context, as requireAccessToken leaves
// it: for calling a handler directly.
func as(r *http.Request, userID string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), identityKey{}, auth.Identity{UserID: userID}))
}

func (a testAPI) languageService() *language.Service {
	return language.NewService(a.pool, slog.New(slog.DiscardHandler))
}

// ---- Reading ----

// A member who has chosen nothing has two empty lists, not a missing
// resource and not nulls.
func TestGetLanguagesBeforeSavingAny(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	requireLanguages(t, api.getLanguages(tokens.AccessToken), noLanguages)
}

func TestLanguageCatalog(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	rec := api.getCatalog(tokens.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	var body map[string][]map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	if keys := slices.Sorted(maps.Keys(body)); !slices.Equal(keys, []string{"languages"}) {
		t.Errorf("fields = %v", keys)
	}
	catalog := body["languages"]
	if n := api.count(t, "languages"); len(catalog) != n || n == 0 {
		t.Errorf("%d languages returned, the catalog has %d", len(catalog), n)
	}
	names := make([]string, len(catalog))
	byCode := make(map[string]map[string]string, len(catalog))
	for i, l := range catalog {
		if keys := slices.Sorted(maps.Keys(l)); !slices.Equal(keys, []string{"code", "endonym", "name"}) {
			t.Fatalf("language %d has fields %v", i, keys)
		}
		names[i] = l["name"]
		byCode[l["code"]] = l
	}
	if !slices.IsSorted(names) {
		t.Errorf("the catalog is not ordered by name")
	}
	if es := byCode["es"]; es["name"] != "Spanish" || es["endonym"] != "Español" {
		t.Errorf("es = %v", es)
	}
}

// The catalog is the same for everyone and reading it changes nothing.
func TestLanguageCatalogIsTheSameForEveryMember(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com")
	ben := api.loggedIn(t, "ben@example.com")
	requireLanguages(t, api.putLanguages(ana.AccessToken, anasLanguages), anasLanguages)

	first := api.getCatalog(ana.AccessToken)
	if other := api.getCatalog(ben.AccessToken); other.Body.String() != first.Body.String() || other.Code != http.StatusOK {
		t.Errorf("ben's catalog differs from ana's (status %d)", other.Code)
	}
}

// ---- Saving ----

func TestPutLanguagesSavesThemAndGetReturnsThem(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	requireLanguages(t, api.putLanguages(tokens.AccessToken, anasLanguages), anasLanguages)
	requireLanguages(t, api.getLanguages(tokens.AccessToken), anasLanguages)

	want := []string{"spoken 0 es 7", "spoken 1 en 5", "learning 0 ja 2"}
	if got := api.storedLanguages(t, "ana@example.com"); !slices.Equal(got, want) {
		t.Errorf("stored %v, want %v", got, want)
	}
}

// PUT replaces the whole selection, in the order sent: a list that is
// absent, null or empty is cleared.
func TestPutLanguagesReplacesTheWholeSelection(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	reordered := languagesJSON(entriesJSON("en", "c2", "fr", "b1", "es", "native"), entriesJSON("de", "a1"))
	requireLanguages(t, api.putLanguages(tokens.AccessToken, anasLanguages), anasLanguages)
	requireLanguages(t, api.putLanguages(tokens.AccessToken, reordered), reordered)
	requireLanguages(t, api.getLanguages(tokens.AccessToken), reordered)

	onlySpoken := languagesJSON(entriesJSON("es", "native"), "[]")
	for _, body := range []string{
		`{"spoken":` + entriesJSON("es", "native") + `}`,
		`{"spoken":` + entriesJSON("es", "native") + `,"learning":null}`,
		onlySpoken,
	} {
		requireLanguages(t, api.putLanguages(tokens.AccessToken, anasLanguages), anasLanguages)
		requireLanguages(t, api.putLanguages(tokens.AccessToken, body), onlySpoken)
		requireLanguages(t, api.getLanguages(tokens.AccessToken), onlySpoken)
	}

	for _, body := range []string{`{}`, `{"spoken":null,"learning":null}`, noLanguages} {
		requireLanguages(t, api.putLanguages(tokens.AccessToken, anasLanguages), anasLanguages)
		requireLanguages(t, api.putLanguages(tokens.AccessToken, body), noLanguages)
		if n := api.languageCount(t); n != 0 {
			t.Errorf("%s: %d rows left, want 0", body, n)
		}
	}
}

func TestPutLanguagesAcceptsTheMaximumOfEachKind(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	body := languagesJSON(
		entriesJSON("es", "native", "en", "native", "fr", "c2", "de", "b2", "it", "a1"),
		entriesJSON("ja", "a1", "ko", "a2", "pt", "b1", "yue", "c1", "zh", "c2"))

	requireLanguages(t, api.putLanguages(tokens.AccessToken, body), body)
	if n := api.languageCount(t); n != 2*language.MaxPerKind {
		t.Errorf("rows = %d", n)
	}
}

// A repeated PUT (a client retrying after a timeout, a 503 or a refreshed
// token) answers exactly as the first one did and writes nothing.
func TestPutLanguagesIsIdempotent(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	versions := func() string {
		var v string
		err := api.pool.QueryRow(context.Background(),
			`SELECT coalesce(string_agg(xmin::text || ':' || ctid::text, ',' ORDER BY kind, position), '') FROM user_languages`).Scan(&v)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	first := api.putLanguages(tokens.AccessToken, anasLanguages)
	requireLanguages(t, first, anasLanguages)
	written := versions()
	for range 3 {
		again := api.putLanguages(tokens.AccessToken, anasLanguages)
		requireLanguages(t, again, anasLanguages)
		if !slices.Equal(slices.Sorted(maps.Keys(again.Header())), slices.Sorted(maps.Keys(first.Header()))) {
			t.Errorf("retry headers %v, first %v", again.Header(), first.Header())
		}
	}
	if got := versions(); got != written {
		t.Errorf("a retry rewrote the rows: %s, were %s", got, written)
	}
	api.svc.Wait()
	if n := strings.Count(api.logs.String(), "languages: saved"); n != 1 {
		t.Errorf("%d saves logged, want 1: the retries changed nothing", n)
	}

	// Clearing is idempotent too.
	for range 2 {
		requireLanguages(t, api.putLanguages(tokens.AccessToken, noLanguages), noLanguages)
	}
	api.svc.Wait()
	if n := strings.Count(api.logs.String(), "languages: saved"); n != 2 {
		t.Errorf("%d saves logged, want 2", n)
	}
}

// ---- Authorization ----

// The caller's session is the only thing that selects a selection: nothing
// in the URL or the body can point a request at someone else's.
func TestLanguagesBelongToTheCallerOnly(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com")
	ben := api.loggedIn(t, "ben@example.com")
	benID := api.userID(t, "ben@example.com")
	bens := languagesJSON(entriesJSON("de", "native"), entriesJSON("es", "b1"))
	requireLanguages(t, api.putLanguages(ana.AccessToken, anasLanguages), anasLanguages)

	// Ben has none, although Ana does.
	requireLanguages(t, api.getLanguages(ben.AccessToken), noLanguages)
	requireLanguages(t, api.putLanguages(ben.AccessToken, bens), bens)

	// An id in the query is ignored: Ana reads and writes her own.
	query := "?user_id=" + benID + "&id=" + benID
	requireLanguages(t, api.profileWith(http.MethodGet, myLanguagesPath+query, "", "Bearer "+ana.AccessToken), anasLanguages)
	anas := languagesJSON(entriesJSON("es", "native"), entriesJSON("ko", "a1"))
	requireLanguages(t, api.profileWith(http.MethodPut, myLanguagesPath+query, anas, "Bearer "+ana.AccessToken), anas)

	// An id in the body is not a writable field: the request is refused.
	for _, body := range []string{
		`{"user_id":"` + benID + `","spoken":[],"learning":[]}`,
		`{"id":"` + benID + `","spoken":[],"learning":[]}`,
		`{"spoken":[{"user_id":"` + benID + `","language":"fr","level":"a1"}],"learning":[]}`,
	} {
		requireLoginResponse(t, api.putLanguages(ana.AccessToken, body), http.StatusBadRequest, invalidRequest)
	}

	requireLanguages(t, api.getLanguages(ben.AccessToken), bens)
	requireLanguages(t, api.getLanguages(ana.AccessToken), anas)
	if got, want := api.storedLanguages(t, "ben@example.com"), []string{"spoken 0 de 7", "learning 0 es 3"}; !slices.Equal(got, want) {
		t.Errorf("ben's rows = %v, want %v", got, want)
	}
}

func TestLanguagesRejectMissingOrUnusableCredentials(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	revoked := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	requireLoggedOut(t, api.logout(revoked.AccessToken))
	expired := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	api.exec(t, `UPDATE sessions SET access_expires_at = now() - interval '1 second' WHERE access_token_hash = $1`,
		auth.HashToken(expired.AccessToken))

	for name, tc := range map[string]struct {
		query  string
		header []string
	}{
		"no header":            {},
		"empty bearer":         {header: []string{"Bearer "}},
		"basic scheme":         {header: []string{"Basic " + tokens.AccessToken}},
		"token without bearer": {header: []string{tokens.AccessToken}},
		"refresh token":        {header: []string{"Bearer " + tokens.RefreshToken}},
		"garbage":              {header: []string{"Bearer " + auth.AccessTokenPrefix + "garbage"}},
		"unknown":              {header: []string{"Bearer " + auth.NewToken(auth.AccessTokenPrefix).Raw}},
		"revoked":              {header: []string{"Bearer " + revoked.AccessToken}},
		"expired":              {header: []string{"Bearer " + expired.AccessToken}},
		"two headers":          {header: []string{"Bearer " + tokens.AccessToken, "Bearer " + tokens.AccessToken}},
		"token only in query":  {query: "?access_token=" + tokens.AccessToken},
	} {
		t.Run(name, func(t *testing.T) {
			requireUnauthorized(t, api.profileWith(http.MethodGet, catalogPath+tc.query, "", tc.header...))
			requireUnauthorized(t, api.profileWith(http.MethodGet, myLanguagesPath+tc.query, "", tc.header...))
			requireUnauthorized(t, api.profileWith(http.MethodPut, myLanguagesPath+tc.query, anasLanguages, tc.header...))
			// The body is not looked at: junk gets the same answer as a valid one.
			requireUnauthorized(t, api.profileWith(http.MethodPut, myLanguagesPath+tc.query, `not json`, tc.header...))
		})
	}
	if n := api.languageCount(t); n != 0 {
		t.Errorf("rows = %d after only unauthenticated requests, want 0", n)
	}
}

// A session that ends between two requests ends the access with it.
func TestLanguagesStopWithTheSession(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	requireLanguages(t, api.putLanguages(tokens.AccessToken, anasLanguages), anasLanguages)
	requireLoggedOut(t, api.logout(tokens.AccessToken))

	requireUnauthorized(t, api.getCatalog(tokens.AccessToken))
	requireUnauthorized(t, api.getLanguages(tokens.AccessToken))
	requireUnauthorized(t, api.putLanguages(tokens.AccessToken, noLanguages))
	if n := api.languageCount(t); n != 3 {
		t.Errorf("rows = %d, want the 3 saved before logging out", n)
	}
}

// ---- Malformed and invalid requests ----

func TestPutLanguagesRejectsMalformedRequests(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	requireLanguages(t, api.putLanguages(tokens.AccessToken, anasLanguages), anasLanguages)
	es := entriesJSON("es", "native")

	for name, body := range map[string]string{
		"empty body":            ``,
		"whitespace":            `   `,
		"not JSON":              `spoken=es`,
		"truncated":             `{"spoken":` + es,
		"null":                  `null`,
		"array":                 `[` + noLanguages + `]`,
		"string":                `"es"`,
		"number":                `5`,
		"list as object":        `{"spoken":{"language":"es","level":"native"}}`,
		"list as string":        `{"spoken":"es"}`,
		"list as number":        `{"learning":3}`,
		"item as string":        `{"spoken":["es"]}`,
		"item as number":        `{"spoken":[1]}`,
		"item as array":         `{"spoken":[["es","native"]]}`,
		"number for a language": `{"spoken":[{"language":5,"level":"native"}]}`,
		"object for a language": `{"spoken":[{"language":{"code":"es"},"level":"native"}]}`,
		"number for a level":    `{"spoken":[{"language":"es","level":7}]}`,
		"boolean for a level":   `{"spoken":[{"language":"es","level":true}]}`,
		"unknown field":         `{"spoken":` + es + `,"native":["es"]}`,
		"unknown item field":    `{"spoken":[{"language":"es","level":"native","name":"Spanish"}]}`,
		"kind in an item":       `{"spoken":[{"language":"es","level":"native","kind":"learning"}]}`,
		"position in an item":   `{"spoken":[{"language":"es","level":"native","position":3}]}`,
		"code instead":          `{"spoken":[{"code":"es","level":"native"}]}`,
		"created_at":            `{"spoken":` + es + `,"created_at":"2020-01-01T00:00:00Z"}`,
		"trailing data":         noLanguages + noLanguages,
		"trailing garbage":      noLanguages + `x`,
		"deeply nested":         `{"spoken":` + strings.Repeat("[", 4000) + strings.Repeat("]", 4000) + `}`,
		"oversized":             `{"spoken":[{"language":"es","level":"` + strings.Repeat("a", maxLanguagesBodyBytes) + `"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			requireLoginResponse(t, api.putLanguages(tokens.AccessToken, body), http.StatusBadRequest, invalidRequest)
		})
	}
	// Nothing a refused request held was applied: not even a clearing.
	requireLanguages(t, api.getLanguages(tokens.AccessToken), anasLanguages)
}

func TestPutLanguagesValidation(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	requireLanguages(t, api.putLanguages(tokens.AccessToken, anasLanguages), anasLanguages)
	spoken := func(language, level string) string { return languagesJSON(entriesJSON(language, level), "[]") }
	learning := func(language, level string) string { return languagesJSON("[]", entriesJSON(language, level)) }
	six := entriesJSON("es", "a1", "en", "a1", "fr", "a1", "de", "a1", "it", "a1", "pt", "a1")

	for name, tc := range map[string]struct {
		body string
		want []string
	}{
		"not in the catalog":          {spoken("xx", "a1"), []string{"spoken", "unknown_language"}},
		"three letters, not in it":    {learning("zzz", "a1"), []string{"learning", "unknown_language"}},
		"unknown among known":         {languagesJSON(entriesJSON("es", "a1", "xx", "a1", "en", "a1"), "[]"), []string{"spoken", "unknown_language"}},
		"unknown in both lists":       {languagesJSON(entriesJSON("xx", "a1"), entriesJSON("qq", "a1")), []string{"spoken", "unknown_language", "learning", "unknown_language"}},
		"upper case code":             {spoken("ES", "a1"), []string{"spoken", "unknown_language"}},
		"padded code":                 {spoken(" es", "a1"), []string{"spoken", "unknown_language"}},
		"empty code":                  {spoken("", "a1"), []string{"spoken", "unknown_language"}},
		"a name, not a code":          {spoken("spanish", "a1"), []string{"spoken", "unknown_language"}},
		"regional variant":            {spoken("pt-BR", "a1"), []string{"spoken", "unknown_language"}},
		"one letter":                  {spoken("e", "a1"), []string{"spoken", "unknown_language"}},
		"non-ASCII lookalike":         {spoken("еs", "a1"), []string{"spoken", "unknown_language"}},
		"SQL in a code":               {spoken(`es'; DROP TABLE user_languages; --`, "a1"), []string{"spoken", "unknown_language"}},
		"wildcard code":               {spoken("%", "a1"), []string{"spoken", "unknown_language"}},
		"control character in a code": {`{"spoken":[{"language":"es\u0000","level":"a1"}]}`, []string{"spoken", "unknown_language"}},
		"no language":                 {`{"spoken":[{"level":"a1"}]}`, []string{"spoken", "unknown_language"}},
		"null language":               {`{"spoken":[{"language":null,"level":"a1"}]}`, []string{"spoken", "unknown_language"}},
		"unknown level":               {spoken("es", "fluent"), []string{"spoken", "invalid_level"}},
		"upper case level":            {spoken("es", "C1"), []string{"spoken", "invalid_level"}},
		"padded level":                {spoken("es", "c1 "), []string{"spoken", "invalid_level"}},
		"empty level":                 {spoken("es", ""), []string{"spoken", "invalid_level"}},
		"level off the scale":         {learning("es", "c3"), []string{"learning", "invalid_level"}},
		"numeric level as text":       {spoken("es", "7"), []string{"spoken", "invalid_level"}},
		"no level":                    {`{"learning":[{"language":"es"}]}`, []string{"learning", "invalid_level"}},
		"null level":                  {`{"learning":[{"language":"es","level":null}]}`, []string{"learning", "invalid_level"}},
		"native while learning":       {learning("es", "native"), []string{"learning", "invalid_level"}},
		"empty item":                  {`{"spoken":[{}]}`, []string{"spoken", "unknown_language", "spoken", "invalid_level"}},
		"null item":                   {`{"learning":[null]}`, []string{"learning", "unknown_language", "learning", "invalid_level"}},
		"six spoken":                  {languagesJSON(six, "[]"), []string{"spoken", "too_many"}},
		"six learning":                {languagesJSON("[]", six), []string{"learning", "too_many"}},
		"hundreds of items":           {`{"spoken":[` + strings.Repeat(`{},`, 2000) + `{}]}`, []string{"spoken", "too_many", "spoken", "unknown_language", "spoken", "invalid_level"}},
		"twice in a list":             {languagesJSON(entriesJSON("es", "a1", "es", "b1"), "[]"), []string{"spoken", "duplicate"}},
		"in both lists":               {languagesJSON(entriesJSON("es", "c1"), entriesJSON("es", "c1")), []string{"learning", "duplicate"}},
		"everything at once": {languagesJSON(six[:len(six)-1]+`,{"language":"es","level":"x"},{"language":"ES","level":"a1"}]`, entriesJSON("es", "native")),
			[]string{"spoken", "too_many", "spoken", "unknown_language", "spoken", "invalid_level", "spoken", "duplicate",
				"learning", "invalid_level", "learning", "duplicate"}},
	} {
		t.Run(name, func(t *testing.T) {
			requireLoginResponse(t, api.putLanguages(tokens.AccessToken, tc.body), http.StatusUnprocessableEntity,
				invalidLanguages(tc.want...))
		})
	}
	// An invalid save writes nothing, whatever part of it was valid.
	requireLanguages(t, api.getLanguages(tokens.AccessToken), anasLanguages)
	if n := api.count(t, "user_languages"); n != 3 {
		t.Errorf("rows = %d, want 3", n)
	}
}

// The body limit itself: a body of exactly the limit is read and saved; one
// byte more is refused unread. The longest valid selection is far below it.
func TestPutLanguagesBodyLimit(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	body := func(size int) string {
		open := anasLanguages[:len(anasLanguages)-1]
		return open + strings.Repeat(" ", size-len(anasLanguages)) + "}"
	}

	atLimit := body(maxLanguagesBodyBytes)
	if len(atLimit) != maxLanguagesBodyBytes {
		t.Fatalf("body is %d bytes", len(atLimit))
	}
	requireLanguages(t, api.putLanguages(tokens.AccessToken, atLimit), anasLanguages)
	requireLanguages(t, api.putLanguages(tokens.AccessToken, noLanguages), noLanguages)

	requireLoginResponse(t, api.putLanguages(tokens.AccessToken, body(maxLanguagesBodyBytes+1)), http.StatusBadRequest, invalidRequest)
	requireLoginResponse(t, api.putLanguages(tokens.AccessToken, body(100*maxLanguagesBodyBytes)), http.StatusBadRequest, invalidRequest)
	if n := api.languageCount(t); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

// A body on a GET is never read, whatever it holds.
func TestGetLanguagesIgnoresABody(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	requireLanguages(t, api.putLanguages(tokens.AccessToken, anasLanguages), anasLanguages)

	junk := strings.Repeat("x", 10*maxLanguagesBodyBytes)
	requireLanguages(t, api.profileWith(http.MethodGet, myLanguagesPath, junk, "Bearer "+tokens.AccessToken), anasLanguages)
	requireLanguages(t, api.profileWith(http.MethodGet, myLanguagesPath, noLanguages, "Bearer "+tokens.AccessToken), anasLanguages)
	if rec := api.profileWith(http.MethodGet, catalogPath, junk, "Bearer "+tokens.AccessToken); rec.Code != http.StatusOK {
		t.Errorf("catalog with a body: status %d", rec.Code)
	}
}

func TestLanguageRoutesAllowOnlyTheirMethods(t *testing.T) {
	// Nil services: the mux must answer before authentication runs.
	h := New(slog.New(slog.DiscardHandler), nil, nil, nil, Options{})
	for path, methods := range map[string][]string{
		myLanguagesPath: {http.MethodPost, http.MethodPatch, http.MethodDelete},
		catalogPath:     {http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete},
	} {
		for _, method := range methods {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(anasLanguages)))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: status = %d, want 405", method, path, rec.Code)
			}
		}
	}
	// No route takes a user or a language in its path.
	for _, path := range []string{myLanguagesPath + "/es", catalogPath + "/es", "/v1/users/x/languages"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, rec.Code)
		}
	}
}

// ---- Fail-closed, deleted users and failures ----

// Mounted without requireAccessToken by mistake, the handlers fail closed.
func TestLanguageHandlersWithoutMiddlewareFailClosed(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	for name, tc := range map[string]struct {
		method string
		h      http.Handler
	}{
		"catalog": {http.MethodGet, handleLanguageCatalog(logger, nil)},
		"get":     {http.MethodGet, handleGetLanguages(logger, nil)},
		"put":     {http.MethodPut, handlePutLanguages(logger, nil)},
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.h.ServeHTTP(rec, httptest.NewRequest(tc.method, myLanguagesPath, strings.NewReader(anasLanguages)))
			requireResponse(t, rec, http.StatusInternalServerError, internalError)
		})
	}
}

// A deleted user's sessions are deleted with them, so every route answers
// the 401 of a dead credential and nothing of theirs is left.
func TestLanguagesOfADeletedUser(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	requireLanguages(t, api.putLanguages(tokens.AccessToken, anasLanguages), anasLanguages)
	api.exec(t, `DELETE FROM users WHERE email = $1`, "ana@example.com")

	requireUnauthorized(t, api.getCatalog(tokens.AccessToken))
	requireUnauthorized(t, api.getLanguages(tokens.AccessToken))
	requireUnauthorized(t, api.putLanguages(tokens.AccessToken, anasLanguages))
	if n := api.languageCount(t); n != 0 {
		t.Errorf("rows = %d, want 0: they go with the user", n)
	}
}

// A user deleted after authentication but before the write: the answer is
// the 401 of a dead credential (016), and nothing is written.
func TestPutLanguagesForAUserDeletedMeanwhile(t *testing.T) {
	api := newTestAPI(t)
	api.loggedIn(t, "ana@example.com")
	id := api.userID(t, "ana@example.com")
	api.exec(t, `DELETE FROM users WHERE id = $1`, id)

	for _, body := range []string{anasLanguages, noLanguages} {
		rec := httptest.NewRecorder()
		req := as(httptest.NewRequest(http.MethodPut, myLanguagesPath, strings.NewReader(body)), id)
		handlePutLanguages(slog.New(slog.DiscardHandler), api.languageService()).ServeHTTP(rec, req)

		requireResponse(t, rec, http.StatusUnauthorized, invalidAccessToken)
		if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
			t.Errorf("WWW-Authenticate = %q, want Bearer", got)
		}
	}
	if n := api.languageCount(t); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

// A user deleted after authentication but before the read has no rows any
// more: the answer is the ordinary empty selection, which reveals nothing,
// and their next request gets the 401 of a dead session.
func TestGetLanguagesForAUserDeletedMeanwhile(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	requireLanguages(t, api.putLanguages(tokens.AccessToken, anasLanguages), anasLanguages)
	id := api.userID(t, "ana@example.com")
	api.exec(t, `DELETE FROM users WHERE id = $1`, id)

	rec := httptest.NewRecorder()
	handleGetLanguages(slog.New(slog.DiscardHandler), api.languageService()).
		ServeHTTP(rec, as(httptest.NewRequest(http.MethodGet, myLanguagesPath, nil), id))
	requireResponse(t, rec, http.StatusOK, noLanguages)

	requireUnauthorized(t, api.getLanguages(tokens.AccessToken))
}

func TestLanguagesInternalErrorIsOpaque(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	// Break every query by hiding a column each of them names.
	api.exec(t, `ALTER TABLE user_languages RENAME COLUMN level TO test_hidden`)
	api.exec(t, `ALTER TABLE languages RENAME COLUMN endonym TO test_hidden`)
	t.Cleanup(func() {
		_, _ = api.pool.Exec(context.Background(), `ALTER TABLE user_languages RENAME COLUMN test_hidden TO level`)
		_, _ = api.pool.Exec(context.Background(), `ALTER TABLE languages RENAME COLUMN test_hidden TO endonym`)
	})

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"catalog": api.getCatalog(tokens.AccessToken),
		"get":     api.getLanguages(tokens.AccessToken),
		"put":     api.putLanguages(tokens.AccessToken, languagesJSON(entriesJSON("yue", "native"), "[]")),
	} {
		t.Run(name, func(t *testing.T) {
			requireLoginResponse(t, rec, http.StatusInternalServerError, internalError)
			if len(rec.Header()["Retry-After"]) != 0 || len(rec.Header()["Www-Authenticate"]) != 0 {
				t.Errorf("headers = %v", rec.Header())
			}
		})
	}

	api.svc.Wait()
	logs := api.logs.String()
	for _, route := range []string{"GET " + catalogPath, "GET " + myLanguagesPath, "PUT " + myLanguagesPath} {
		if !strings.Contains(logs, `route="`+route+`"`) {
			t.Errorf("the failure of %s was not logged with its route: %s", route, logs)
		}
	}
	if n := strings.Count(logs, "request failed"); n != 3 {
		t.Errorf("%d failures logged, want 3", n)
	}
	if strings.Contains(logs, "yue") || strings.Contains(logs, "native") || strings.Contains(logs, tokens.AccessToken) {
		t.Errorf("logs contain a language, a level or the token: %s", logs)
	}
}

// A save that can't finish before the request deadline (here it waits for a
// transaction holding the user's row) answers 503 with Retry-After and writes
// nothing; the retry it invites then succeeds.
func TestPutLanguagesPastTheDeadlineIsUnavailable(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	id := api.userID(t, "ana@example.com")
	ctx := context.Background()

	tx, err := api.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}

	h := requestDeadline(200 * time.Millisecond)(handlePutLanguages(slog.New(slog.DiscardHandler), api.languageService()))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, as(httptest.NewRequest(http.MethodPut, myLanguagesPath, strings.NewReader(anasLanguages)), id))
	requireLoginResponse(t, rec, http.StatusServiceUnavailable, serviceUnavailable)
	if got := rec.Header().Get("Retry-After"); got == "" || got == "0" {
		t.Errorf("Retry-After = %q", got)
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if n := api.languageCount(t); n != 0 {
		t.Errorf("rows = %d after a save that timed out waiting, want 0", n)
	}
	requireLanguages(t, api.putLanguages(tokens.AccessToken, anasLanguages), anasLanguages)
}

// A request whose time is already up does no work at all.
func TestLanguagesWithAnExpiredDeadlineAreUnavailable(t *testing.T) {
	api := newTestAPI(t)
	api.loggedIn(t, "ana@example.com")
	id := api.userID(t, "ana@example.com")
	logger := slog.New(slog.DiscardHandler)
	svc := api.languageService()

	for name, tc := range map[string]struct {
		method string
		h      http.Handler
	}{
		"catalog": {http.MethodGet, handleLanguageCatalog(logger, svc)},
		"get":     {http.MethodGet, handleGetLanguages(logger, svc)},
		"put":     {http.MethodPut, handlePutLanguages(logger, svc)},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel()
			req := as(httptest.NewRequest(tc.method, myLanguagesPath, strings.NewReader(anasLanguages)).WithContext(ctx), id)
			rec := httptest.NewRecorder()
			tc.h.ServeHTTP(rec, req)
			requireLoginResponse(t, rec, http.StatusServiceUnavailable, serviceUnavailable)
		})
	}
	if n := api.languageCount(t); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

// ---- Concurrency ----

// Concurrent saves of one member, with reads in between: every save succeeds
// and answers its own selection, every read sees one request's whole
// selection (or none yet), and what is left is one request's, never a mix.
func TestConcurrentPutLanguagesLeaveOneWholeSelection(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	// Every body shares languages with the others, at other levels, in
	// other positions and in the other list.
	codes := []string{"es", "en", "fr", "de", "it", "pt", "ja", "ko"}
	levels := []string{"a1", "a2", "b1", "b2", "c1", "c2"}
	const n = 16
	bodies := make([]string, n)
	for i := range bodies {
		code := func(k int) string { return codes[(i+k)%len(codes)] }
		level := levels[i%len(levels)]
		three := entriesJSON(code(0), level, code(1), level, code(2), level)
		two := entriesJSON(code(3), level, code(4), level)
		if i%2 == 1 {
			three, two = two, three
		}
		bodies[i] = languagesJSON(three, two)
	}

	puts := make([]*httptest.ResponseRecorder, n)
	gets := make([]*httptest.ResponseRecorder, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			puts[i] = api.putLanguages(tokens.AccessToken, bodies[i])
		})
		wg.Go(func() {
			<-start
			gets[i] = api.getLanguages(tokens.AccessToken)
		})
	}
	close(start)
	wg.Wait()

	for i := range n {
		requireLanguages(t, puts[i], bodies[i])
		if gets[i].Code != http.StatusOK {
			t.Errorf("read %d: status %d", i, gets[i].Code)
		}
		if got := strings.TrimSuffix(gets[i].Body.String(), "\n"); got != noLanguages && !slices.Contains(bodies, got) {
			t.Errorf("read %d saw a selection no request sent: %s", i, got)
		}
	}
	final := strings.TrimSuffix(api.getLanguages(tokens.AccessToken).Body.String(), "\n")
	if !slices.Contains(bodies, final) {
		t.Errorf("the selection is not the one of any request: %s", final)
	}
	if rows := api.languageCount(t); rows != 5 {
		t.Errorf("rows = %d, want the 5 of one request", rows)
	}
}

// Several identical first saves at once (a client retrying while its first
// request is still running), from two sessions: all succeed with the same
// answer, written once.
func TestConcurrentIdenticalPutLanguagesAllSucceed(t *testing.T) {
	api := newTestAPI(t)
	first := api.loggedIn(t, "ana@example.com")
	second := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))

	const n = 16
	recs := make([]*httptest.ResponseRecorder, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		token := first.AccessToken
		if i%2 == 1 {
			token = second.AccessToken
		}
		wg.Go(func() {
			<-start
			recs[i] = api.putLanguages(token, anasLanguages)
		})
	}
	close(start)
	wg.Wait()

	for _, rec := range recs {
		requireLanguages(t, rec, anasLanguages)
	}
	if rows := api.languageCount(t); rows != 3 {
		t.Errorf("rows = %d, want 3", rows)
	}
	api.svc.Wait()
	if saves := strings.Count(api.logs.String(), "languages: saved"); saves != 1 {
		t.Errorf("%d saves logged, want 1", saves)
	}
}

// Different members saving at once don't wait for or affect each other.
func TestConcurrentPutLanguagesOfDifferentMembers(t *testing.T) {
	api := newTestAPI(t)
	addrs := []string{"ana@example.com", "ben@example.com", "cai@example.com", "dee@example.com"}
	codes := []string{"es", "en", "fr", "de"}
	tokens := make([]loginTokens, len(addrs))
	bodies := make([]string, len(addrs))
	for i, addr := range addrs {
		tokens[i] = api.loggedIn(t, addr)
		bodies[i] = languagesJSON(entriesJSON(codes[i], "native"), entriesJSON(codes[(i+1)%len(codes)], "a1"))
	}

	recs := make([]*httptest.ResponseRecorder, len(addrs))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range addrs {
		wg.Go(func() {
			<-start
			recs[i] = api.putLanguages(tokens[i].AccessToken, bodies[i])
		})
	}
	close(start)
	wg.Wait()

	for i := range addrs {
		requireLanguages(t, recs[i], bodies[i])
		requireLanguages(t, api.getLanguages(tokens[i].AccessToken), bodies[i])
	}
}

// ---- Information leaks ----

// What a member chooses never reaches the logs, on success or on rejection,
// and no response says more than a code.
func TestLanguagesAreNeverLogged(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	body := languagesJSON(entriesJSON("yue", "native"), entriesJSON("tr", "b2"))

	requireLanguages(t, api.putLanguages(tokens.AccessToken, body), body)
	requireLanguages(t, api.getLanguages(tokens.AccessToken), body)
	rejected := []*httptest.ResponseRecorder{
		api.putLanguages(tokens.AccessToken, languagesJSON(entriesJSON("MARKERCODE", "MARKERLEVEL"), entriesJSON("yue", "native"))),
		api.putLanguages(tokens.AccessToken, `{"spoken":[],"marker_field":"MARKERVALUE"}`),
		api.putLanguages(tokens.AccessToken, `{"spoken":[{"language":"yue","level":"native"`),
	}
	for _, rec := range rejected {
		if got := rec.Body.String(); strings.Contains(strings.ToLower(got), "marker") || strings.Contains(got, "yue") ||
			strings.Contains(got, "json") || strings.Contains(got, "unexpected") {
			t.Errorf("a rejection echoes the request or the parser: %s", got)
		}
	}

	api.svc.Wait()
	logs := api.logs.String()
	if n := strings.Count(logs, "languages: saved"); n != 1 {
		t.Errorf("%d saves logged, want 1: %s", n, logs)
	}
	if !strings.Contains(logs, "user_id="+api.userID(t, "ana@example.com")) {
		t.Errorf("the save does not name the user: %s", logs)
	}
	// Not the CEFR levels: "b2" is also two digits of any UUID.
	for _, secret := range []string{"yue", "native", "MARKER", "marker_field", tokens.AccessToken, "ana@example.com"} {
		if strings.Contains(logs, secret) {
			t.Errorf("logs contain %q: %s", secret, logs)
		}
	}
}

// ---- Per-user write limit ----

func TestPutLanguagesIsLimitedPerUser(t *testing.T) {
	api := newTestAPIWith(t, Options{UserLimits: UserLimits{LanguagesWrite: tightUserLimiter()}})
	ana := api.loggedIn(t, "ana@example.com")
	ben := api.loggedIn(t, "ben@example.com")
	other := languagesJSON(entriesJSON("fr", "native"), "[]")

	// Requests that don't authenticate never spend anyone's allowance.
	requireUnauthorized(t, api.profileWith(http.MethodPut, myLanguagesPath, anasLanguages))
	requireUnauthorized(t, api.putLanguages(auth.NewToken(auth.AccessTokenPrefix).Raw, anasLanguages))

	requireLanguages(t, api.putLanguages(ana.AccessToken, anasLanguages), anasLanguages)
	rec := api.putLanguages(ana.AccessToken, other)
	requireLoginResponse(t, rec, http.StatusTooManyRequests, rateLimited)
	if got := rec.Header().Get("Retry-After"); got == "" || got == "0" {
		t.Errorf("Retry-After = %q", got)
	}
	// A 429 did nothing, and it is the answer whatever the body is.
	requireLoginResponse(t, api.putLanguages(ana.AccessToken, `not json`), http.StatusTooManyRequests, rateLimited)
	requireLoginResponse(t, api.putLanguages(ana.AccessToken, languagesJSON(entriesJSON("xx", "a1"), "[]")),
		http.StatusTooManyRequests, rateLimited)
	requireLanguages(t, api.getLanguages(ana.AccessToken), anasLanguages)

	// The limit is the user's: a second session of Ana's shares it, Ben has his own.
	second := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	requireLoginResponse(t, api.putLanguages(second.AccessToken, other), http.StatusTooManyRequests, rateLimited)
	requireLanguages(t, api.putLanguages(ben.AccessToken, other), other)

	// Reading is not limited, and saving a profile has its own allowance.
	for range 5 {
		requireLanguages(t, api.getLanguages(ana.AccessToken), anasLanguages)
		if rec := api.getCatalog(ana.AccessToken); rec.Code != http.StatusOK {
			t.Fatalf("catalog: status %d", rec.Code)
		}
	}
	requireProfile(t, api.putProfile(ana.AccessToken, profileJSON(t, "Ana", "")))
}

// Junk and invalid selections cost a token too: the limit is checked before
// the body is read.
func TestPutLanguagesLimitCountsRejectedRequests(t *testing.T) {
	for name, tc := range map[string]struct {
		body   string
		status int
		want   string
	}{
		"malformed": {`not json`, http.StatusBadRequest, invalidRequest},
		"oversized": {strings.Repeat(" ", maxLanguagesBodyBytes+1), http.StatusBadRequest, invalidRequest},
		"invalid":   {languagesJSON(entriesJSON("xx", "a1"), "[]"), http.StatusUnprocessableEntity, invalidLanguages("spoken", "unknown_language")},
		"unchanged": {noLanguages, http.StatusOK, noLanguages},
	} {
		t.Run(name, func(t *testing.T) {
			api := newTestAPIWith(t, Options{UserLimits: UserLimits{LanguagesWrite: tightUserLimiter()}})
			ana := api.loggedIn(t, "ana@example.com")

			requireLoginResponse(t, api.putLanguages(ana.AccessToken, tc.body), tc.status, tc.want)
			requireLoginResponse(t, api.putLanguages(ana.AccessToken, anasLanguages), http.StatusTooManyRequests, rateLimited)
		})
	}
}

// The two write limits are separate buckets: spending one leaves the other.
func TestLanguageAndProfileWriteLimitsAreSeparate(t *testing.T) {
	api := newTestAPIWith(t, Options{UserLimits: UserLimits{ProfileWrite: tightUserLimiter(), LanguagesWrite: tightUserLimiter()}})
	ana := api.loggedIn(t, "ana@example.com")

	requireProfile(t, api.putProfile(ana.AccessToken, profileJSON(t, "Ana", "")))
	requireLoginResponse(t, api.putProfile(ana.AccessToken, profileJSON(t, "Ana", "")), http.StatusTooManyRequests, rateLimited)
	requireLanguages(t, api.putLanguages(ana.AccessToken, anasLanguages), anasLanguages)
	requireLoginResponse(t, api.putLanguages(ana.AccessToken, anasLanguages), http.StatusTooManyRequests, rateLimited)
}

func TestNewUserLimitsForLanguagesMatchTheDecisionLog(t *testing.T) {
	l := NewUserLimits(slog.New(slog.DiscardHandler))
	const burst = 10
	for i := range burst {
		if ok, _ := l.LanguagesWrite.Allow("user"); !ok {
			t.Fatalf("denied at %d, burst is %d", i+1, burst)
		}
	}
	ok, retryAfter := l.LanguagesWrite.Allow("user")
	if ok {
		t.Errorf("allowed past burst %d", burst)
	}
	if retryAfter <= 0 || retryAfter > 6*time.Second {
		t.Errorf("retry after %v, want within the 6 s refill", retryAfter)
	}
	// Its own bucket, and one per user.
	if ok, _ := l.ProfileWrite.Allow("user"); !ok {
		t.Error("spending the languages limit spent the profile's")
	}
	if ok, _ := l.LanguagesWrite.Allow("another user"); !ok {
		t.Error("one user's limit refused another")
	}
}
