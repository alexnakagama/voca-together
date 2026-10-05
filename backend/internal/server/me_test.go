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
)

const mePath = "/v1/me"

// meWith gets the me endpoint at target (mePath plus any query) with the
// given Authorization header values (none if empty).
func (a testAPI) meWith(target string, authorization ...string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for _, v := range authorization {
		req.Header.Add("Authorization", v)
	}
	a.handler.ServeHTTP(rec, req)
	return rec
}

func (a testAPI) me(accessToken string) *httptest.ResponseRecorder {
	return a.meWith(mePath, "Bearer "+accessToken)
}

type meBody struct {
	ID              string    `json:"id"`
	Email           string    `json:"email"`
	EmailVerifiedAt time.Time `json:"email_verified_at"`
	CreatedAt       time.Time `json:"created_at"`
}

// requireMe checks a successful me response and returns its body. It must
// have exactly the public identity fields, and nothing secret.
func requireMe(t *testing.T, rec *httptest.ResponseRecorder) meBody {
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
	if keys := slices.Sorted(maps.Keys(fields)); !slices.Equal(keys, []string{"created_at", "email", "email_verified_at", "id"}) {
		t.Errorf("fields = %v", keys)
	}
	for _, secret := range []string{"password", "hash", "session", "token", "vt_", "$argon2"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("body contains %q: %s", secret, rec.Body)
		}
	}
	var body meBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func (a testAPI) userID(t *testing.T, addr string) string {
	t.Helper()
	var id string
	if err := a.pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, addr).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMeEndpoint(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	got := requireMe(t, api.me(tokens.AccessToken))
	if got.ID != api.userID(t, "ana@example.com") || got.Email != "ana@example.com" {
		t.Errorf("me = %+v", got)
	}
	if got.EmailVerifiedAt.IsZero() || got.CreatedAt.IsZero() || got.EmailVerifiedAt.Before(got.CreatedAt) {
		t.Errorf("timestamps = %+v", got)
	}

	api.svc.Wait()
	if logs := api.logs.String(); strings.Contains(logs, tokens.AccessToken) || strings.Contains(logs, tokens.RefreshToken) {
		t.Errorf("logs contain a token: %s", logs)
	}
}

func TestMeEndpointReturnsEachCallersOwnUser(t *testing.T) {
	api := newTestAPI(t)
	ana := api.loggedIn(t, "ana@example.com")
	ben := api.loggedIn(t, "ben@example.com")

	if got := requireMe(t, api.me(ana.AccessToken)); got.Email != "ana@example.com" {
		t.Errorf("ana's token: %+v", got)
	}
	if got := requireMe(t, api.me(ben.AccessToken)); got.Email != "ben@example.com" {
		t.Errorf("ben's token: %+v", got)
	}
}

func TestMeEndpointSchemeIsCaseInsensitive(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	requireMe(t, api.meWith(mePath, "bEARER "+tokens.AccessToken))
}

func TestMeEndpointRejectsMissingOrMalformedCredentials(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")

	for name, tc := range map[string]struct {
		target string
		header []string
	}{
		"no header":           {},
		"basic scheme":        {header: []string{"Basic " + tokens.AccessToken}},
		"no scheme":           {header: []string{tokens.AccessToken}},
		"bearer only":         {header: []string{"Bearer"}},
		"empty token":         {header: []string{"Bearer "}},
		"double space":        {header: []string{"Bearer  " + tokens.AccessToken}},
		"trailing space":      {header: []string{"Bearer " + tokens.AccessToken + " "}},
		"refresh token":       {header: []string{"Bearer " + tokens.RefreshToken}},
		"garbage":             {header: []string{"Bearer " + auth.AccessTokenPrefix + "garbage"}},
		"two headers":         {header: []string{"Bearer " + tokens.AccessToken, "Bearer " + tokens.AccessToken}},
		"token only in query": {target: mePath + "?access_token=" + tokens.AccessToken},
	} {
		t.Run(name, func(t *testing.T) {
			target := tc.target
			if target == "" {
				target = mePath
			}
			requireUnauthorized(t, api.meWith(target, tc.header...))
		})
	}
}

func TestMeEndpointRejectsUnusableTokens(t *testing.T) {
	api := newTestAPI(t)
	revoked := api.loggedIn(t, "ana@example.com")
	requireLoggedOut(t, api.logout(revoked.AccessToken))
	expired := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	api.exec(t, `UPDATE sessions SET access_expires_at = now() - interval '1 second' WHERE access_token_hash = $1`,
		auth.HashToken(expired.AccessToken))
	stale := decodeTokens(t, api.login(loginBody("ana@example.com", loginPassword)))
	current := decodeTokens(t, api.refresh(stale.RefreshToken)) // rotates stale.AccessToken out

	for name, token := range map[string]string{
		"unknown":     auth.NewToken(auth.AccessTokenPrefix).Raw,
		"revoked":     revoked.AccessToken,
		"expired":     expired.AccessToken,
		"rotated out": stale.AccessToken,
	} {
		t.Run(name, func(t *testing.T) { requireUnauthorized(t, api.me(token)) })
	}

	requireMe(t, api.me(current.AccessToken))
}

func TestMeEndpointInternalErrorIsOpaque(t *testing.T) {
	for name, tc := range map[string]struct{ table, column string }{
		"authentication": {"sessions", "access_token_hash"},
		"user lookup":    {"users", "email"},
	} {
		t.Run(name, func(t *testing.T) {
			api := newTestAPI(t)
			tokens := api.loggedIn(t, "ana@example.com")
			// SELECTs can't fire triggers, so break the query by hiding a column.
			api.exec(t, `ALTER TABLE `+tc.table+` RENAME COLUMN `+tc.column+` TO test_hidden`)
			t.Cleanup(func() {
				_, _ = api.pool.Exec(context.Background(),
					`ALTER TABLE `+tc.table+` RENAME COLUMN test_hidden TO `+tc.column)
			})

			rec := api.me(tokens.AccessToken)
			requireLoginResponse(t, rec, http.StatusInternalServerError, internalError)
			if rec.Header().Get("WWW-Authenticate") != "" {
				t.Error("500 carries WWW-Authenticate")
			}
			api.svc.Wait()
			logs := api.logs.String()
			if !strings.Contains(logs, "request failed") || !strings.Contains(logs, mePath) {
				t.Errorf("failure not logged with its route: %s", logs)
			}
			if strings.Contains(logs, tokens.AccessToken) {
				t.Errorf("logs contain the access token: %s", logs)
			}
		})
	}
}

func TestMeEndpointIsGetOnly(t *testing.T) {
	// A nil service: the mux must answer before authentication runs.
	h := New(slog.New(slog.DiscardHandler), nil, nil, Options{})
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, mePath, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
	}
}

// Mounted without requireAccessToken by mistake, the handler fails closed.
func TestMeHandlerWithoutMiddlewareFailsClosed(t *testing.T) {
	rec := httptest.NewRecorder()
	handleMe(slog.New(slog.DiscardHandler), nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, mePath, nil))
	requireResponse(t, rec, http.StatusInternalServerError, internalError)
}
