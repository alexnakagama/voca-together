package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"vocatogether/backend/internal/auth"
	"vocatogether/backend/internal/email"
	"vocatogether/backend/internal/testutil"
)

const registerPath = "/v1/auth/register"

type testAPI struct {
	handler http.Handler
	svc     *auth.Service
	pool    *pgxpool.Pool
	emails  *email.Recorder
	logs    *bytes.Buffer // read only after svc.Wait
}

func newTestAPI(t *testing.T) testAPI {
	t.Helper()
	pool := testutil.DB(t)
	rec := &email.Recorder{}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	base, _ := url.Parse("https://api.example.com")
	svc := auth.NewService(pool, rec, base, logger, auth.AccountLimits{})
	t.Cleanup(svc.Wait)
	return testAPI{handler: New(logger, svc, Options{}), svc: svc, pool: pool, emails: rec, logs: logs}
}

func (a testAPI) post(body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, registerPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	a.handler.ServeHTTP(rec, req)
	return rec
}

func (a testAPI) userCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func requireResponse(t *testing.T, rec *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("status = %d, want %d", rec.Code, status)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Body.String(); got != body+"\n" {
		t.Errorf("body = %q, want %q", got, body)
	}
}

const acceptedBody = `{"status":"accepted"}`

func TestRegisterAccepted(t *testing.T) {
	api := newTestAPI(t)
	rec := api.post(`{"email":"Ana@Example.com","password":"plum-lantern-47-orbit"}`)
	requireResponse(t, rec, http.StatusAccepted, acceptedBody)

	api.svc.Wait()
	if msgs := api.emails.Messages(); len(msgs) != 1 || msgs[0].To != "ana@example.com" {
		t.Errorf("verification email not sent to the normalized address")
	}
}

// No account enumeration: an existing address gets byte-identical output.
func TestRegisterExistingEmailGetsIdenticalResponse(t *testing.T) {
	api := newTestAPI(t)
	first := api.post(`{"email":"ana@example.com","password":"plum-lantern-47-orbit"}`)
	second := api.post(`{"email":"ANA@example.com","password":"another-password-9"}`)

	requireResponse(t, second, http.StatusAccepted, acceptedBody)
	if first.Code != second.Code || first.Body.String() != second.Body.String() ||
		!reflect.DeepEqual(first.Header(), second.Header()) {
		t.Errorf("responses differ:\nnew:      %d %v %q\nexisting: %d %v %q",
			first.Code, first.Header(), first.Body, second.Code, second.Header(), second.Body)
	}
	if n := api.userCount(t); n != 1 {
		t.Errorf("users = %d, want 1", n)
	}
}

func TestRegisterValidationError(t *testing.T) {
	api := newTestAPI(t)
	rec := api.post(`{"email":"nope","password":"abc123x"}`)
	requireResponse(t, rec, http.StatusUnprocessableEntity,
		`{"error":{"code":"validation_failed","fields":[{"field":"email","code":"invalid"},{"field":"password","code":"too_short"}]}}`)
	if n := api.userCount(t); n != 0 {
		t.Errorf("users = %d, want 0", n)
	}
}

func TestRegisterRejectsMalformedRequests(t *testing.T) {
	tests := map[string]string{
		"empty body":        ``,
		"malformed JSON":    `{"email":"ana@example.com",`,
		"not an object":     `["ana@example.com"]`,
		"wrong field type":  `{"email":1,"password":"plum-lantern-47-orbit"}`,
		"unknown field":     `{"email":"ana@example.com","password":"plum-lantern-47-orbit","admin":true}`,
		"trailing JSON":     `{"email":"ana@example.com","password":"plum-lantern-47-orbit"}{}`,
		"trailing garbage":  `{"email":"ana@example.com","password":"plum-lantern-47-orbit"} x`,
		"oversized body":    `{"email":"ana@example.com","password":"` + strings.Repeat("a", maxAuthBodyBytes) + `"}`,
		"two JSON requests": `{"email":"ana@example.com","password":"plum-lantern-47-orbit"}` + "\n" + `{"email":"bob@example.com","password":"plum-lantern-47-orbit"}`,
	}
	api := newTestAPI(t)
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			requireResponse(t, api.post(body), http.StatusBadRequest, `{"error":{"code":"invalid_request"}}`)
		})
	}
	api.svc.Wait()
	if n := api.userCount(t); n != 0 {
		t.Errorf("users = %d, want 0", n)
	}
	if n := len(api.emails.Messages()); n != 0 {
		t.Errorf("sent %d emails, want 0", n)
	}
}

func TestRegisterInternalErrorIsOpaque(t *testing.T) {
	api := newTestAPI(t)
	api.pool.Close()

	rec := api.post(`{"email":"ana@example.com","password":"plum-lantern-47-orbit"}`)
	requireResponse(t, rec, http.StatusInternalServerError, `{"error":{"code":"internal_error"}}`)

	api.svc.Wait()
	logs := api.logs.String()
	if !strings.Contains(logs, "request failed") || !strings.Contains(logs, registerPath) {
		t.Errorf("failure not logged: %s", logs)
	}
	for _, secret := range []string{"ana@example.com", "plum-lantern-47-orbit"} {
		if strings.Contains(logs, secret) {
			t.Errorf("logs contain %q: %s", secret, logs)
		}
	}
}

func TestRegisterRejectsOtherMethods(t *testing.T) {
	rec := httptest.NewRecorder()
	New(slog.New(slog.DiscardHandler), nil, Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, registerPath, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
