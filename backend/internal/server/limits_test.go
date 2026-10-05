package server

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"vocatogether/backend/internal/auth"
	"vocatogether/backend/internal/email"
	"vocatogether/backend/internal/language"
	"vocatogether/backend/internal/profile"
	"vocatogether/backend/internal/ratelimit"
	"vocatogether/backend/internal/testutil"
)

const (
	rateLimited        = `{"error":{"code":"rate_limited"}}`
	serviceUnavailable = `{"error":{"code":"service_unavailable"}}`
)

// dbFreeService is an auth service without a database, for requests that
// are rejected before any database access (missing credentials).
func dbFreeService() *auth.Service {
	return auth.NewService(nil, nil, nil, slog.New(slog.DiscardHandler), auth.AccountLimits{}, nil)
}

func tightLimiter() *ratelimit.Limiter[netip.Prefix] {
	return ratelimit.New[netip.Prefix]("test", 1, time.Hour, 100, slog.New(slog.DiscardHandler))
}

// sendFrom sends a request from the client IP ip (no proxy).
func sendFrom(h http.Handler, ip, method, path, contentType, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = ip + ":1234"
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	h.ServeHTTP(rec, req)
	return rec
}

func requireRateLimitedResponse(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	requireResponse(t, rec, http.StatusTooManyRequests, rateLimited)
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("Retry-After"); got != "3600" {
		t.Errorf("Retry-After = %q, want 3600", got)
	}
}

// Every limited route answers 429 once its bucket is empty, before reading
// the body (junk costs a token too) and without any service call (the
// handler has no service: reaching it would panic).
func TestIPLimitedRoutes(t *testing.T) {
	json := "application/json"
	for _, tc := range []struct {
		path, contentType string
		limiter           func(*IPLimits) **ratelimit.Limiter[netip.Prefix]
	}{
		{loginPath, json, func(l *IPLimits) **ratelimit.Limiter[netip.Prefix] { return &l.Login }},
		{googlePath, json, func(l *IPLimits) **ratelimit.Limiter[netip.Prefix] { return &l.Login }},
		{registerPath, json, func(l *IPLimits) **ratelimit.Limiter[netip.Prefix] { return &l.Register }},
		{resendPath, json, func(l *IPLimits) **ratelimit.Limiter[netip.Prefix] { return &l.Email }},
		{forgotPath, json, func(l *IPLimits) **ratelimit.Limiter[netip.Prefix] { return &l.Email }},
		{verifyPath, json, func(l *IPLimits) **ratelimit.Limiter[netip.Prefix] { return &l.Token }},
		{resetPath, json, func(l *IPLimits) **ratelimit.Limiter[netip.Prefix] { return &l.Token }},
		{refreshPath, json, func(l *IPLimits) **ratelimit.Limiter[netip.Prefix] { return &l.Refresh }},
	} {
		t.Run(tc.path, func(t *testing.T) {
			var limits IPLimits
			*tc.limiter(&limits) = tightLimiter()
			h := New(slog.New(slog.DiscardHandler), nil, nil, nil, Options{IPLimits: limits})

			// The first request passes the limiter; malformed JSON is then
			// rejected by the handler before any service call.
			if rec := sendFrom(h, "198.51.100.1", http.MethodPost, tc.path, tc.contentType, "{"); rec.Code != http.StatusBadRequest {
				t.Fatalf("first request: status %d, want 400", rec.Code)
			}
			requireRateLimitedResponse(t, sendFrom(h, "198.51.100.1", http.MethodPost, tc.path, tc.contentType, "{"))
			// Other clients are unaffected.
			if rec := sendFrom(h, "198.51.100.2", http.MethodPost, tc.path, tc.contentType, "{"); rec.Code != http.StatusBadRequest {
				t.Errorf("other IP: status %d, want 400", rec.Code)
			}
		})
	}
}

// Password login and Google sign-in share one sign-in bucket per IP.
func TestLoginAndGoogleShareIPBucket(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler), nil, nil, nil, Options{IPLimits: IPLimits{Login: tightLimiter()}})
	sendFrom(h, "198.51.100.1", http.MethodPost, loginPath, "application/json", "{")
	requireRateLimitedResponse(t, sendFrom(h, "198.51.100.1", http.MethodPost, googlePath, "application/json", "{"))
}

func TestResendAndForgotShareIPBucket(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler), nil, nil, nil, Options{IPLimits: IPLimits{Email: tightLimiter()}})
	sendFrom(h, "198.51.100.1", http.MethodPost, resendPath, "application/json", "{")
	requireRateLimitedResponse(t, sendFrom(h, "198.51.100.1", http.MethodPost, forgotPath, "application/json", "{"))
}

// The JSON reset endpoint and the emailed page's form share one bucket; the
// form gets the page version of the 429.
func TestResetFormSharesTokenBucketAndGetsAPage(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler), nil, nil, nil, Options{IPLimits: IPLimits{Token: tightLimiter()}})
	sendFrom(h, "198.51.100.1", http.MethodPost, resetPath, "application/json", "{")

	rec := sendFrom(h, "198.51.100.1", http.MethodPost, resetEmailPath, "application/x-www-form-urlencoded", "")
	requirePage(t, rec, http.StatusTooManyRequests, "Too many attempts")
	if got := rec.Header().Get("Retry-After"); got != "3600" {
		t.Errorf("Retry-After = %q, want 3600", got)
	}
}

func TestUnlimitedRoutesNeverRateLimit(t *testing.T) {
	all := IPLimits{Login: tightLimiter(), Register: tightLimiter(), Email: tightLimiter(),
		Token: tightLimiter(), Refresh: tightLimiter()}
	h := New(slog.New(slog.DiscardHandler), dbFreeService(), nil, nil, Options{IPLimits: all})
	for range 5 {
		for _, r := range []struct{ method, path string }{
			{http.MethodGet, "/healthz"},
			{http.MethodGet, resetEmailPath + "?token=x"},
			{http.MethodGet, verifyEmailPath + "?token=x"},
			{http.MethodPost, logoutPath}, // no credentials: 401 before any service call
			{http.MethodGet, mePath},
		} {
			if rec := sendFrom(h, "198.51.100.1", r.method, r.path, "", ""); rec.Code == http.StatusTooManyRequests {
				t.Fatalf("%s %s was rate limited", r.method, r.path)
			}
		}
	}
}

// The client IP comes from X-Forwarded-For only as configured: with one
// trusted hop, clients behind the proxy get separate buckets, and a spoofed
// left-hand entry doesn't buy a fresh one.
func TestIPLimitsUseTrustedProxyHops(t *testing.T) {
	h := New(slog.New(slog.DiscardHandler), nil, nil, nil, Options{TrustedProxyHops: 1, IPLimits: IPLimits{Login: tightLimiter()}})
	send := func(xff string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, loginPath, strings.NewReader("{"))
		req.RemoteAddr = "10.0.0.1:1234" // the proxy
		req.Header.Set("X-Forwarded-For", xff)
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := send("198.51.100.1"); code != http.StatusBadRequest {
		t.Fatalf("first: %d", code)
	}
	if code := send("1.2.3.4, 198.51.100.1"); code != http.StatusTooManyRequests {
		t.Errorf("spoofed left entry: %d, want 429", code)
	}
	if code := send("198.51.100.2"); code != http.StatusBadRequest {
		t.Errorf("other client behind the proxy: %d, want 400", code)
	}
}

func TestNewIPLimitsMatchesTheDecisionLog(t *testing.T) {
	l := NewIPLimits(slog.New(slog.DiscardHandler))
	for name, tc := range map[string]struct {
		lim   *ratelimit.Limiter[netip.Prefix]
		burst int
	}{
		"login": {l.Login, 20}, "register": {l.Register, 10}, "email": {l.Email, 5},
		"token": {l.Token, 10}, "refresh": {l.Refresh, 60},
	} {
		key := netip.MustParsePrefix("198.51.100.1/32")
		for i := range tc.burst {
			if ok, _ := tc.lim.Allow(key); !ok {
				t.Fatalf("%s: denied at %d, burst is %d", name, i+1, tc.burst)
			}
		}
		if ok, _ := tc.lim.Allow(key); ok {
			t.Errorf("%s: allowed past burst %d", name, tc.burst)
		}
	}
}

// ---- Per-account limits over HTTP ----

func newLimitedTestAPI(t *testing.T) testAPI {
	t.Helper()
	pool := testutil.DB(t)
	rec := &email.Recorder{}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	base, _ := url.Parse("https://api.example.com")
	svc := auth.NewService(pool, rec, base, logger, auth.NewAccountLimits(logger), nil)
	t.Cleanup(svc.Wait)
	return testAPI{handler: New(logger, svc, profile.NewService(pool, logger), language.NewService(pool, logger), Options{}), svc: svc, pool: pool, emails: rec, logs: logs}
}

// The 429 for a spent per-account limit is byte-for-byte the same for an
// unknown, an unverified and a verified address, on the same attempt.
func TestAccountLimitResponsesAreIdenticalForAllAccountStates(t *testing.T) {
	api := newLimitedTestAPI(t)
	api.register(t, "unverified@example.com")
	api.verifiedAccount(t, "verified@example.com")

	var first *httptest.ResponseRecorder
	for _, addr := range []string{"unknown@example.com", "unverified@example.com", "verified@example.com"} {
		// register used one mail token for the two existing accounts; top the
		// unknown address up to the same count first.
		if addr == "unknown@example.com" {
			api.postTo(resendPath, forgotBody(addr))
		}
		var last *httptest.ResponseRecorder
		for i := range 5 {
			last = api.postTo(forgotPath, forgotBody(addr))
			if i < 4 && last.Code != http.StatusAccepted {
				t.Fatalf("%s attempt %d: status %d", addr, i+1, last.Code)
			}
		}
		requireRateLimitedResponse(t, last)
		if first == nil {
			first = last
			continue
		}
		if fmt.Sprint(last.Header()) != fmt.Sprint(first.Header()) || last.Body.String() != first.Body.String() {
			t.Errorf("%s: response differs:\n%v %s\nvs\n%v %s", addr, last.Header(), last.Body, first.Header(), first.Body)
		}
	}
	// Login too: 10 attempts, then 429, whatever the account.
	for _, addr := range []string{"unknown@example.com", "verified@example.com"} {
		var last *httptest.ResponseRecorder
		for range 11 {
			last = api.login(loginBody(addr, "wrong-password-1"))
		}
		requireResponse(t, last, http.StatusTooManyRequests, rateLimited)
		if got := last.Header().Get("Retry-After"); got != "60" {
			t.Errorf("%s: Retry-After = %q, want 60", addr, got)
		}
	}
}

// ---- 503 ----

func TestOverloadAndDeadlineMapTo503(t *testing.T) {
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	for _, err := range []error{
		fmt.Errorf("auth: login: %w", auth.ErrOverloaded),
		fmt.Errorf("auth: login: %w", context.DeadlineExceeded),
		auth.ErrGoogleUnavailable,
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, loginPath, nil)
		writeServiceError(rec, req, logger, err)
		requireResponse(t, rec, http.StatusServiceUnavailable, serviceUnavailable)
		if got := rec.Header().Get("Retry-After"); got != "5" {
			t.Errorf("%v: Retry-After = %q, want 5", err, got)
		}
	}
	if !strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("503 not logged at WARN:\n%s", logs)
	}
	// A client that went away is still an ordinary 500.
	rec := httptest.NewRecorder()
	writeServiceError(rec, httptest.NewRequest(http.MethodPost, loginPath, nil), logger, context.Canceled)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Canceled: status %d, want 500", rec.Code)
	}
}

func TestRateLimitedErrorMapsTo429(t *testing.T) {
	for retry, want := range map[time.Duration]string{
		59*time.Minute + 59*time.Second + time.Millisecond: "3600",
		time.Millisecond: "1",
		0:                "1",
		30 * time.Second: "30",
	} {
		rec := httptest.NewRecorder()
		writeServiceError(rec, httptest.NewRequest(http.MethodPost, forgotPath, nil), slog.New(slog.DiscardHandler),
			&auth.RateLimitedError{RetryAfter: retry})
		requireResponse(t, rec, http.StatusTooManyRequests, rateLimited)
		if got := rec.Header().Get("Retry-After"); got != want {
			t.Errorf("RetryAfter %v: header %q, want %q", retry, got, want)
		}
	}
}

func TestResetFormUnavailablePage(t *testing.T) {
	rec := httptest.NewRecorder()
	writePage(rec, http.StatusServiceUnavailable, resetPasswordPageData{State: pageUnavailable})
	requirePage(t, rec, http.StatusServiceUnavailable, "busy")
}
