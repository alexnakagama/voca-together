package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"vocatogether/backend/internal/auth"
)

const (
	verifyEmailPath = "/verify-email"

	verifyFormTitle    = "Confirm your email address"
	verifyDoneTitle    = "Email address verified"
	verifyInvalidTitle = "This link can't be used"
)

func (a testAPI) verifyPage(target string) *httptest.ResponseRecorder {
	return a.resetPage(target) // a plain GET
}

func (a testAPI) submitVerifyForm(target, token string) *httptest.ResponseRecorder {
	return a.submitResetForm(target, url.Values{"token": {token}})
}

func (a testAPI) emailVerified(t *testing.T, addr string) bool {
	t.Helper()
	var verified bool
	if err := a.pool.QueryRow(context.Background(),
		`SELECT email_verified_at IS NOT NULL FROM users WHERE email = $1`, addr).Scan(&verified); err != nil {
		t.Fatal(err)
	}
	return verified
}

// requireVerifyPage checks what requirePage does (status, content, headers)
// and that the page is inert: no scripts, nothing fetched, no links out, and
// the token nowhere but, on the form, its single hidden field.
func requireVerifyPage(t *testing.T, rec *httptest.ResponseRecorder, status int, want, token string) {
	t.Helper()
	requirePage(t, rec, status, want)
	body := rec.Body.String()
	lower := strings.ToLower(body)
	for _, bad := range []string{"<script", "javascript:", "<img", "<iframe", "<object", "<embed", "<link",
		"<svg", "<video", "<audio", "<base", "src=", "srcset=", "url(", "@import", "http://", "https://", "href="} {
		if strings.Contains(lower, bad) {
			t.Errorf("page contains %q", bad)
		}
	}
	if token == "" {
		return
	}
	hidden := `<input type="hidden" name="token" value="` + token + `">`
	switch n := strings.Count(body, token); {
	case want == verifyFormTitle && (n != 1 || !strings.Contains(body, hidden)):
		t.Errorf("form shows the token %d times, want once, in the hidden field", n)
	case want != verifyFormTitle && n != 0:
		t.Errorf("page shows the token")
	}
}

// GET only renders: it never reads the database or uses the token, so a mail
// scanner prefetching the link changes nothing.
func TestVerifyEmailPageRendersFormWithoutDatabase(t *testing.T) {
	api := newTestAPI(t)
	token := api.register(t, "ana@example.com")
	api.pool.Close()

	rec := api.verifyPage(verifyEmailPath + "?token=" + token)
	requireVerifyPage(t, rec, http.StatusOK, verifyFormTitle, token)
	for _, want := range []string{`<form method="post" action="/verify-email">`, `<button type="submit">Verify my email</button>`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("form lacks %s", want)
		}
	}
}

func TestVerifyEmailPageGetDoesNotConsumeToken(t *testing.T) {
	api := newTestAPI(t)
	token := api.register(t, "ana@example.com")

	for range 3 {
		requireVerifyPage(t, api.verifyPage(verifyEmailPath+"?token="+token), http.StatusOK, verifyFormTitle, token)
	}
	if api.tokenUsed(t, token) || api.emailVerified(t, "ana@example.com") {
		t.Fatal("GET used the token")
	}
	requireVerifyPage(t, api.submitVerifyForm(verifyEmailPath, token), http.StatusOK, verifyDoneTitle, token)
}

func TestVerifyEmailPageRejectsMalformedLinks(t *testing.T) {
	api := newTestAPI(t)
	for name, target := range map[string]string{
		"no token":      verifyEmailPath,
		"empty token":   verifyEmailPath + "?token=",
		"junk":          verifyEmailPath + "?token=not-a-token",
		"markup":        verifyEmailPath + "?token=" + url.QueryEscape(`"><script>alert(1)</script>`),
		"refresh token": verifyEmailPath + "?token=" + auth.NewToken(auth.RefreshTokenPrefix).Raw,
	} {
		t.Run(name, func(t *testing.T) {
			rec := api.verifyPage(target)
			requireVerifyPage(t, rec, http.StatusBadRequest, verifyInvalidTitle, "")
			if strings.Contains(rec.Body.String(), "<form") || strings.Contains(rec.Body.String(), "alert") ||
				strings.Contains(rec.Body.String(), "not-a-token") {
				t.Errorf("invalid link rendered a form or echoed input:\n%s", rec.Body)
			}
		})
	}
}

func TestVerifyEmailFormVerifiesOnce(t *testing.T) {
	api := newTestAPI(t)
	token := api.register(t, "ana@example.com")

	requireVerifyPage(t, api.submitVerifyForm(verifyEmailPath, token), http.StatusOK, verifyDoneTitle, token)
	if !api.tokenUsed(t, token) || !api.emailVerified(t, "ana@example.com") {
		t.Fatal("form did not verify the address")
	}
	// Submitting again (back button, double click) is refused.
	requireVerifyPage(t, api.submitVerifyForm(verifyEmailPath, token), http.StatusBadRequest, verifyInvalidTitle, token)
	requireResponse(t, api.postTo(verifyPath, tokenBody(token)), http.StatusUnprocessableEntity, invalidTokenBody)
}

// Every unusable token gets the same page, so it can't tell why.
func TestVerifyEmailFormInvalidTokensAreIndistinguishable(t *testing.T) {
	api := newTestAPI(t)
	used := api.register(t, "used@example.com")
	requireVerifyPage(t, api.submitVerifyForm(verifyEmailPath, used), http.StatusOK, verifyDoneTitle, used)
	expired := api.register(t, "expired@example.com")
	api.exec(t, `UPDATE user_tokens SET expires_at = now() - interval '1 second' WHERE token_hash = $1`,
		auth.HashToken(expired))
	api.verifiedAccount(t, "reset@example.com")
	reset := api.forgot(t, "reset@example.com") // well-formed, wrong purpose

	var bodies []string
	for name, token := range map[string]string{
		"used":        used,
		"expired":     expired,
		"unknown":     auth.NewToken("").Raw,
		"reset token": reset,
		"junk":        "not-a-token",
		"empty":       "",
	} {
		rec := api.submitVerifyForm(verifyEmailPath, token)
		t.Run(name, func(t *testing.T) {
			requireVerifyPage(t, rec, http.StatusBadRequest, verifyInvalidTitle, token)
		})
		bodies = append(bodies, rec.Body.String())
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Error("invalid-token pages differ")
		}
	}
	if api.tokenUsed(t, reset) || api.emailVerified(t, "expired@example.com") {
		t.Error("an invalid submission changed state")
	}
}

// Only the form body counts: a token in the query string of the POST (the
// form's action has none) is ignored.
func TestVerifyEmailFormIgnoresQueryToken(t *testing.T) {
	api := newTestAPI(t)
	token := api.register(t, "ana@example.com")

	rec := api.submitResetForm(verifyEmailPath+"?token="+token, url.Values{})
	requireVerifyPage(t, rec, http.StatusBadRequest, verifyInvalidTitle, token)
	if api.tokenUsed(t, token) {
		t.Error("token from the query string was used")
	}
}

func TestVerifyEmailFormRejectsOversizedBody(t *testing.T) {
	api := newTestAPI(t)
	rec := api.submitVerifyForm(verifyEmailPath, strings.Repeat("a", maxAuthBodyBytes))
	requireVerifyPage(t, rec, http.StatusBadRequest, "The form couldn't be read", "")
}

func TestVerifyEmailFormInternalErrorIsOpaque(t *testing.T) {
	api := newTestAPI(t)
	token := api.register(t, "ana@example.com")
	api.pool.Close()

	rec := api.submitVerifyForm(verifyEmailPath, token)
	requireVerifyPage(t, rec, http.StatusInternalServerError, "Something went wrong", token)
	logs := api.logs.String()
	if !strings.Contains(logs, "request failed") || !strings.Contains(logs, "POST "+verifyEmailPath) {
		t.Errorf("failure not logged with its route: %s", logs)
	}
	if strings.Contains(logs, token) {
		t.Error("logs contain the token")
	}
}

// The JSON verify endpoint and the page's form share one bucket; the form gets
// the page version of the 429. The GET is never limited.
func TestVerifyEmailFormSharesTokenBucketAndGetsAPage(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler), nil, nil, nil, nil, nil, Options{IPLimits: IPLimits{Token: tightLimiter()}})
	sendFrom(h, "198.51.100.1", http.MethodPost, verifyPath, "application/json", "{")

	rec := sendFrom(h, "198.51.100.1", http.MethodPost, verifyEmailPath, "application/x-www-form-urlencoded", "")
	requireVerifyPage(t, rec, http.StatusTooManyRequests, "Too many attempts", "")
	if got := rec.Header().Get("Retry-After"); got != "3600" {
		t.Errorf("Retry-After = %q, want 3600", got)
	}
	// The reset form draws from the same bucket.
	rec = sendFrom(h, "198.51.100.1", http.MethodPost, resetEmailPath, "application/x-www-form-urlencoded", "")
	requirePage(t, rec, http.StatusTooManyRequests, "Too many attempts")

	for range 5 {
		rec := sendFrom(h, "198.51.100.1", http.MethodGet, verifyEmailPath+"?token=x", "", "")
		requireVerifyPage(t, rec, http.StatusBadRequest, verifyInvalidTitle, "")
	}
}

func TestVerifyEmailPageMethods(t *testing.T) {
	handler := New(nil, nil, nil, nil, nil, nil, Options{})
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, verifyEmailPath, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
	}
}
