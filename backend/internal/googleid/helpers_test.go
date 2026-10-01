package googleid

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testAud     = "123456789012-abcdefghijklmnopqrstuvwxyz012345.apps.googleusercontent.com"
	testSubject = "110169484474386276334"
	testEmail   = "ana.secret@example.com"
	testHD      = "secret-domain.example"
	kidA        = "kid-a-0123456789abcdef"
	kidB        = "kid-b-0123456789abcdef"
)

// Keys are generated once per test binary: 2048-bit generation is slow.
var (
	keysOnce           sync.Once
	keyA, keyB, keyBad *rsa.PrivateKey // keyBad is 1024-bit: too weak
)

func testKeys(t testing.TB) {
	t.Helper()
	keysOnce.Do(func() {
		var err error
		for _, k := range []**rsa.PrivateKey{&keyA, &keyB} {
			if *k, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
				panic(err)
			}
		}
		if keyBad, err = rsa.GenerateKey(rand.Reader, 1024); err != nil {
			panic(err)
		}
	})
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// jwk returns the JWKS entry Google would publish for key.
func jwk(kid string, key *rsa.PublicKey) map[string]any {
	return map[string]any{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid,
		"n": b64(key.N.Bytes()), "e": b64(big.NewInt(int64(key.E)).Bytes()),
	}
}

func jwks(entries ...map[string]any) []byte {
	b, err := json.Marshal(map[string]any{"keys": entries})
	if err != nil {
		panic(err)
	}
	return b
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// signRaw builds a compact token from raw header and payload JSON, signed
// with RS256 by key, so tests can produce any (also malformed) JSON.
func signRaw(key *rsa.PrivateKey, headerJSON, payloadJSON string) string {
	input := b64([]byte(headerJSON)) + "." + b64([]byte(payloadJSON))
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	return input + "." + b64(sig)
}

func header(kid string) map[string]any {
	return map[string]any{"alg": "RS256", "kid": kid, "typ": "JWT"}
}

// googleClaims is a valid payload as Google issues it, relative to now.
func googleClaims(now time.Time) map[string]any {
	return map[string]any{
		"iss": "https://accounts.google.com", "azp": "android-client.apps.googleusercontent.com",
		"aud": testAud, "sub": testSubject, "email": testEmail, "email_verified": true, "hd": testHD,
		"iat": now.Add(-10 * time.Second).Unix(), "exp": now.Add(time.Hour - 10*time.Second).Unix(),
		"name": "Ana", "picture": "https://example.com/a.png",
	}
}

// with returns a copy of m with the given members set; a nil value deletes.
func with(m map[string]any, kv ...any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	for i := 0; i < len(kv); i += 2 {
		if kv[i+1] == nil {
			delete(out, kv[i].(string))
		} else {
			out[kv[i].(string)] = kv[i+1]
		}
	}
	return out
}

func sign(key *rsa.PrivateKey, h, claims map[string]any) string {
	return signRaw(key, mustJSON(h), mustJSON(claims))
}

// fakeClock is a settable clock, safe for concurrent use.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Unix(1_790_000_000, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// keyServer is a local TLS stand-in for Google's JWKS endpoint.
type keyServer struct {
	*httptest.Server
	requests atomic.Int64

	mu       sync.Mutex
	status   int
	body     []byte
	headers  map[string]string
	gate     chan struct{} // if non-nil, requests block until it is closed
	received chan struct{} // gets a value (if there's room) per request
}

func newKeyServer(t testing.TB, body []byte) *keyServer {
	t.Helper()
	ks := &keyServer{status: http.StatusOK, body: body, received: make(chan struct{}, 100),
		headers: map[string]string{"Content-Type": "application/json; charset=UTF-8", "Cache-Control": "public, max-age=3600"}}
	ks.Server = httptest.NewTLSServer(http.HandlerFunc(ks.serve))
	t.Cleanup(ks.Close)
	t.Cleanup(ks.release) // runs first: blocked handlers must finish before Close
	return ks
}

func (ks *keyServer) serve(w http.ResponseWriter, r *http.Request) {
	ks.requests.Add(1)
	select {
	case ks.received <- struct{}{}:
	default:
	}
	ks.mu.Lock()
	gate, status, body := ks.gate, ks.status, ks.body
	for k, v := range ks.headers {
		w.Header().Set(k, v)
	}
	ks.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
		status = http.StatusTeapot // the verifier must send a bare GET
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (ks *keyServer) set(status int, body []byte) {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	ks.status, ks.body = status, body
}

func (ks *keyServer) setHeader(k, v string) {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	ks.headers[k] = v
}

// block makes requests wait until release.
func (ks *keyServer) block() {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	ks.gate = make(chan struct{})
}

func (ks *keyServer) release() {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	if ks.gate != nil {
		close(ks.gate)
		ks.gate = nil
	}
}

func (ks *keyServer) count() int64 { return ks.requests.Load() }

func newVerifier(t testing.TB, ks *keyServer, clock *fakeClock) *TokenVerifier {
	t.Helper()
	v, err := NewTokenVerifier([]string{testAud}, Options{CertsURL: ks.URL, HTTPClient: ks.Client(), Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// fixture is a verifier whose key server publishes keyA as kidA.
type fixture struct {
	ks    *keyServer
	clock *fakeClock
	v     *TokenVerifier
}

func newFixture(t testing.TB) fixture {
	t.Helper()
	testKeys(t)
	ks := newKeyServer(t, jwks(jwk(kidA, &keyA.PublicKey)))
	clock := newFakeClock()
	return fixture{ks: ks, clock: clock, v: newVerifier(t, ks, clock)}
}

// valid returns a valid token for now, signed with keyA as kidA.
func (f fixture) valid() string { return sign(keyA, header(kidA), googleClaims(f.clock.Now())) }

// prime makes the verifier fetch and cache the key set.
func (f fixture) prime(t testing.TB) {
	t.Helper()
	if _, err := f.v.Verify(context.Background(), f.valid()); err != nil {
		t.Fatalf("priming verification failed: %v", err)
	}
}

// fixedErrors are the only messages Verify may return: fixed strings that
// name a reason and nothing else (no token, claim, key or provider data).
var fixedErrors = func() map[string]bool {
	m := map[string]bool{context.Canceled.Error(): true, context.DeadlineExceeded.Error(): true}
	for _, r := range []Reason{ReasonMalformed, ReasonUnsupportedAlg, ReasonUnknownKey, ReasonBadSignature,
		ReasonWrongIssuer, ReasonWrongAudience, ReasonExpired, ReasonIssuedInFuture, ReasonBadSubject, ReasonBadClaims} {
		m[invalid(r).Error()] = true
	}
	for _, r := range []string{failTransport, failTimeout, failStatus, failContentType, failTooLarge, failMalformed, failNoUsableKeys} {
		m[(&UnavailableError{Reason: r}).Error()] = true
	}
	return m
}()

func requireFixedError(t testing.TB, err error) {
	t.Helper()
	if err != nil && !fixedErrors[err.Error()] {
		t.Fatalf("error %q is not one of the fixed messages", err)
	}
}

func requireReason(t testing.TB, err error, want Reason) {
	t.Helper()
	var inv *InvalidTokenError
	if !errors.As(err, &inv) || inv.Reason != want || !errors.Is(err, ErrInvalidToken) || errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want invalid token (%s)", err, want)
	}
	requireFixedError(t, err)
}

func requireUnavailable(t testing.TB, err error, want string) {
	t.Helper()
	var u *UnavailableError
	if !errors.As(err, &u) || u.Reason != want || !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want unavailable (%s)", err, want)
	}
	requireFixedError(t, err)
}

func requireNoLeak(t testing.TB, what, s string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(s, secret) {
			t.Errorf("%s leaks %q: %s", what, secret, s)
		}
	}
}
