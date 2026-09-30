package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"vocatogether/backend/internal/auth"
)

func (a testAPI) resetPage(target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func (a testAPI) submitResetForm(target string, form url.Values) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	a.handler.ServeHTTP(rec, req)
	return rec
}

func resetForm(token, password, confirm string) url.Values {
	return url.Values{"token": {token}, "password": {password}, "password_confirm": {confirm}}
}

// requirePage checks the status, that the page shows want, and the headers
// every reset page carries: its URL and form hold a secret.
func requirePage(t *testing.T, rec *httptest.ResponseRecorder, status int, want string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("status = %d, want %d", rec.Code, status)
	}
	if !strings.Contains(rec.Body.String(), want) {
		t.Errorf("page lacks %q:\n%s", want, rec.Body)
	}
	for name, value := range map[string]string{
		"Content-Type":           "text/html; charset=utf-8",
		"Cache-Control":          "no-store",
		"Referrer-Policy":        "no-referrer",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; " +
			"frame-ancestors 'none'; base-uri 'none'",
	} {
		if got := rec.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
}

const (
	pageFormTitle    = "Choose a new password"
	pageDoneTitle    = "Password changed"
	pageInvalidTitle = "This link can't be used"
)

func (a testAPI) tokenUsed(t *testing.T, token string) bool {
	t.Helper()
	var used bool
	if err := a.pool.QueryRow(context.Background(),
		`SELECT used_at IS NOT NULL FROM user_tokens WHERE token_hash = $1`, auth.HashToken(token)).Scan(&used); err != nil {
		t.Fatal(err)
	}
	return used
}

// GET only renders: it never reads the database or uses the token, so a mail
// scanner prefetching the link changes nothing (decision 006).
func TestResetPasswordPageRendersForm(t *testing.T) {
	api := newTestAPI(t)
	api.verifiedAccount(t, "ana@example.com")
	token := api.forgot(t, "ana@example.com")
	api.pool.Close()

	rec := api.resetPage(resetEmailPath + "?token=" + token)
	requirePage(t, rec, http.StatusOK, pageFormTitle)
	body := rec.Body.String()
	for _, want := range []string{
		`<input type="hidden" name="token" value="` + token + `">`,
		`<form method="post" action="/reset-password">`,
		`autocomplete="new-password"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("form lacks %s", want)
		}
	}
}

func TestResetPasswordPageRejectsMalformedLinks(t *testing.T) {
	api := newTestAPI(t)
	for name, target := range map[string]string{
		"no token":      resetEmailPath,
		"empty token":   resetEmailPath + "?token=",
		"junk":          resetEmailPath + "?token=not-a-token",
		"markup":        resetEmailPath + "?token=" + url.QueryEscape(`"><script>alert(1)</script>`),
		"refresh token": resetEmailPath + "?token=" + auth.NewToken(auth.RefreshTokenPrefix).Raw,
	} {
		t.Run(name, func(t *testing.T) {
			rec := api.resetPage(target)
			requirePage(t, rec, http.StatusBadRequest, pageInvalidTitle)
			if strings.Contains(rec.Body.String(), "<form") || strings.Contains(rec.Body.String(), "script>alert") {
				t.Errorf("invalid link rendered a form or echoed input:\n%s", rec.Body)
			}
		})
	}
}

func TestResetPasswordFormResetsPassword(t *testing.T) {
	api := newTestAPI(t)
	old := api.loggedIn(t, "ana@example.com")
	token := api.forgot(t, "ana@example.com")

	requirePage(t, api.submitResetForm(resetEmailPath, resetForm(token, resetPassword, resetPassword)),
		http.StatusOK, pageDoneTitle)
	requireUnauthorized(t, api.me(old.AccessToken))
	decodeTokens(t, api.login(loginBody("ana@example.com", resetPassword)))

	// Submitting the form again (back button, double click) is refused.
	requirePage(t, api.submitResetForm(resetEmailPath, resetForm(token, resetPassword, resetPassword)),
		http.StatusBadRequest, pageInvalidTitle)
}

func TestResetPasswordFormConfirmMismatchKeepsToken(t *testing.T) {
	api := newTestAPI(t)
	api.verifiedAccount(t, "ana@example.com")
	token := api.forgot(t, "ana@example.com")

	rec := api.submitResetForm(resetEmailPath, resetForm(token, resetPassword, resetPassword+"x"))
	requirePage(t, rec, http.StatusUnprocessableEntity, "The passwords do not match.")
	if !strings.Contains(rec.Body.String(), `value="`+token+`"`) {
		t.Error("re-rendered form lost the token")
	}
	if api.tokenUsed(t, token) {
		t.Error("token consumed by a mismatched confirmation")
	}
}

func TestResetPasswordFormPolicyErrorKeepsToken(t *testing.T) {
	api := newTestAPI(t)
	api.verifiedAccount(t, "ana@example.com")
	token := api.forgot(t, "ana@example.com")

	for password, message := range map[string]string{
		"short":           "The password is too short.",
		"password123":     "This password is too common.",
		"ANA@example.com": "The password must not be your email address.",
	} {
		rec := api.submitResetForm(resetEmailPath, resetForm(token, password, password))
		requirePage(t, rec, http.StatusUnprocessableEntity, message)
		if !strings.Contains(rec.Body.String(), `value="`+token+`"`) {
			t.Errorf("%s: re-rendered form lost the token", password)
		}
	}
	if api.tokenUsed(t, token) {
		t.Error("token consumed by a rejected password")
	}
	requirePage(t, api.submitResetForm(resetEmailPath, resetForm(token, resetPassword, resetPassword)),
		http.StatusOK, pageDoneTitle)
}

func TestResetPasswordFormRejectsInvalidTokens(t *testing.T) {
	api := newTestAPI(t)
	for name, token := range map[string]string{
		"unknown": auth.NewToken("").Raw,
		"junk":    "not-a-token",
		"empty":   "",
	} {
		t.Run(name, func(t *testing.T) {
			rec := api.submitResetForm(resetEmailPath, resetForm(token, resetPassword, resetPassword))
			requirePage(t, rec, http.StatusBadRequest, pageInvalidTitle)
		})
	}
}

// Only the form body counts: a token in the query string of the POST (the
// form's action has none) is ignored.
func TestResetPasswordFormIgnoresQueryToken(t *testing.T) {
	api := newTestAPI(t)
	api.verifiedAccount(t, "ana@example.com")
	token := api.forgot(t, "ana@example.com")

	rec := api.submitResetForm(resetEmailPath+"?token="+token, url.Values{"password": {resetPassword}, "password_confirm": {resetPassword}})
	requirePage(t, rec, http.StatusBadRequest, pageInvalidTitle)
	if api.tokenUsed(t, token) {
		t.Error("token from the query string was used")
	}
}

func TestResetPasswordFormRejectsOversizedBody(t *testing.T) {
	api := newTestAPI(t)
	rec := api.submitResetForm(resetEmailPath, url.Values{"token": {strings.Repeat("a", maxAuthBodyBytes)}})
	requirePage(t, rec, http.StatusBadRequest, "The form couldn't be read")
}

func TestResetPasswordFormInternalErrorIsOpaque(t *testing.T) {
	api := newTestAPI(t)
	api.verifiedAccount(t, "ana@example.com")
	token := api.forgot(t, "ana@example.com")
	api.pool.Close()

	rec := api.submitResetForm(resetEmailPath, resetForm(token, resetPassword, resetPassword))
	requirePage(t, rec, http.StatusInternalServerError, "Something went wrong")
	api.svc.Wait()
	logs := api.logs.String()
	if !strings.Contains(logs, "request failed") || !strings.Contains(logs, "POST "+resetEmailPath) {
		t.Errorf("failure not logged with its route: %s", logs)
	}
	for _, secret := range []string{token, resetPassword, "ana@example.com"} {
		if strings.Contains(logs, secret) || strings.Contains(rec.Body.String(), secret) {
			t.Errorf("response or logs contain a secret %q", secret)
		}
	}
}

func TestResetPasswordPageMethods(t *testing.T) {
	handler := New(nil, nil)
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, resetEmailPath, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
	}
}
