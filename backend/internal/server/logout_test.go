package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"vocatogether/backend/internal/auth"
)

const (
	logoutPath         = "/v1/auth/logout"
	invalidAccessToken = `{"error":{"code":"invalid_access_token"}}`
)

// logoutWith posts to the logout endpoint with the given Authorization
// header values (none if empty) and body.
func (a testAPI) logoutWith(body string, authorization ...string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, logoutPath, strings.NewReader(body))
	for _, v := range authorization {
		req.Header.Add("Authorization", v)
	}
	a.handler.ServeHTTP(rec, req)
	return rec
}

func (a testAPI) logout(accessToken string) *httptest.ResponseRecorder {
	return a.logoutWith("", "Bearer "+accessToken)
}

func requireLoggedOut(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body %s", rec.Code, rec.Body)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body)
	}
	want := http.Header{"Cache-Control": {"no-store"}}
	for name, value := range apiSecurityHeaders {
		want.Set(name, value)
	}
	if !reflect.DeepEqual(rec.Header(), want) {
		t.Errorf("headers = %v, want %v", rec.Header(), want)
	}
}

func requireUnauthorized(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	requireLoginResponse(t, rec, http.StatusUnauthorized, invalidAccessToken)
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", got)
	}
}

func TestLogoutEndpoint(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	requireLoggedOut(t, api.logout(tokens.AccessToken))
	requireLoginResponse(t, api.refresh(tokens.RefreshToken), http.StatusUnauthorized, invalidRefreshToken)

	api.svc.Wait()
	if logs := api.logs.String(); strings.Contains(logs, tokens.AccessToken) || strings.Contains(logs, tokens.RefreshToken) {
		t.Errorf("logs contain a token: %s", logs)
	}
}

// Every well-formed access token gets the same answer, whatever the state
// of its session, so responses reveal nothing.
func TestLogoutEndpointIsIdempotentAndUniform(t *testing.T) {
	api := newTestAPI(t)
	live := api.loggedIn(t, "ana@example.com")
	expired := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	api.exec(t, `UPDATE sessions SET access_expires_at = now() - interval '1 minute' WHERE access_token_hash = $1`,
		auth.HashToken(expired.AccessToken))
	stale := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	decodeTokens(t, api.refresh(stale.RefreshToken)) // rotates stale.AccessToken out

	for name, token := range map[string]string{
		"live":        live.AccessToken,
		"repeated":    live.AccessToken,
		"expired":     expired.AccessToken,
		"rotated out": stale.AccessToken,
		"unknown":     auth.NewToken(auth.AccessTokenPrefix).Raw,
	} {
		t.Run(name, func(t *testing.T) { requireLoggedOut(t, api.logout(token)) })
	}
}

func TestLogoutEndpointRejectsMissingOrMalformedCredentials(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	for name, tc := range map[string]struct {
		body   string
		header []string
	}{
		"no header":          {},
		"basic scheme":       {header: []string{"Basic " + tokens.AccessToken}},
		"no scheme":          {header: []string{tokens.AccessToken}},
		"bearer only":        {header: []string{"Bearer"}},
		"empty token":        {header: []string{"Bearer "}},
		"double space":       {header: []string{"Bearer  " + tokens.AccessToken}},
		"trailing space":     {header: []string{"Bearer " + tokens.AccessToken + " "}},
		"refresh token":      {header: []string{"Bearer " + tokens.RefreshToken}},
		"garbage":            {header: []string{"Bearer " + auth.AccessTokenPrefix + "garbage"}},
		"two headers":        {header: []string{"Bearer " + tokens.AccessToken, "Bearer " + tokens.AccessToken}},
		"token only in body": {body: `{"access_token":"` + tokens.AccessToken + `"}`},
	} {
		t.Run(name, func(t *testing.T) { requireUnauthorized(t, api.logoutWith(tc.body, tc.header...)) })
	}

	// None of them revoked the session.
	decodeTokens(t, api.refresh(tokens.RefreshToken))
}

func TestLogoutEndpointSchemeIsCaseInsensitive(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	requireLoggedOut(t, api.logoutWith("", "bEARER "+tokens.AccessToken))
	requireLoginResponse(t, api.refresh(tokens.RefreshToken), http.StatusUnauthorized, invalidRefreshToken)
}

func TestLogoutEndpointRevokesOnlyCurrentSession(t *testing.T) {
	api := newTestAPI(t)
	first := api.loggedIn(t, "ana@example.com")
	second := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))

	requireLoggedOut(t, api.logout(first.AccessToken))
	decodeTokens(t, api.refresh(second.RefreshToken))
}

func TestLogoutEndpointInternalErrorIsOpaque(t *testing.T) {
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

	rec := api.logout(tokens.AccessToken)
	requireLoginResponse(t, rec, http.StatusInternalServerError, internalError)
	if strings.Contains(rec.Body.String(), "vt_") {
		t.Errorf("error response contains a token: %s", rec.Body)
	}
	api.svc.Wait()
	logs := api.logs.String()
	if !strings.Contains(logs, "request failed") || !strings.Contains(logs, logoutPath) {
		t.Errorf("failure not logged with its route: %s", logs)
	}
	if strings.Contains(logs, tokens.AccessToken) {
		t.Errorf("logs contain the access token: %s", logs)
	}
}

func TestLogoutEndpointIsPostOnly(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler), nil, nil, Options{})
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, logoutPath, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
	}
}
