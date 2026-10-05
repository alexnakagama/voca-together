package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vocatogether/backend/internal/auth"
)

const (
	refreshPath         = "/v1/auth/refresh"
	invalidRefreshToken = `{"error":{"code":"invalid_refresh_token"}}`
)

func refreshBody(token string) string {
	return `{"refresh_token":"` + token + `"}`
}

// loggedIn creates a verified account for addr and logs it in through the
// API, returning its tokens.
func (a testAPI) loggedIn(t *testing.T, addr string) loginTokens {
	t.Helper()
	a.verifiedAccount(t, addr)
	return decodeTokens(t, a.login(loginBody(addr, loginPassword)))
}

func decodeTokens(t *testing.T, rec *httptest.ResponseRecorder) loginTokens {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	var got loginTokens
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	return got
}

func (a testAPI) refresh(token string) *httptest.ResponseRecorder {
	return a.postTo(refreshPath, refreshBody(token))
}

func (a testAPI) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := a.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshEndpoint(t *testing.T) {
	api := newTestAPI(t)
	old := api.loggedIn(t, "ana@example.com")

	rec := api.refresh(old.RefreshToken)
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	got := decodeTokens(t, rec)
	if !strings.HasPrefix(got.AccessToken, auth.AccessTokenPrefix) || !strings.HasPrefix(got.RefreshToken, auth.RefreshTokenPrefix) ||
		got.TokenType != "Bearer" || got.ExpiresIn != 900 {
		t.Errorf("response = %+v", got)
	}
	if got.AccessToken == old.AccessToken || got.RefreshToken == old.RefreshToken {
		t.Error("refresh returned an old token")
	}

	var n int
	if err := api.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sessions WHERE access_token_hash = $1 AND refresh_token_hash = $2 AND revoked_at IS NULL`,
		auth.HashToken(got.AccessToken), auth.HashToken(got.RefreshToken)).Scan(&n); err != nil || n != 1 {
		t.Fatalf("matching sessions = %d (err %v), want 1", n, err)
	}

	api.svc.Wait()
	logs := api.logs.String()
	for _, secret := range []string{old.AccessToken, old.RefreshToken, got.AccessToken, got.RefreshToken} {
		if strings.Contains(logs, secret) {
			t.Errorf("logs contain a token: %s", logs)
		}
	}
}

// Every unusable token gets byte-identical status, headers and body.
func TestRefreshEndpointUnusableTokensAreIdentical(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	valid := tokens.RefreshToken
	body := strings.TrimPrefix(valid, auth.RefreshTokenPrefix)

	cases := map[string]func() string{
		"malformed":    func() string { return valid + "A" },
		"no prefix":    func() string { return body },
		"access token": func() string { return tokens.AccessToken },
		"unknown":      func() string { return auth.RefreshTokenPrefix + strings.Repeat("A", len(body)) },
		"expired (sliding)": func() string {
			t2 := api.loggedIn(t, "ben@example.com")
			api.exec(t, `UPDATE sessions SET refresh_expires_at = now() - interval '1 second',
			             access_expires_at = now() - interval '1 second' WHERE refresh_token_hash = $1`, auth.HashToken(t2.RefreshToken))
			return t2.RefreshToken
		},
		"expired (absolute)": func() string {
			t2 := api.loggedIn(t, "cai@example.com")
			api.exec(t, `UPDATE sessions SET expires_at = now() - interval '1 second', refresh_expires_at = now() - interval '1 second',
			             access_expires_at = now() - interval '1 second' WHERE refresh_token_hash = $1`, auth.HashToken(t2.RefreshToken))
			return t2.RefreshToken
		},
		"revoked": func() string {
			t2 := api.loggedIn(t, "dan@example.com")
			api.exec(t, `UPDATE sessions SET revoked_at = now() WHERE refresh_token_hash = $1`, auth.HashToken(t2.RefreshToken))
			return t2.RefreshToken
		},
		"reused": func() string {
			t2 := api.loggedIn(t, "eva@example.com")
			decodeTokens(t, api.refresh(t2.RefreshToken))
			return t2.RefreshToken
		},
	}

	var first *httptest.ResponseRecorder
	for name, token := range cases {
		rec := api.refresh(token())
		requireLoginResponse(t, rec, http.StatusUnauthorized, invalidRefreshToken)
		if first == nil {
			first = rec
			continue
		}
		if len(rec.Header()) != len(first.Header()) {
			t.Errorf("%s: headers %v differ from %v", name, rec.Header(), first.Header())
		}
		for k := range first.Header() {
			if rec.Header().Get(k) != first.Header().Get(k) {
				t.Errorf("%s: header %s = %q, want %q", name, k, rec.Header().Get(k), first.Header().Get(k))
			}
		}
	}
}

// After reuse is detected, the response carries no tokens and the session's
// current refresh token is dead too.
func TestRefreshEndpointReuseRevokesSession(t *testing.T) {
	api := newTestAPI(t)
	old := api.loggedIn(t, "ana@example.com")
	current := decodeTokens(t, api.refresh(old.RefreshToken))

	rec := api.refresh(old.RefreshToken)
	requireLoginResponse(t, rec, http.StatusUnauthorized, invalidRefreshToken)
	if strings.Contains(rec.Body.String(), "vt_") {
		t.Errorf("reuse response contains a token: %s", rec.Body)
	}
	requireLoginResponse(t, api.refresh(current.RefreshToken), http.StatusUnauthorized, invalidRefreshToken)

	api.svc.Wait()
	if logs := api.logs.String(); !strings.Contains(logs, "reuse detected") {
		t.Errorf("reuse not logged: %s", logs)
	}
}

func TestRefreshEndpointValidationError(t *testing.T) {
	api := newTestAPI(t)
	want := `{"error":{"code":"validation_failed","fields":[{"field":"refresh_token","code":"required"}]}}`
	for _, body := range []string{`{"refresh_token":""}`, `{}`} {
		requireLoginResponse(t, api.postTo(refreshPath, body), http.StatusUnprocessableEntity, want)
	}
}

func TestRefreshEndpointRejectsMalformedRequests(t *testing.T) {
	api := newTestAPI(t)
	for name, body := range map[string]string{
		"not JSON":      `refresh_token=vt_rt_x`,
		"wrong type":    `{"refresh_token":1}`,
		"unknown field": `{"refresh_token":"x","access_token":"y"}`,
		"trailing data": `{"refresh_token":"x"}{}`,
		"too large":     `{"refresh_token":"` + strings.Repeat("a", 9<<10) + `"}`,
		"empty":         ``,
	} {
		t.Run(name, func(t *testing.T) {
			requireLoginResponse(t, api.postTo(refreshPath, body), http.StatusBadRequest, invalidRequest)
		})
	}
}

// An expired access token in the Authorization header doesn't matter:
// refresh authenticates with the refresh token alone.
func TestRefreshEndpointIgnoresAuthorizationHeader(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, refreshPath, strings.NewReader(refreshBody(tokens.RefreshToken)))
	req.Header.Set("Authorization", "Bearer "+auth.AccessTokenPrefix+"garbage")
	api.handler.ServeHTTP(rec, req)
	decodeTokens(t, rec)
}

func TestRefreshEndpointInternalErrorIsOpaque(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	ctx := context.Background()
	api.exec(t, `
		CREATE FUNCTION test_injected_failure() RETURNS trigger LANGUAGE plpgsql
		AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END $$;
		CREATE TRIGGER test_injected_failure BEFORE UPDATE ON sessions FOR EACH ROW EXECUTE FUNCTION test_injected_failure()`)
	t.Cleanup(func() {
		_, _ = api.pool.Exec(ctx, `DROP TRIGGER IF EXISTS test_injected_failure ON sessions; DROP FUNCTION IF EXISTS test_injected_failure()`)
	})

	rec := api.refresh(tokens.RefreshToken)
	requireLoginResponse(t, rec, http.StatusInternalServerError, internalError)
	if strings.Contains(rec.Body.String(), "vt_") {
		t.Errorf("error response contains a token: %s", rec.Body)
	}
	api.svc.Wait()
	logs := api.logs.String()
	if !strings.Contains(logs, "request failed") || !strings.Contains(logs, refreshPath) {
		t.Errorf("failure not logged with its route: %s", logs)
	}
	if strings.Contains(logs, tokens.RefreshToken) {
		t.Errorf("logs contain the refresh token: %s", logs)
	}
}

func TestRefreshEndpointIsPostOnly(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler), nil, nil, Options{})
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, refreshPath, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
	}
}
