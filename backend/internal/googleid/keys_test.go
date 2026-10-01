package googleid

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestKeysAreFetchedLazilyAndCached(t *testing.T) {
	f := newFixture(t)
	if n := f.ks.count(); n != 0 {
		t.Fatalf("constructor fetched %d times", n)
	}
	for range 3 {
		if _, err := f.v.Verify(context.Background(), f.valid()); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.ks.count(); n != 1 {
		t.Errorf("fetches = %d, want 1", n)
	}
}

func TestCacheLifetime(t *testing.T) {
	tests := []struct {
		headers []string
		want    time.Duration
	}{
		{nil, defaultCacheLifetime},
		{[]string{""}, defaultCacheLifetime},
		{[]string{"public, max-age=18766, must-revalidate, no-transform"}, 18766 * time.Second}, // Google's
		{[]string{"max-age=600"}, 10 * time.Minute},
		{[]string{"MAX-AGE=600"}, 10 * time.Minute},
		{[]string{"public", "max-age=600"}, 10 * time.Minute},
		{[]string{" max-age = 600 "}, 10 * time.Minute},
		{[]string{"max-age=60"}, minCacheLifetime},
		{[]string{"max-age=0"}, minCacheLifetime},
		{[]string{"no-cache, max-age=0"}, minCacheLifetime},
		{[]string{"max-age=86400"}, maxCacheLifetime},
		{[]string{"max-age=999999999"}, maxCacheLifetime},
		{[]string{"max-age=99999999999999999999999"}, maxCacheLifetime},
		{[]string{"max-age=9223372036854775807"}, maxCacheLifetime},
		{[]string{"max-age="}, defaultCacheLifetime},
		{[]string{"max-age"}, defaultCacheLifetime},
		{[]string{"max-age=-5"}, defaultCacheLifetime},
		{[]string{"max-age=+5"}, defaultCacheLifetime},
		{[]string{"max-age=1e3"}, defaultCacheLifetime},
		{[]string{"max-age=600s"}, defaultCacheLifetime},
		{[]string{`max-age="600"`}, defaultCacheLifetime},
		{[]string{"max-age=600, max-age=700"}, defaultCacheLifetime},
		{[]string{"max-age=600", "max-age=600"}, defaultCacheLifetime},
		{[]string{"s-maxage=600"}, defaultCacheLifetime},
	}
	for _, tt := range tests {
		if got := cacheLifetime(tt.headers); got != tt.want {
			t.Errorf("cacheLifetime(%q) = %v, want %v", tt.headers, got, tt.want)
		}
	}
}

// The set stays fresh for max-age and is refetched once it is over.
func TestKeysRefreshAfterMaxAge(t *testing.T) {
	f := newFixture(t)
	f.ks.setHeader("Cache-Control", "max-age=600")
	f.prime(t)

	f.clock.Advance(10*time.Minute - time.Second)
	if _, err := f.v.Verify(context.Background(), f.valid()); err != nil || f.ks.count() != 1 {
		t.Fatalf("within max-age: err=%v fetches=%d, want nil, 1", err, f.ks.count())
	}
	f.clock.Advance(time.Second)
	if _, err := f.v.Verify(context.Background(), f.valid()); err != nil || f.ks.count() != 2 {
		t.Fatalf("at max-age: err=%v fetches=%d, want nil, 2", err, f.ks.count())
	}
}

func TestKeysMaxAgeClamped(t *testing.T) {
	for _, tt := range []struct {
		header       string
		stillFresh   time.Duration
		refetchAfter time.Duration
	}{
		{"max-age=1", minCacheLifetime - time.Second, minCacheLifetime},
		{"max-age=31536000", maxCacheLifetime - time.Second, maxCacheLifetime},
		{"max-age=garbage", defaultCacheLifetime - time.Second, defaultCacheLifetime},
	} {
		t.Run(tt.header, func(t *testing.T) {
			f := newFixture(t)
			f.ks.setHeader("Cache-Control", tt.header)
			f.prime(t)
			f.clock.Advance(tt.stillFresh)
			// A token issued "now", so only freshness decides.
			if _, err := f.v.Verify(context.Background(), f.valid()); err != nil || f.ks.count() != 1 {
				t.Fatalf("before lifetime: err=%v fetches=%d", err, f.ks.count())
			}
			f.clock.Advance(tt.refetchAfter - tt.stillFresh)
			if _, err := f.v.Verify(context.Background(), f.valid()); err != nil || f.ks.count() != 2 {
				t.Fatalf("at lifetime: err=%v fetches=%d", err, f.ks.count())
			}
		})
	}
}

// Many requests arriving with an empty cache share one fetch.
func TestConcurrentVerificationsShareOneFetch(t *testing.T) {
	f := newFixture(t)
	f.ks.block()
	tok := f.valid()

	const n = 50
	errs := make(chan error, n)
	for range n {
		go func() {
			_, err := f.v.Verify(context.Background(), tok)
			errs <- err
		}()
	}
	<-f.ks.received
	time.Sleep(50 * time.Millisecond) // let the others queue behind the fetch
	f.ks.release()
	for range n {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	if c := f.ks.count(); c != 1 {
		t.Errorf("fetches = %d, want 1", c)
	}
}

// A request that gives up stops waiting at once; the fetch it started goes
// on (others may be waiting for it) and its result is cached.
func TestCancelledWaitDoesNotAbortFetch(t *testing.T) {
	f := newFixture(t)
	f.ks.block()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := f.v.Verify(ctx, f.valid())
		done <- err
	}()
	<-f.ks.received // the request is in flight
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled request still waiting for the fetch")
	}

	f.ks.release()
	if _, err := f.v.Verify(context.Background(), f.valid()); err != nil {
		t.Fatal(err)
	}
	if c := f.ks.count(); c != 1 {
		t.Errorf("fetches = %d, want 1 (the detached fetch completed and was cached)", c)
	}
}

func TestDeadlineWhileWaitingForFetch(t *testing.T) {
	f := newFixture(t)
	f.ks.block()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := f.v.Verify(ctx, f.valid()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
}

// A fetch that hangs is abandoned after the fetch timeout.
func TestFetchTimeout(t *testing.T) {
	f := newFixture(t)
	f.v.keys.timeout = 100 * time.Millisecond
	f.ks.block()
	start := time.Now()
	_, err := f.v.Verify(context.Background(), f.valid())
	requireUnavailable(t, err, failTimeout)
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("waited %v for a 100ms fetch timeout", d)
	}
}

// An unknown kid may be a key Google just rotated in: it triggers one
// refresh, at most once a minute.
func TestUnknownKidRefreshesKeys(t *testing.T) {
	f := newFixture(t)
	f.prime(t)
	f.ks.set(http.StatusOK, jwks(jwk(kidA, &keyA.PublicKey), jwk(kidB, &keyB.PublicKey)))
	tokB := sign(keyB, header(kidB), googleClaims(f.clock.Now()))

	// Within a minute of the last fetch: no refresh, so still unknown.
	_, err := f.v.Verify(context.Background(), tokB)
	requireReason(t, err, ReasonUnknownKey)
	if c := f.ks.count(); c != 1 {
		t.Fatalf("fetches = %d, want 1", c)
	}

	f.clock.Advance(minRefetchInterval)
	if _, err := f.v.Verify(context.Background(), tokB); err != nil {
		t.Fatalf("rotated-in key: %v", err)
	}
	if c := f.ks.count(); c != 2 {
		t.Errorf("fetches = %d, want 2", c)
	}
}

// Junk kids can't make the verifier call Google more than once a minute.
func TestUnknownKidRefreshIsThrottled(t *testing.T) {
	f := newFixture(t)
	f.prime(t)
	f.clock.Advance(minRefetchInterval)
	now := f.clock.Now()
	for i := range 200 {
		tok := sign(keyA, header(fmt.Sprintf("junk-%d", i)), googleClaims(now))
		_, err := f.v.Verify(context.Background(), tok)
		requireReason(t, err, ReasonUnknownKey)
	}
	if c := f.ks.count(); c != 2 {
		t.Errorf("fetches = %d, want 2 (one priming, one refresh)", c)
	}
	f.clock.Advance(minRefetchInterval)
	_, _ = f.v.Verify(context.Background(), sign(keyA, header("junk-next"), googleClaims(now)))
	if c := f.ks.count(); c != 3 {
		t.Errorf("fetches = %d, want 3 after another minute", c)
	}
	// Known keys keep verifying without fetching.
	if _, err := f.v.Verify(context.Background(), f.valid()); err != nil || f.ks.count() != 3 {
		t.Errorf("known key: err=%v fetches=%d", err, f.ks.count())
	}
}

// Rotation: old and new keys overlap, then the old one is withdrawn; a
// successful refresh replaces the set, so the withdrawn key stops working.
func TestKeyRotation(t *testing.T) {
	f := newFixture(t)
	f.ks.setHeader("Cache-Control", "max-age=600")
	f.prime(t)

	f.ks.set(http.StatusOK, jwks(jwk(kidA, &keyA.PublicKey), jwk(kidB, &keyB.PublicKey)))
	f.clock.Advance(10 * time.Minute)
	now := f.clock.Now()
	for _, tok := range []string{sign(keyA, header(kidA), googleClaims(now)), sign(keyB, header(kidB), googleClaims(now))} {
		if _, err := f.v.Verify(context.Background(), tok); err != nil {
			t.Fatalf("overlap: %v", err)
		}
	}

	f.ks.set(http.StatusOK, jwks(jwk(kidB, &keyB.PublicKey)))
	f.clock.Advance(10 * time.Minute)
	now = f.clock.Now()
	_, err := f.v.Verify(context.Background(), sign(keyA, header(kidA), googleClaims(now)))
	requireReason(t, err, ReasonUnknownKey)
	if _, err := f.v.Verify(context.Background(), sign(keyB, header(kidB), googleClaims(now))); err != nil {
		t.Fatalf("new key: %v", err)
	}
	if c := f.ks.count(); c != 3 {
		t.Errorf("fetches = %d, want 3", c)
	}
}

// A refresh failure doesn't matter while the set is fresh; for an unknown
// kid it is reported as unavailable (the kid might be a key we couldn't get).
func TestFetchFailureWithFreshCache(t *testing.T) {
	f := newFixture(t)
	f.prime(t)
	f.ks.set(http.StatusInternalServerError, []byte("PROVIDER-SECRET-BODY"))
	f.clock.Advance(minRefetchInterval)

	if _, err := f.v.Verify(context.Background(), f.valid()); err != nil {
		t.Fatalf("known key with a fresh set: %v", err)
	}
	if c := f.ks.count(); c != 1 {
		t.Fatalf("fetches = %d, want 1: a fresh set needs no refresh", c)
	}
	_, err := f.v.Verify(context.Background(), sign(keyB, header(kidB), googleClaims(f.clock.Now())))
	requireUnavailable(t, err, failStatus)
	requireNoLeak(t, "error", err.Error(), "PROVIDER-SECRET-BODY", f.ks.URL)
}

// While refreshing fails, a set past its freshness serves for staleGrace
// more, then never again.
func TestStaleGrace(t *testing.T) {
	f := newFixture(t)
	f.ks.setHeader("Cache-Control", "max-age=600")
	f.prime(t)
	f.ks.set(http.StatusServiceUnavailable, nil)

	f.clock.Advance(10 * time.Minute) // stale now
	if _, err := f.v.Verify(context.Background(), f.valid()); err != nil {
		t.Fatalf("just stale: %v", err)
	}
	f.clock.Advance(staleGrace - time.Second) // one second of grace left
	if _, err := f.v.Verify(context.Background(), f.valid()); err != nil {
		t.Fatalf("end of grace: %v", err)
	}
	f.clock.Advance(time.Second)
	_, err := f.v.Verify(context.Background(), f.valid())
	requireUnavailable(t, err, failStatus)

	// Each attempt was throttled: priming, then at most one per minute.
	if c := f.ks.count(); c != 3 {
		t.Errorf("fetches = %d, want 3", c)
	}
	// Within the throttle window it stays unavailable without fetching.
	_, err = f.v.Verify(context.Background(), f.valid())
	requireUnavailable(t, err, failStatus)
	if c := f.ks.count(); c != 3 {
		t.Errorf("fetches = %d, want 3", c)
	}

	// Google is back: the next allowed attempt recovers.
	f.ks.set(http.StatusOK, jwks(jwk(kidA, &keyA.PublicKey)))
	f.clock.Advance(minRefetchInterval)
	if _, err := f.v.Verify(context.Background(), f.valid()); err != nil {
		t.Fatalf("after recovery: %v", err)
	}
}

func TestFetchFailures(t *testing.T) {
	testKeys(t)
	good := jwk(kidA, &keyA.PublicKey)
	tooMany := make([]map[string]any, maxJWKSKeys+1)
	for i := range tooMany {
		tooMany[i] = with(good, "kid", fmt.Sprintf("k%d", i))
	}
	tests := []struct {
		name        string
		status      int
		contentType string
		body        []byte
		want        string
	}{
		{"server error", http.StatusInternalServerError, "application/json", jwks(good), failStatus},
		{"not found", http.StatusNotFound, "application/json", nil, failStatus},
		{"no content", http.StatusNoContent, "application/json", nil, failStatus},
		{"HTML", http.StatusOK, "text/html", jwks(good), failContentType},
		{"no content type", http.StatusOK, "", jwks(good), failContentType},
		{"bad content type", http.StatusOK, "application/json;;", jwks(good), failContentType},
		{"oversized", http.StatusOK, "application/json", append(jwks(good), make([]byte, maxJWKSBytes)...), failTooLarge},
		{"invalid JSON", http.StatusOK, "application/json", []byte(`{"keys":[`), failMalformed},
		{"empty body", http.StatusOK, "application/json", nil, failMalformed},
		{"array", http.StatusOK, "application/json", []byte(`[]`), failMalformed},
		{"trailing data", http.StatusOK, "application/json", append(jwks(good), []byte(`{}`)...), failMalformed},
		{"keys missing", http.StatusOK, "application/json", []byte(`{"other":[]}`), failMalformed},
		{"keys not an array", http.StatusOK, "application/json", []byte(`{"keys":{}}`), failMalformed},
		{"duplicate keys member", http.StatusOK, "application/json", []byte(`{"keys":[],"keys":[]}`), failMalformed},
		{"entry not an object", http.StatusOK, "application/json", []byte(`{"keys":["x"]}`), failMalformed},
		{"too many entries", http.StatusOK, "application/json", jwks(tooMany...), failMalformed},
		{"duplicate kid", http.StatusOK, "application/json", jwks(good, jwk(kidA, &keyB.PublicKey)), failMalformed},
		{"keys null", http.StatusOK, "application/json", []byte(`{"keys":null}`), failMalformed},
		{"no keys", http.StatusOK, "application/json", []byte(`{"keys":[]}`), failNoUsableKeys},
		{"only unusable keys", http.StatusOK, "application/json", jwks(with(good, "kty", "EC")), failNoUsableKeys},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.ks.set(tt.status, tt.body)
			f.ks.setHeader("Content-Type", tt.contentType)
			_, err := f.v.Verify(context.Background(), f.valid())
			requireUnavailable(t, err, tt.want)
		})
	}
}

func TestFetchTransportFailure(t *testing.T) {
	f := newFixture(t)
	f.ks.Close() // nothing listens any more
	_, err := f.v.Verify(context.Background(), f.valid())
	requireUnavailable(t, err, failTransport)
	requireNoLeak(t, "error", err.Error(), f.ks.URL, "127.0.0.1")
}

// A redirect is never followed: it fails the fetch, and the target is never
// contacted.
func TestFetchDoesNotFollowRedirects(t *testing.T) {
	testKeys(t)
	target := newKeyServer(t, jwks(jwk(kidA, &keyA.PublicKey)))
	redirector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	t.Cleanup(redirector.Close)

	clock := newFakeClock()
	client := redirector.Client()
	client.CheckRedirect = nil // the injected client would follow; the verifier must not
	v, err := NewTokenVerifier([]string{testAud}, Options{CertsURL: redirector.URL, HTTPClient: client, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = v.Verify(context.Background(), sign(keyA, header(kidA), googleClaims(clock.Now())))
	requireUnavailable(t, err, failStatus)
	if c := target.count(); c != 0 {
		t.Errorf("redirect target contacted %d times", c)
	}
	if client.CheckRedirect != nil {
		t.Error("the injected client was modified")
	}
}

// The request carries no credentials or cookies, even if the injected client
// has a jar (keyServer answers 418 otherwise).
func TestFetchSendsBareRequest(t *testing.T) {
	f := newFixture(t)
	jarClient := f.ks.Client()
	jarClient.Jar = cookieJar{}
	v, err := NewTokenVerifier([]string{testAud}, Options{CertsURL: f.ks.URL, HTTPClient: jarClient, Now: f.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(context.Background(), f.valid()); err != nil {
		t.Fatal(err)
	}
}

// cookieJar offers a cookie for every request.
type cookieJar struct{}

func (cookieJar) SetCookies(*url.URL, []*http.Cookie) {}
func (cookieJar) Cookies(*url.URL) []*http.Cookie {
	return []*http.Cookie{{Name: "session", Value: "secret"}}
}

// Unusable entries are skipped; usable ones in the same set still work.
func TestUnusableKeysAreIgnored(t *testing.T) {
	testKeys(t)
	pub := &keyB.PublicKey
	even := new(big.Int).Add(pub.N, big.NewInt(1)) // an even modulus is never an RSA modulus
	unusable := map[string]map[string]any{
		"weak 1024-bit key": jwk("weak", &keyBad.PublicKey),
		"EC key":            with(jwk("ec", pub), "kty", "EC"),
		"oct key":           with(jwk("oct", pub), "kty", "oct"),
		"missing kty":       with(jwk("nokty", pub), "kty", nil),
		"alg RS512":         with(jwk("rs512", pub), "alg", "RS512"),
		"alg not a string":  with(jwk("algnum", pub), "alg", 1),
		"use enc":           with(jwk("enc", pub), "use", "enc"),
		"key_ops present":   with(jwk("ops", pub), "key_ops", []string{"verify"}),
		"exponent 3":        with(jwk("e3", pub), "e", "Aw"),
		"exponent padded":   with(jwk("epad", pub), "e", "AAEAAQ"),
		"exponent missing":  with(jwk("enone", pub), "e", nil),
		"modulus even":      with(jwk("even", pub), "n", b64(even.Bytes())),
		"modulus padded":    with(jwk("npad", pub), "n", b64(pub.N.Bytes())+"=="),
		"modulus std b64":   with(jwk("nstd", pub), "n", strings.NewReplacer("-", "+", "_", "/").Replace(b64(pub.N.Bytes()))+"+"),
		"modulus missing":   with(jwk("nnone", pub), "n", nil),
		"modulus number":    with(jwk("nnum", pub), "n", 12345),
		"modulus too large": with(jwk("nbig", pub), "n", b64(make([]byte, maxRSABits/8+1))),
		"modulus empty":     with(jwk("nempty", pub), "n", ""),
		"kid with space":    jwk("bad kid", pub),
		"kid missing":       with(jwk("x", pub), "kid", nil),
	}
	entries := []map[string]any{jwk(kidA, &keyA.PublicKey)}
	for _, e := range unusable {
		entries = append(entries, e)
	}
	if len(entries) > maxJWKSKeys {
		// Split across sets so the entry cap doesn't mask what is tested.
		for name, e := range unusable {
			t.Run(name, func(t *testing.T) { requireOnlyKidAUsable(t, jwks(jwk(kidA, &keyA.PublicKey), e)) })
		}
		return
	}
	requireOnlyKidAUsable(t, jwks(entries...))
}

func requireOnlyKidAUsable(t *testing.T, set []byte) {
	t.Helper()
	keys, failure := parseKeySet(set)
	if failure != "" {
		t.Fatalf("set rejected: %s", failure)
	}
	if len(keys) != 1 || keys[kidA] == nil {
		t.Fatalf("usable kids = %v, want only %s", keys, kidA)
	}
}

// A token signed by a key we refuse to use (weak) fails as an unknown key,
// never as valid.
func TestWeakKeyNeverVerifies(t *testing.T) {
	f := newFixture(t)
	f.ks.set(http.StatusOK, jwks(jwk(kidA, &keyA.PublicKey), jwk("weak", &keyBad.PublicKey)))
	tok := sign(keyBad, header("weak"), googleClaims(f.clock.Now()))
	_, err := f.v.Verify(context.Background(), tok)
	// The 1024-bit signature is too short for any key we accept.
	requireReason(t, err, ReasonBadSignature)
}

// Many goroutines verify while the set expires and rotates; run with -race.
// Every result must be a success or a legitimate rejection, and fetches stay
// bounded by the throttle.
func TestConcurrentVerificationDuringRotation(t *testing.T) {
	f := newFixture(t)
	f.ks.setHeader("Cache-Control", "max-age=300")
	both := jwks(jwk(kidA, &keyA.PublicKey), jwk(kidB, &keyB.PublicKey))
	onlyB := jwks(jwk(kidB, &keyB.PublicKey))

	var wg sync.WaitGroup
	stop := make(chan struct{})
	var mu sync.Mutex
	unexpected := []error{}
	for g := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, kid := keyA, kidA
			if g%2 == 1 {
				key, kid = keyB, kidB
			}
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, err := f.v.Verify(context.Background(), sign(key, header(kid), googleClaims(f.clock.Now())))
				var inv *InvalidTokenError
				if err != nil && !(errors.As(err, &inv) && inv.Reason == ReasonUnknownKey) {
					mu.Lock()
					unexpected = append(unexpected, err)
					mu.Unlock()
				}
			}
		}()
	}
	for i := range 10 {
		time.Sleep(5 * time.Millisecond)
		switch i {
		case 3:
			f.ks.set(http.StatusOK, both)
		case 6:
			f.ks.set(http.StatusOK, onlyB)
		}
		f.clock.Advance(5 * time.Minute) // expires the set every step
	}
	close(stop)
	wg.Wait()
	for _, err := range unexpected {
		t.Errorf("unexpected error: %v", err)
	}
	// One fetch per step at most (each step is past the throttle), plus the first.
	if c := f.ks.count(); c > 11 {
		t.Errorf("fetches = %d, want at most 11", c)
	}
}

// Response headers are bounded like the body: a server sending more than
// maxResponseHeaderBytes fails the fetch, and nothing is cached.
func TestFetchRejectsOversizedHeaders(t *testing.T) {
	f := newFixture(t)
	f.ks.setHeader("X-Padding", strings.Repeat("a", maxResponseHeaderBytes+1))
	_, err := f.v.Verify(context.Background(), f.valid())
	requireUnavailable(t, err, failTransport)

	// Headers under the cap are fine, once the throttle allows a new fetch.
	f.ks.setHeader("X-Padding", strings.Repeat("a", maxResponseHeaderBytes/2))
	f.clock.Advance(minRefetchInterval)
	if _, err := f.v.Verify(context.Background(), f.valid()); err != nil {
		t.Fatalf("verification after a normal response failed: %v", err)
	}
}

// The injected client's transport is copied before the header cap is set.
func TestInjectedTransportIsNotModified(t *testing.T) {
	transport := &http.Transport{}
	base := &http.Client{Transport: transport}
	c := newHTTPClient(base)
	if transport.MaxResponseHeaderBytes != 0 {
		t.Error("the injected transport was modified")
	}
	if got := c.Transport.(*http.Transport).MaxResponseHeaderBytes; got != maxResponseHeaderBytes {
		t.Errorf("MaxResponseHeaderBytes = %d, want %d", got, maxResponseHeaderBytes)
	}
	if got := newHTTPClient(nil).Transport.(*http.Transport).MaxResponseHeaderBytes; got != maxResponseHeaderBytes {
		t.Errorf("default MaxResponseHeaderBytes = %d, want %d", got, maxResponseHeaderBytes)
	}
}
