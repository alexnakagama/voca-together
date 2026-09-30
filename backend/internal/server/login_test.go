package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"vocatogether/backend/internal/auth"
)

const (
	loginPath          = "/v1/auth/login"
	loginPassword      = "plum-lantern-47-orbit" // what register uses
	loginUserAgent     = "VocaTogether/1.0 (iOS 18.1)"
	invalidCredentials = `{"error":{"code":"invalid_credentials"}}`
	emailNotVerified   = `{"error":{"code":"email_not_verified"}}`
)

func loginBody(addr, password string) string {
	return `{"email":"` + addr + `","password":"` + password + `"}`
}

func (a testAPI) login(body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, loginPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", loginUserAgent)
	a.handler.ServeHTTP(rec, req)
	return rec
}

// verifiedAccount registers and verifies addr through the API.
func (a testAPI) verifiedAccount(t *testing.T, addr string) {
	t.Helper()
	token := a.register(t, addr)
	if rec := a.postTo(verifyPath, tokenBody(token)); rec.Code != http.StatusOK {
		t.Fatalf("verify: status %d", rec.Code)
	}
}

func (a testAPI) sessionCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// requireLoginResponse is requireResponse plus the header every login
// response carries, successful or not.
func requireLoginResponse(t *testing.T, rec *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	requireResponse(t, rec, status, body)
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

type loginTokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

func TestLoginEndpoint(t *testing.T) {
	api := newTestAPI(t)
	api.verifiedAccount(t, "ana@example.com")

	rec := api.login(loginBody("Ana@Example.com", loginPassword))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	// Exactly these fields, nothing else.
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	var got loginTokens
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	if !strings.HasPrefix(got.AccessToken, auth.AccessTokenPrefix) || !strings.HasPrefix(got.RefreshToken, auth.RefreshTokenPrefix) ||
		got.TokenType != "Bearer" || got.ExpiresIn != 900 {
		t.Errorf("response = %+v", got)
	}

	// The stored session is the one whose tokens were returned.
	var n int
	if err := api.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sessions WHERE access_token_hash = $1 AND refresh_token_hash = $2 AND user_agent = $3`,
		auth.HashToken(got.AccessToken), auth.HashToken(got.RefreshToken), loginUserAgent).Scan(&n); err != nil || n != 1 {
		t.Fatalf("matching sessions = %d (err %v), want 1", n, err)
	}

	api.svc.Wait()
	logs := api.logs.String()
	for _, secret := range []string{got.AccessToken, got.RefreshToken, loginPassword, "ana@example.com", loginUserAgent} {
		if strings.Contains(logs, secret) {
			t.Errorf("logs contain a secret %q: %s", secret, logs)
		}
	}
}

// No account enumeration: unknown email and wrong password get
// byte-identical status, headers and body.
func TestLoginEndpointUnknownEmailAndWrongPasswordAreIdentical(t *testing.T) {
	api := newTestAPI(t)
	api.verifiedAccount(t, "ana@example.com")

	wrong := api.login(loginBody("ana@example.com", "wrong-password-123"))
	unknown := api.login(loginBody("nobody@example.com", loginPassword))

	requireLoginResponse(t, wrong, http.StatusUnauthorized, invalidCredentials)
	if wrong.Code != unknown.Code || wrong.Body.String() != unknown.Body.String() ||
		!reflect.DeepEqual(wrong.Header(), unknown.Header()) {
		t.Errorf("responses differ:\nwrong:   %d %v %q\nunknown: %d %v %q",
			wrong.Code, wrong.Header(), wrong.Body, unknown.Code, unknown.Header(), unknown.Body)
	}
	if n := api.sessionCount(t); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
}

func TestLoginEndpointUnverifiedAccount(t *testing.T) {
	api := newTestAPI(t)
	api.register(t, "ana@example.com")

	requireLoginResponse(t, api.login(loginBody("ana@example.com", loginPassword)), http.StatusForbidden, emailNotVerified)
	// Without the right password, an unverified account looks like any other failure.
	requireLoginResponse(t, api.login(loginBody("ana@example.com", "wrong-password-123")), http.StatusUnauthorized, invalidCredentials)
	if n := api.sessionCount(t); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
}

func TestLoginEndpointValidationError(t *testing.T) {
	api := newTestAPI(t)
	both := `{"error":{"code":"validation_failed","fields":[{"field":"email","code":"invalid"},{"field":"password","code":"required"}]}}`
	tests := map[string]struct{ body, want string }{
		"invalid email":  {`{"email":"nope","password":"x"}`, `{"error":{"code":"validation_failed","fields":[{"field":"email","code":"invalid"}]}}`},
		"empty password": {loginBody("ana@example.com", ""), `{"error":{"code":"validation_failed","fields":[{"field":"password","code":"required"}]}}`},
		"missing fields": {`{}`, both},
		"null fields":    {`{"email":null,"password":null}`, both},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			requireLoginResponse(t, api.login(tt.body), http.StatusUnprocessableEntity, tt.want)
		})
	}
}

func TestLoginEndpointRejectsMalformedRequests(t *testing.T) {
	api := newTestAPI(t)
	api.verifiedAccount(t, "ana@example.com")
	valid := loginBody("ana@example.com", loginPassword)

	tests := malformedBodies("email")
	tests["password wrong type"] = `{"email":"ana@example.com","password":12345678901}`
	tests["unknown field with valid credentials"] = `{"email":"ana@example.com","password":"` + loginPassword + `","remember":true}`
	tests["trailing JSON after valid credentials"] = valid + `{}`
	tests["two requests"] = valid + "\n" + valid
	tests["oversized password"] = loginBody("ana@example.com", strings.Repeat("a", maxAuthBodyBytes))
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			requireLoginResponse(t, api.login(body), http.StatusBadRequest, invalidRequest)
		})
	}
	if n := api.sessionCount(t); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
}

func TestLoginEndpointInternalErrorIsOpaque(t *testing.T) {
	api := newTestAPI(t)
	api.verifiedAccount(t, "ana@example.com")
	api.pool.Close()

	requireLoginResponse(t, api.login(loginBody("ana@example.com", loginPassword)), http.StatusInternalServerError, internalError)
	api.svc.Wait()
	logs := api.logs.String()
	if !strings.Contains(logs, "request failed") || !strings.Contains(logs, loginPath) {
		t.Errorf("failure not logged with its route: %s", logs)
	}
	for _, secret := range []string{"ana@example.com", loginPassword, loginUserAgent} {
		if strings.Contains(logs, secret) {
			t.Errorf("logs contain %q: %s", secret, logs)
		}
	}
}

// A failure while storing the session never returns credentials.
func TestLoginEndpointSessionInsertFailureReturnsNoTokens(t *testing.T) {
	api := newTestAPI(t)
	api.verifiedAccount(t, "ana@example.com")
	ctx := context.Background()
	if _, err := api.pool.Exec(ctx, `
		CREATE FUNCTION test_injected_failure() RETURNS trigger LANGUAGE plpgsql
		AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END $$;
		CREATE TRIGGER test_injected_failure BEFORE INSERT ON sessions FOR EACH ROW EXECUTE FUNCTION test_injected_failure()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = api.pool.Exec(ctx, `DROP TRIGGER IF EXISTS test_injected_failure ON sessions; DROP FUNCTION IF EXISTS test_injected_failure()`)
	})

	rec := api.login(loginBody("ana@example.com", loginPassword))
	requireLoginResponse(t, rec, http.StatusInternalServerError, internalError)
	if strings.Contains(rec.Body.String(), "vt_") {
		t.Errorf("error response contains a token: %s", rec.Body)
	}
	if n := api.sessionCount(t); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
}

func TestLoginEndpointIsPostOnly(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler), nil)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, loginPath, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
	}
}
