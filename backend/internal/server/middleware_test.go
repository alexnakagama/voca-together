package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var apiSecurityHeaders = map[string]string{
	"X-Content-Type-Options":  "nosniff",
	"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
	"Referrer-Policy":         "no-referrer",
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler), dbFreeService(), nil, nil, nil, nil, Options{})
	for _, r := range []struct{ method, path string }{
		{http.MethodGet, "/healthz"},
		{http.MethodGet, "/nope"},         // 404
		{http.MethodPut, "/healthz"},      // 405
		{http.MethodPost, loginPath},      // 400 (empty body)
		{http.MethodPost, logoutPath},     // 401
		{http.MethodGet, mePath},          // 401
		{http.MethodPost, resetEmailPath}, // 400 page
		{http.MethodGet, resetEmailPath},  // 400 page (no token)
		{http.MethodGet, resetEmailPath + "?token=" + "x"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(r.method, r.path, nil))
		for name, want := range apiSecurityHeaders {
			got := rec.Header().Get(name)
			if name == "Content-Security-Policy" && rec.Header().Get("Content-Type") == "text/html; charset=utf-8" {
				continue // pages set their own policy (see requirePage)
			}
			if got != want {
				t.Errorf("%s %s: %s = %q, want %q", r.method, r.path, name, got, want)
			}
		}
		if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
			t.Errorf("%s %s: HSTS sent without the option: %q", r.method, r.path, got)
		}
	}
}

func TestHSTSOnlyWhenEnabled(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler), nil, nil, nil, nil, nil, Options{HSTS: true})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Errorf("HSTS = %q", got)
	}
}

// Every auth endpoint marks every response no-store (CLAUDE.md convention),
// including the ones that carry no credentials.
func TestAuthEndpointsAreNoStore(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler), nil, nil, nil, nil, nil, Options{})
	for _, path := range []string{registerPath, verifyPath, resendPath, forgotPath, resetPath, loginPath, refreshPath, googlePath} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil)) // 400
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", path, got)
		}
	}
}

func TestRegisterVerifyResendSuccessAreNoStore(t *testing.T) {
	api := newTestAPI(t)
	for _, rec := range []*httptest.ResponseRecorder{
		api.post(`{"email":"ana@example.com","password":"plum-lantern-47-orbit"}`),
		api.postTo(resendPath, forgotBody("ana@example.com")),
	} {
		if rec.Code != http.StatusAccepted || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("status %d, Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
		}
	}
	api.svc.Wait()
	msgs := api.emails.Messages()
	rec := api.postTo(verifyPath, tokenBody(tokenFromEmail(t, msgs[len(msgs)-1].Text)))
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("verify: status %d, Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

func TestRequestDeadline(t *testing.T) {
	var deadline time.Time
	var ok bool
	h := requestDeadline(time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok = r.Context().Deadline()
	}))
	start := time.Now()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !ok || deadline.Before(start.Add(59*time.Second)) || deadline.After(start.Add(61*time.Second)) {
		t.Fatalf("deadline = %v (set %v), want about a minute from now", deadline, ok)
	}

	// The deadline is below the server's WriteTimeout, so the 503 can still
	// be written.
	if requestTimeout >= 15*time.Second {
		t.Errorf("requestTimeout = %v, must stay below the 15s WriteTimeout", requestTimeout)
	}
}

// A handler blocked on the request context (as the DB pool and the argon2
// queue are) is released by the deadline and answers 503.
func TestRequestDeadlineEndsBlockedHandlerWith503(t *testing.T) {
	h := requestDeadline(20 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		writeServiceError(w, r, slog.New(slog.DiscardHandler), r.Context().Err())
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, loginPath, nil))
	requireResponse(t, rec, http.StatusServiceUnavailable, serviceUnavailable)
}
