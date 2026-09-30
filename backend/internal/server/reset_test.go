package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"vocatogether/backend/internal/auth"
)

const (
	forgotPath = "/v1/auth/forgot-password"
	resetPath  = "/v1/auth/reset-password"

	resetPassword  = "violet-harbor-82-comet"
	resetDoneBody  = `{"status":"password_reset"}`
	resetEmailPath = "/reset-password"
)

func forgotBody(addr string) string { return `{"email":"` + addr + `"}` }

func resetBody(token, password string) string {
	return `{"token":"` + token + `","password":"` + password + `"}`
}

// forgot requests a reset for addr and returns the token from the emailed
// link, checking that the link is the reset page with only the token.
func (a testAPI) forgot(t *testing.T, addr string) string {
	t.Helper()
	requireLoginResponse(t, a.postTo(forgotPath, forgotBody(addr)), http.StatusAccepted, acceptedBody)
	a.svc.Wait()
	msgs := a.emails.Messages()
	if len(msgs) == 0 {
		t.Fatal("no reset email sent")
	}
	text := msgs[len(msgs)-1].Text
	if !strings.Contains(text, "https://api.example.com"+resetEmailPath+"?token=") {
		t.Fatalf("last email is not a reset email: %s", text)
	}
	return tokenFromEmail(t, text)
}

func TestForgotPasswordEndpoint(t *testing.T) {
	api := newTestAPI(t)
	api.verifiedAccount(t, "ana@example.com")

	token := api.forgot(t, "Ana@Example.com")
	if !auth.WellFormedResetToken(token) {
		t.Errorf("emailed token %q is not a well-formed reset token", token)
	}
}

// No account enumeration: unknown, unverified and verified addresses get
// byte-identical responses, headers included.
func TestForgotPasswordEndpointResponsesAreIdentical(t *testing.T) {
	api := newTestAPI(t)
	api.verifiedAccount(t, "ana@example.com")
	api.register(t, "bob@example.com")

	responses := map[string]*httptest.ResponseRecorder{
		"verified":   api.postTo(forgotPath, forgotBody("ana@example.com")),
		"unverified": api.postTo(forgotPath, forgotBody("bob@example.com")),
		"unknown":    api.postTo(forgotPath, forgotBody("nobody@example.com")),
	}
	want := responses["verified"]
	requireLoginResponse(t, want, http.StatusAccepted, acceptedBody)
	for name, rec := range responses {
		if rec.Code != want.Code || rec.Body.String() != want.Body.String() || !reflect.DeepEqual(rec.Header(), want.Header()) {
			t.Errorf("%s: %d %v %q, want %d %v %q", name, rec.Code, rec.Header(), rec.Body, want.Code, want.Header(), want.Body)
		}
	}
}

func TestForgotPasswordEndpointValidation(t *testing.T) {
	api := newTestAPI(t)
	requireLoginResponse(t, api.postTo(forgotPath, forgotBody("nope")), http.StatusUnprocessableEntity,
		`{"error":{"code":"validation_failed","fields":[{"field":"email","code":"invalid"}]}}`)
}

func TestForgotPasswordEndpointRejectsMalformedRequests(t *testing.T) {
	api := newTestAPI(t)
	for name, body := range malformedBodies("email") {
		t.Run(name, func(t *testing.T) {
			requireLoginResponse(t, api.postTo(forgotPath, body), http.StatusBadRequest, invalidRequest)
		})
	}
}

// The full flow: every credential issued before the reset is dead, and the
// new password logs in.
func TestResetPasswordEndpoint(t *testing.T) {
	api := newTestAPI(t)
	old := api.loggedIn(t, "ana@example.com")
	token := api.forgot(t, "ana@example.com")

	rec := api.postTo(resetPath, resetBody(token, resetPassword))
	requireLoginResponse(t, rec, http.StatusOK, resetDoneBody)

	requireUnauthorized(t, api.me(old.AccessToken))
	requireLoginResponse(t, api.refresh(old.RefreshToken), http.StatusUnauthorized, `{"error":{"code":"invalid_refresh_token"}}`)
	requireLoginResponse(t, api.login(loginBody("ana@example.com", loginPassword)), http.StatusUnauthorized, invalidCredentials)
	fresh := decodeTokens(t, api.login(loginBody("ana@example.com", resetPassword)))
	requireMe(t, api.me(fresh.AccessToken))

	// Replaying the link is rejected like any other invalid token.
	requireLoginResponse(t, api.postTo(resetPath, resetBody(token, "another-long-passphrase-9")),
		http.StatusUnprocessableEntity, invalidTokenBody)

	api.svc.Wait()
	logs := api.logs.String()
	for _, secret := range []string{token, resetPassword, loginPassword, "ana@example.com", old.AccessToken, old.RefreshToken} {
		if strings.Contains(logs, secret) {
			t.Errorf("logs contain a secret %q", secret)
		}
	}
}

// Every unusable token gets the same response, so clients can't learn why.
func TestResetPasswordEndpointInvalidTokensAreIndistinguishable(t *testing.T) {
	api := newTestAPI(t)
	tokens := api.loggedIn(t, "ana@example.com")
	used := api.forgot(t, "ana@example.com")
	requireLoginResponse(t, api.postTo(resetPath, resetBody(used, resetPassword)), http.StatusOK, resetDoneBody)
	replaced := api.forgot(t, "ana@example.com")
	api.forgot(t, "ana@example.com")
	api.verifiedAccount(t, "bea@example.com")
	expired := api.forgot(t, "bea@example.com")
	api.exec(t, `UPDATE user_tokens SET expires_at = now() - interval '1 second' WHERE token_hash = $1`, auth.HashToken(expired))
	verification := api.register(t, "cid@example.com")

	tests := map[string]string{
		"used":               resetBody(used, resetPassword),
		"replaced":           resetBody(replaced, resetPassword),
		"expired":            resetBody(expired, resetPassword),
		"verification token": resetBody(verification, resetPassword),
		"unknown":            resetBody("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", resetPassword),
		"access token":       resetBody(tokens.AccessToken, resetPassword),
		"refresh token":      resetBody(tokens.RefreshToken, resetPassword),
		"malformed":          resetBody("not-a-token", resetPassword),
		"empty":              resetBody("", resetPassword),
		"missing":            `{"password":"` + resetPassword + `"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			requireLoginResponse(t, api.postTo(resetPath, body), http.StatusUnprocessableEntity, invalidTokenBody)
		})
	}
}

func TestResetPasswordEndpointPolicyErrorKeepsToken(t *testing.T) {
	api := newTestAPI(t)
	api.verifiedAccount(t, "ana@example.com")
	token := api.forgot(t, "ana@example.com")

	requireLoginResponse(t, api.postTo(resetPath, resetBody(token, "short")), http.StatusUnprocessableEntity,
		`{"error":{"code":"validation_failed","fields":[{"field":"password","code":"too_short"}]}}`)
	requireLoginResponse(t, api.postTo(resetPath, resetBody(token, "ANA@example.com")), http.StatusUnprocessableEntity,
		`{"error":{"code":"validation_failed","fields":[{"field":"password","code":"same_as_email"}]}}`)
	requireLoginResponse(t, api.postTo(resetPath, resetBody("junk", "short")), http.StatusUnprocessableEntity,
		`{"error":{"code":"validation_failed","fields":[{"field":"token","code":"invalid"},{"field":"password","code":"too_short"}]}}`)
	// The link still works after the rejected attempts.
	requireLoginResponse(t, api.postTo(resetPath, resetBody(token, resetPassword)), http.StatusOK, resetDoneBody)
}

func TestResetPasswordEndpointRejectsMalformedRequests(t *testing.T) {
	api := newTestAPI(t)
	tests := malformedBodies("token")
	tests["other endpoint's field"] = `{"email":"ana@example.com"}`
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			requireLoginResponse(t, api.postTo(resetPath, body), http.StatusBadRequest, invalidRequest)
		})
	}
}

func TestResetPasswordEndpointInternalErrorIsOpaque(t *testing.T) {
	api := newTestAPI(t)
	api.verifiedAccount(t, "ana@example.com")
	token := api.forgot(t, "ana@example.com")
	ctx := context.Background()
	api.exec(t, `
		CREATE FUNCTION test_injected_failure() RETURNS trigger LANGUAGE plpgsql
		AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END $$;
		CREATE TRIGGER test_injected_failure BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION test_injected_failure()`)
	t.Cleanup(func() {
		_, _ = api.pool.Exec(ctx, `DROP TRIGGER IF EXISTS test_injected_failure ON users; DROP FUNCTION IF EXISTS test_injected_failure()`)
	})

	rec := api.postTo(resetPath, resetBody(token, resetPassword))
	requireLoginResponse(t, rec, http.StatusInternalServerError, internalError)
	api.svc.Wait()
	logs := api.logs.String()
	if !strings.Contains(logs, "request failed") || !strings.Contains(logs, resetPath) {
		t.Errorf("failure not logged with its route: %s", logs)
	}
	for _, secret := range []string{token, resetPassword, "ana@example.com"} {
		if strings.Contains(logs, secret) || strings.Contains(rec.Body.String(), secret) {
			t.Errorf("response or logs contain a secret %q", secret)
		}
	}
}

func TestPasswordResetEndpointsArePostOnly(t *testing.T) {
	handler := New(nil, nil)
	for _, path := range []string{forgotPath, resetPath} {
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: status = %d, want 405", method, path, rec.Code)
			}
		}
	}
}
