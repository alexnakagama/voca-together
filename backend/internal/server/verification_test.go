package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

const (
	verifyPath = "/v1/auth/verify-email"
	resendPath = "/v1/auth/resend-verification"

	verifiedBody     = `{"status":"verified"}`
	invalidTokenBody = `{"error":{"code":"validation_failed","fields":[{"field":"token","code":"invalid"}]}}`
	invalidRequest   = `{"error":{"code":"invalid_request"}}`
	internalError    = `{"error":{"code":"internal_error"}}`
)

func (a testAPI) postTo(path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	a.handler.ServeHTTP(rec, req)
	return rec
}

// register creates an unverified account through the API and returns the
// token from its verification email.
func (a testAPI) register(t *testing.T, addr string) string {
	t.Helper()
	if rec := a.post(`{"email":"` + addr + `","password":"plum-lantern-47-orbit"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("register: status %d", rec.Code)
	}
	a.svc.Wait()
	msgs := a.emails.Messages()
	return tokenFromEmail(t, msgs[len(msgs)-1].Text)
}

func tokenFromEmail(t *testing.T, text string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "https://") {
			u, err := url.Parse(line)
			if err != nil {
				t.Fatal(err)
			}
			return u.Query().Get("token")
		}
	}
	t.Fatal("no link in email")
	return ""
}

func tokenBody(token string) string { return `{"token":"` + token + `"}` }

func TestVerifyEmailEndpoint(t *testing.T) {
	api := newTestAPI(t)
	token := api.register(t, "ana@example.com")

	requireResponse(t, api.postTo(verifyPath, tokenBody(token)), http.StatusOK, verifiedBody)
	// Replaying the same link is rejected like any other invalid token.
	requireResponse(t, api.postTo(verifyPath, tokenBody(token)), http.StatusUnprocessableEntity, invalidTokenBody)
}

// Every unusable token gets the same response, so clients can't learn why.
func TestVerifyEmailEndpointInvalidTokensAreIndistinguishable(t *testing.T) {
	api := newTestAPI(t)
	used := api.register(t, "ana@example.com")
	requireResponse(t, api.postTo(verifyPath, tokenBody(used)), http.StatusOK, verifiedBody)
	expired := api.register(t, "bob@example.com")
	if _, err := api.pool.Exec(context.Background(),
		`UPDATE user_tokens SET expires_at = now() - interval '1 second' WHERE used_at IS NULL`); err != nil {
		t.Fatal(err)
	}

	tests := map[string]string{
		"used":       tokenBody(used),
		"expired":    tokenBody(expired),
		"unknown":    tokenBody("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"),
		"malformed":  tokenBody("not-a-token"),
		"empty":      tokenBody(""),
		"missing":    `{}`,
		"null":       `{"token":null}`,
		"whitespace": tokenBody(" " + used),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			requireResponse(t, api.postTo(verifyPath, body), http.StatusUnprocessableEntity, invalidTokenBody)
		})
	}
}

// malformedBodies returns request bodies, built around the endpoint's only
// field, that must be rejected with 400 before reaching the service.
func malformedBodies(field string) map[string]string {
	f := `"` + field + `"`
	return map[string]string{
		"empty body":       ``,
		"malformed JSON":   `{` + f + `:`,
		"not an object":    `["x"]`,
		"wrong type":       `{` + f + `:123}`,
		"unknown field":    `{` + f + `:"x","extra":1}`,
		"trailing JSON":    `{` + f + `:"x"}{}`,
		"trailing garbage": `{` + f + `:"x"} x`,
		"oversized body":   `{` + f + `:"` + strings.Repeat("a", maxAuthBodyBytes) + `"}`,
	}
}

func TestVerifyEmailEndpointRejectsMalformedRequests(t *testing.T) {
	api := newTestAPI(t)
	tests := malformedBodies("token")
	tests["other endpoint's field"] = `{"email":"ana@example.com"}`
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			requireResponse(t, api.postTo(verifyPath, body), http.StatusBadRequest, invalidRequest)
		})
	}
}

func TestVerifyEmailEndpointInternalErrorIsOpaque(t *testing.T) {
	api := newTestAPI(t)
	token := api.register(t, "ana@example.com")
	api.pool.Close()

	requireResponse(t, api.postTo(verifyPath, tokenBody(token)), http.StatusInternalServerError, internalError)
	api.svc.Wait()
	logs := api.logs.String()
	if !strings.Contains(logs, "request failed") || !strings.Contains(logs, verifyPath) {
		t.Errorf("failure not logged with its route: %s", logs)
	}
	if strings.Contains(logs, token) {
		t.Errorf("logs contain the token: %s", logs)
	}
}

func TestResendVerificationEndpoint(t *testing.T) {
	api := newTestAPI(t)
	old := api.register(t, "ana@example.com")

	requireResponse(t, api.postTo(resendPath, `{"email":"Ana@Example.com"}`), http.StatusAccepted, acceptedBody)
	api.svc.Wait()
	msgs := api.emails.Messages()
	if len(msgs) != 2 {
		t.Fatalf("sent %d emails, want 2", len(msgs))
	}
	token := tokenFromEmail(t, msgs[1].Text)

	requireResponse(t, api.postTo(verifyPath, tokenBody(old)), http.StatusUnprocessableEntity, invalidTokenBody)
	requireResponse(t, api.postTo(verifyPath, tokenBody(token)), http.StatusOK, verifiedBody)
}

// No account enumeration: unknown, unverified and verified addresses get
// byte-identical responses.
func TestResendVerificationEndpointIsIndistinguishable(t *testing.T) {
	api := newTestAPI(t)
	api.register(t, "unverified@example.com")
	verified := api.register(t, "verified@example.com")
	requireResponse(t, api.postTo(verifyPath, tokenBody(verified)), http.StatusOK, verifiedBody)

	var responses []*httptest.ResponseRecorder
	for _, addr := range []string{"unknown@example.com", "unverified@example.com", "verified@example.com"} {
		rec := api.postTo(resendPath, `{"email":"`+addr+`"}`)
		requireResponse(t, rec, http.StatusAccepted, acceptedBody)
		responses = append(responses, rec)
	}
	for _, rec := range responses[1:] {
		if !reflect.DeepEqual(rec.Header(), responses[0].Header()) || rec.Body.String() != responses[0].Body.String() {
			t.Errorf("responses differ: %v %q vs %v %q", rec.Header(), rec.Body, responses[0].Header(), responses[0].Body)
		}
	}
}

func TestResendVerificationEndpointValidationError(t *testing.T) {
	api := newTestAPI(t)
	requireResponse(t, api.postTo(resendPath, `{"email":"nope"}`), http.StatusUnprocessableEntity,
		`{"error":{"code":"validation_failed","fields":[{"field":"email","code":"invalid"}]}}`)
}

func TestResendVerificationEndpointRejectsMalformedRequests(t *testing.T) {
	api := newTestAPI(t)
	tests := malformedBodies("email")
	tests["other endpoint's field"] = `{"token":"x"}`
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			requireResponse(t, api.postTo(resendPath, body), http.StatusBadRequest, invalidRequest)
		})
	}
	api.svc.Wait()
	if n := len(api.emails.Messages()); n != 0 {
		t.Errorf("sent %d emails, want 0", n)
	}
}

func TestResendVerificationEndpointInternalErrorIsOpaque(t *testing.T) {
	api := newTestAPI(t)
	api.pool.Close()

	requireResponse(t, api.postTo(resendPath, `{"email":"ana@example.com"}`), http.StatusInternalServerError, internalError)
	api.svc.Wait()
	logs := api.logs.String()
	if !strings.Contains(logs, "request failed") || !strings.Contains(logs, resendPath) {
		t.Errorf("failure not logged with its route: %s", logs)
	}
	if strings.Contains(logs, "ana@example.com") {
		t.Errorf("logs contain the address: %s", logs)
	}
}

func TestVerificationEndpointsArePostOnly(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler), nil, nil, nil, nil, Options{})
	for _, path := range []string{verifyPath, resendPath} {
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: status = %d, want 405", method, path, rec.Code)
			}
		}
	}
}
