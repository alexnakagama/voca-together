package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"vocatogether/backend/internal/auth"
	"vocatogether/backend/internal/email"
	"vocatogether/backend/internal/googleid"
	"vocatogether/backend/internal/language"
	"vocatogether/backend/internal/profile"
	"vocatogether/backend/internal/ratelimit"
	"vocatogether/backend/internal/testutil"
)

const (
	googlePath          = "/v1/auth/google"
	googleUserAgent     = "VocaTogether/1.0 (Android 15)"
	googleSubject       = "109876543210987654321"
	invalidGoogleToken  = `{"error":{"code":"invalid_google_token"}}`
	googleEmailUnusable = `{"error":{"code":"google_email_unusable"}}`
	accountExists       = `{"error":{"code":"account_exists"}}`
	idTokenRequired     = `{"error":{"code":"validation_failed","fields":[{"field":"id_token","code":"required"}]}}`
)

// countingVerifier is a googleid.Fake that counts its calls and keeps the
// context of the last one. Tests run sequentially, so no locking.
type countingVerifier struct {
	googleid.Fake
	calls   int
	lastCtx context.Context
}

func (v *countingVerifier) Verify(ctx context.Context, raw string) (googleid.Claims, error) {
	v.calls++
	v.lastCtx = ctx
	return v.Fake.Verify(ctx, raw)
}

type googleAPI struct {
	testAPI
	verifier *countingVerifier
}

// newGoogleTestAPI is newTestAPI with a fake Google verifier and the given
// per-account limits.
func newGoogleTestAPI(t *testing.T, limits auth.AccountLimits) googleAPI {
	t.Helper()
	pool := testutil.DB(t)
	rec := &email.Recorder{}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	base, _ := url.Parse("https://api.example.com")
	verifier := &countingVerifier{Fake: googleid.Fake{Tokens: map[string]googleid.Claims{}}}
	svc := auth.NewService(pool, rec, base, logger, limits, verifier)
	t.Cleanup(svc.Wait)
	return googleAPI{
		testAPI:  testAPI{handler: New(logger, svc, profile.NewService(pool, logger), language.NewService(pool, logger), Options{}), svc: svc, pool: pool, emails: rec, logs: logs},
		verifier: verifier,
	}
}

// issue returns a new raw stand-in for an ID token that verifies to the
// given claims.
func (a googleAPI) issue(subject, addr string, verified bool) string {
	raw := "eyJhbGciOiJSUzI1NiJ9.http-test." + auth.NewToken("").Raw
	a.verifier.Tokens[raw] = googleid.NewClaims(subject, addr, verified, "",
		time.Unix(time.Now().Add(time.Hour).Unix(), 0))
	return raw
}

func googleBody(idToken string) string {
	return `{"id_token":"` + idToken + `"}`
}

// google posts body to the Google sign-in endpoint with a fixed user agent;
// edit can change the request before it is sent.
func (a testAPI) google(body string, edit ...func(*http.Request)) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, googlePath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", googleUserAgent)
	for _, e := range edit {
		e(req)
	}
	a.handler.ServeHTTP(rec, req)
	return rec
}

func (a testAPI) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// requireGoogleCredentials checks a successful sign-in response and returns
// its tokens.
func requireGoogleCredentials(t *testing.T, rec *httptest.ResponseRecorder) loginTokens {
	t.Helper()
	got := decodeTokens(t, rec)
	if got.TokenType != "Bearer" || got.ExpiresIn != 900 ||
		!strings.HasPrefix(got.AccessToken, auth.AccessTokenPrefix) ||
		!strings.HasPrefix(got.RefreshToken, auth.RefreshTokenPrefix) {
		t.Errorf("credentials = %+v", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	return got
}

func (a testAPI) meID(t *testing.T, accessToken string) string {
	t.Helper()
	rec := a.me(accessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: status %d", rec.Code)
	}
	var got meBody
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got.ID
}

// A new identity gets an account and a session; the same identity signs in
// to the same account again. Each request calls the service once and stores
// the request's user agent with its session.
func TestGoogleSignInEndpoint(t *testing.T) {
	api := newGoogleTestAPI(t, auth.AccountLimits{})

	first := requireGoogleCredentials(t, api.google(googleBody(api.issue(googleSubject, "ana@example.com", true))))
	if api.verifier.calls != 1 {
		t.Errorf("verifier calls = %d, want 1", api.verifier.calls)
	}
	again := requireGoogleCredentials(t, api.google(googleBody(api.issue(googleSubject, "ana@example.com", true))))
	if api.verifier.calls != 2 {
		t.Errorf("verifier calls = %d, want 2", api.verifier.calls)
	}

	if a, b := api.meID(t, first.AccessToken), api.meID(t, again.AccessToken); a != b {
		t.Errorf("second sign-in reached user %s, want %s", b, a)
	}
	if first.AccessToken == again.AccessToken || first.RefreshToken == again.RefreshToken {
		t.Error("sign-ins share credentials")
	}
	for table, want := range map[string]int{"users": 1, "user_identities": 1, "sessions": 2} {
		if n := api.count(t, table); n != want {
			t.Errorf("%s = %d, want %d", table, n, want)
		}
	}
	var n int
	if err := api.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sessions WHERE user_agent = $1`, googleUserAgent).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("sessions with the request's user agent = %d, want 2", n)
	}
}

func TestGoogleSignInEndpointValidationError(t *testing.T) {
	api := newGoogleTestAPI(t, auth.AccountLimits{})
	for name, body := range map[string]string{
		"missing": `{}`,
		"null":    `{"id_token":null}`,
		"empty":   googleBody(""),
	} {
		t.Run(name, func(t *testing.T) {
			requireLoginResponse(t, api.google(body), http.StatusUnprocessableEntity, idTokenRequired)
		})
	}
	if api.verifier.calls != 0 {
		t.Errorf("verifier calls = %d, want 0", api.verifier.calls)
	}
}

// Malformed requests are rejected before the service: nothing is verified,
// and the token still works in a well-formed request.
func TestGoogleSignInEndpointRejectsMalformedRequests(t *testing.T) {
	api := newGoogleTestAPI(t, auth.AccountLimits{})
	token := api.issue(googleSubject, "ana@example.com", true)
	valid := googleBody(token)

	tests := malformedBodies("id_token")
	tests["unknown field with a valid token"] = `{"id_token":"` + token + `","nonce":"x"}`
	tests["trailing JSON after a valid token"] = valid + `{}`
	tests["two requests"] = valid + "\n" + valid
	tests["other endpoint's field"] = `{"refresh_token":"` + token + `"}`
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			requireLoginResponse(t, api.google(body), http.StatusBadRequest, invalidRequest)
		})
	}
	if api.verifier.calls != 0 {
		t.Errorf("verifier calls = %d, want 0", api.verifier.calls)
	}
	requireGoogleCredentials(t, api.google(valid))
}

// Google returns the same ID token to an app again while it is valid, so a
// token that verifies is accepted every time it is presented (decision 026):
// each request is a 200 with its own session for the same account.
func TestGoogleSignInEndpointSameTokenTwice(t *testing.T) {
	api := newGoogleTestAPI(t, auth.AccountLimits{})
	body := googleBody(api.issue(googleSubject, "ana@example.com", true))

	first := requireGoogleCredentials(t, api.google(body))
	second := requireGoogleCredentials(t, api.google(body))

	if a, b := api.meID(t, first.AccessToken), api.meID(t, second.AccessToken); a != b {
		t.Errorf("second sign-in reached user %s, want %s", b, a)
	}
	if first.AccessToken == second.AccessToken || first.RefreshToken == second.RefreshToken {
		t.Error("sign-ins share credentials")
	}
	for table, want := range map[string]int{"users": 1, "user_identities": 1, "sessions": 2} {
		if n := api.count(t, table); n != want {
			t.Errorf("%s = %d, want %d", table, n, want)
		}
	}
}

// The same token in several requests at once: all succeed, with one account
// and one identity between them.
func TestGoogleSignInEndpointSameTokenConcurrently(t *testing.T) {
	api := newGoogleTestAPI(t, auth.AccountLimits{})
	body := googleBody(api.issue(googleSubject, "ana@example.com", true))

	const requests = 8
	codes := make([]int, requests)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			codes[i] = api.google(body).Code
		}()
	}
	close(start)
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("request %d: status %d, want 200", i, code)
		}
	}
	for table, want := range map[string]int{"users": 1, "user_identities": 1, "sessions": requests} {
		if n := api.count(t, table); n != want {
			t.Errorf("%s = %d, want %d", table, n, want)
		}
	}
}

// A refusal is the same every time the token is presented, and still links
// and changes nothing.
func TestGoogleSignInEndpointSameTokenAfterRefusal(t *testing.T) {
	api := newGoogleTestAPI(t, auth.AccountLimits{})
	api.verifiedAccount(t, "ana@example.com")
	taken := googleBody(api.issue(googleSubject, "ana@example.com", true))
	unverified := googleBody(api.issue("123456789012345678901", "bob@example.com", false))

	for range 2 {
		requireLoginResponse(t, api.google(taken), http.StatusConflict, accountExists)
		requireLoginResponse(t, api.google(unverified), http.StatusForbidden, googleEmailUnusable)
	}
	for table, want := range map[string]int{"users": 1, "user_identities": 0, "sessions": 0} {
		if n := api.count(t, table); n != want {
			t.Errorf("%s = %d, want %d", table, n, want)
		}
	}
}

// An unknown token and any token while Google sign-in isn't configured get
// the identical 401: no verifier reason reaches the client. No
// WWW-Authenticate: the ID token is a credential in the body, not an HTTP
// authentication scheme.
func TestGoogleSignInEndpointUnusableTokensAreIdentical(t *testing.T) {
	api := newGoogleTestAPI(t, auth.AccountLimits{})

	base, _ := url.Parse("https://api.example.com")
	discard := slog.New(slog.DiscardHandler)
	notConfigured := testAPI{handler: New(discard,
		auth.NewService(api.pool, &email.Recorder{}, base, discard, auth.AccountLimits{}, nil), nil, nil, Options{})}

	responses := map[string]*httptest.ResponseRecorder{
		"unknown":        api.google(googleBody("eyJhbGciOiJSUzI1NiJ9.unknown.token")),
		"not configured": notConfigured.google(googleBody(api.issue(googleSubject, "ana@example.com", true))),
	}
	var first *httptest.ResponseRecorder
	for name, rec := range responses {
		requireLoginResponse(t, rec, http.StatusUnauthorized, invalidGoogleToken)
		if got := rec.Header().Get("WWW-Authenticate"); got != "" {
			t.Errorf("%s: WWW-Authenticate = %q, want none", name, got)
		}
		if first == nil {
			first = rec
			continue
		}
		if fmt.Sprint(rec.Header()) != fmt.Sprint(first.Header()) {
			t.Errorf("%s: headers differ:\n%v\nvs\n%v", name, rec.Header(), first.Header())
		}
	}
	if n := api.count(t, "sessions"); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
}

// Unavailable Google keys are a 503 that did nothing: the same token works
// once the keys are back.
func TestGoogleSignInEndpointGoogleUnavailable(t *testing.T) {
	api := newGoogleTestAPI(t, auth.AccountLimits{})
	token := api.issue(googleSubject, "ana@example.com", true)
	api.verifier.Err = &googleid.UnavailableError{Reason: "fetch_failed"}

	rec := api.google(googleBody(token))
	requireLoginResponse(t, rec, http.StatusServiceUnavailable, serviceUnavailable)
	if got := rec.Header().Get("Retry-After"); got != "5" {
		t.Errorf("Retry-After = %q, want 5", got)
	}
	if n := api.count(t, "users"); n != 0 {
		t.Errorf("users = %d, want 0", n)
	}
	api.verifier.Err = nil
	requireGoogleCredentials(t, api.google(googleBody(token)))
}

func TestGoogleSignInEndpointEmailUnusable(t *testing.T) {
	api := newGoogleTestAPI(t, auth.AccountLimits{})
	requireLoginResponse(t, api.google(googleBody(api.issue(googleSubject, "ana@example.com", false))),
		http.StatusForbidden, googleEmailUnusable)
	if n := api.count(t, "users"); n != 0 {
		t.Errorf("users = %d, want 0", n)
	}
}

// An email that already has an account is refused; nothing is linked.
func TestGoogleSignInEndpointAccountExists(t *testing.T) {
	api := newGoogleTestAPI(t, auth.AccountLimits{})
	api.verifiedAccount(t, "ana@example.com")

	requireLoginResponse(t, api.google(googleBody(api.issue(googleSubject, "ana@example.com", true))),
		http.StatusConflict, accountExists)
	if n := api.count(t, "user_identities"); n != 0 {
		t.Errorf("identities = %d, want 0", n)
	}
	if n := api.count(t, "sessions"); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
}

// A spent per-subject limit is a 429 that creates no session.
func TestGoogleSignInEndpointSubjectRateLimited(t *testing.T) {
	limits := auth.AccountLimits{Login: ratelimit.New[[32]byte]("test", 1, time.Hour, 100, slog.New(slog.DiscardHandler))}
	api := newGoogleTestAPI(t, limits)
	requireGoogleCredentials(t, api.google(googleBody(api.issue(googleSubject, "ana@example.com", true))))

	requireRateLimitedResponse(t, api.google(googleBody(api.issue(googleSubject, "ana@example.com", true))))
	if n := api.count(t, "sessions"); n != 1 {
		t.Errorf("sessions = %d, want 1", n)
	}
}

// The deadline is a 503 and a client that went away an opaque 500, as on
// every endpoint.
func TestGoogleSignInEndpointContextErrors(t *testing.T) {
	api := newGoogleTestAPI(t, auth.AccountLimits{})
	api.verifier.Err = context.DeadlineExceeded
	requireLoginResponse(t, api.google(googleBody(api.issue(googleSubject, "ana@example.com", true))),
		http.StatusServiceUnavailable, serviceUnavailable)

	api.verifier.Err = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := api.google(googleBody(api.issue(googleSubject, "ana@example.com", true)), func(r *http.Request) {
		*r = *r.WithContext(ctx)
	})
	requireLoginResponse(t, rec, http.StatusInternalServerError, internalError)

	if api.verifier.calls != 2 {
		t.Errorf("verifier calls = %d, want 2 (no retries)", api.verifier.calls)
	}
	if n := api.count(t, "sessions"); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
}

// The service gets the request's own context, with the request deadline.
func TestGoogleSignInEndpointPassesRequestContext(t *testing.T) {
	api := newGoogleTestAPI(t, auth.AccountLimits{})
	type key struct{}
	start := time.Now()
	requireGoogleCredentials(t, api.google(googleBody(api.issue(googleSubject, "ana@example.com", true)),
		func(r *http.Request) { *r = *r.WithContext(context.WithValue(r.Context(), key{}, "marker")) }))

	ctx := api.verifier.lastCtx
	if ctx == nil || ctx.Value(key{}) != "marker" {
		t.Fatal("verifier didn't get the request's context")
	}
	deadline, ok := ctx.Deadline()
	if !ok || deadline.After(start.Add(requestTimeout+time.Second)) {
		t.Errorf("deadline = %v (set %v), want the request deadline", deadline, ok)
	}
}

// Only the body carries the ID token: one in the Authorization header, the
// query string or another header is ignored.
func TestGoogleSignInEndpointIgnoresTokenOutsideBody(t *testing.T) {
	api := newGoogleTestAPI(t, auth.AccountLimits{})
	token := api.issue(googleSubject, "ana@example.com", true)

	for name, edit := range map[string]func(*http.Request){
		"Authorization": func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) },
		"query": func(r *http.Request) {
			r.URL.RawQuery = url.Values{"id_token": {token}}.Encode()
		},
		"other header": func(r *http.Request) { r.Header.Set("X-Id-Token", token) },
	} {
		t.Run(name, func(t *testing.T) {
			requireLoginResponse(t, api.google(`{}`, edit), http.StatusUnprocessableEntity, idTokenRequired)
		})
	}
	if api.verifier.calls != 0 {
		t.Errorf("verifier calls = %d, want 0", api.verifier.calls)
	}
	requireGoogleCredentials(t, api.google(googleBody(token)))
}

// No outcome logs the ID token, its hash, the subject, the email, the user
// agent or the issued credentials. A 500 is logged with its route.
func TestGoogleSignInEndpointLogsNoSecrets(t *testing.T) {
	api := newGoogleTestAPI(t, auth.AccountLimits{})
	api.verifiedAccount(t, "bob@example.com")

	signedIn := api.issue(googleSubject, "ana@example.com", true)
	creds := requireGoogleCredentials(t, api.google(googleBody(signedIn)))
	unknown := "eyJhbGciOiJSUzI1NiJ9.unknown.token"
	requireLoginResponse(t, api.google(googleBody(unknown)), http.StatusUnauthorized, invalidGoogleToken)
	exists := api.issue("123456789012345678901", "bob@example.com", true)
	requireLoginResponse(t, api.google(googleBody(exists)), http.StatusConflict, accountExists)
	api.pool.Close()
	failed := api.issue("111111111111111111111", "carol@example.com", true)
	requireLoginResponse(t, api.google(googleBody(failed)), http.StatusInternalServerError, internalError)

	api.svc.Wait()
	logs := api.logs.String()
	if !strings.Contains(logs, "request failed") || !strings.Contains(logs, googlePath) {
		t.Errorf("failure not logged with its route: %s", logs)
	}
	secrets := []string{googleSubject, "123456789012345678901", "111111111111111111111",
		"ana@example.com", "bob@example.com", "carol@example.com", googleUserAgent, "http-test", "unknown.token"}
	for _, raw := range []string{signedIn, unknown, exists, failed, creds.AccessToken, creds.RefreshToken} {
		hash := auth.HashToken(raw)
		secrets = append(secrets, raw, hex.EncodeToString(hash), base64.StdEncoding.EncodeToString(hash), fmt.Sprint(hash))
	}
	for _, secret := range secrets {
		if strings.Contains(logs, secret) {
			t.Errorf("logs contain %q:\n%s", secret, logs)
		}
	}
}

func TestGoogleSignInEndpointIsPostOnly(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler), nil, nil, nil, Options{})
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, googlePath, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
	}
}
