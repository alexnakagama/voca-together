package googleid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

var testAcceptedUntil = time.Unix(1_790_003_650, 0)

func secretClaims() Claims {
	return NewClaims(testSubject, testEmail, true, testHD, testAcceptedUntil)
}

// Claims never show their values, through any formatting path.
func TestClaimsAreRedacted(t *testing.T) {
	c := secretClaims()
	holder := struct {
		C  Claims
		P  *Claims
		Cs []Claims
	}{c, &c, []Claims{c}}
	secrets := []string{testSubject, testEmail, testHD}

	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%p", "%T", "%10.3v"} {
		requireNoLeak(t, verb, fmt.Sprintf(verb, c), secrets...)
		requireNoLeak(t, verb+" of holder", fmt.Sprintf(verb, holder), secrets...)
		requireNoLeak(t, verb+" of pointer", fmt.Sprintf(verb, &c), secrets...)
	}
	requireNoLeak(t, "Sprint", fmt.Sprint(c, &c, holder), secrets...)
	requireNoLeak(t, "Sprintln", fmt.Sprintln(c, holder), secrets...)
	requireNoLeak(t, "String", c.String(), secrets...)
	requireNoLeak(t, "GoString", c.GoString(), secrets...)

	for _, v := range []any{c, &c, holder} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		requireNoLeak(t, "JSON", string(b), secrets...)
	}
	text, _ := c.MarshalText()
	requireNoLeak(t, "text", string(text), secrets...)

	var buf bytes.Buffer
	for _, h := range []slog.Handler{slog.NewTextHandler(&buf, nil), slog.NewJSONHandler(&buf, nil)} {
		l := slog.New(h)
		l.Info("claims", "c", c, "p", &c, "holder", holder, slog.Any("any", c), slog.Group("g", "c", c))
	}
	requireNoLeak(t, "slog", buf.String(), secrets...)
	if !strings.Contains(buf.String(), redacted) {
		t.Errorf("slog output doesn't show the redaction marker: %s", buf.String())
	}

	// The values are still there for the code that reads them.
	if c.Subject() != testSubject || c.Email() != testEmail || !c.EmailVerified() || c.HostedDomain() != testHD ||
		!c.AcceptedUntil().Equal(testAcceptedUntil) {
		t.Error("accessors don't return the values")
	}
}

func TestZeroClaims(t *testing.T) {
	var c Claims
	if c.Subject() != "" || c.Email() != "" || c.EmailVerified() || c.HostedDomain() != "" || !c.AcceptedUntil().IsZero() {
		t.Error("zero Claims not empty")
	}
}

// Errors from every path name a fixed reason and nothing else: not the
// token, its claims, the key modulus, the provider's body or the URL.
func TestErrorsLeakNothing(t *testing.T) {
	f := newFixture(t)
	tok := f.valid()
	modulus := b64(keyA.N.Bytes())
	secrets := []string{tok, testSubject, testEmail, testHD, modulus[:40], "PROVIDER-SECRET-BODY", f.ks.URL, kidA}

	var errs []error
	collect := func(raw string) {
		_, err := f.v.Verify(context.Background(), raw)
		errs = append(errs, err)
	}
	f.ks.set(http.StatusInternalServerError, []byte("PROVIDER-SECRET-BODY"))
	collect(tok) // unavailable: status
	f.ks.set(http.StatusOK, []byte(`{"keys":"PROVIDER-SECRET-BODY"}`))
	f.clock.Advance(minRefetchInterval)
	collect(tok) // unavailable: malformed
	f.ks.set(http.StatusOK, jwks(jwk(kidA, &keyA.PublicKey)))
	f.clock.Advance(minRefetchInterval)
	now := f.clock.Now()
	collect(tok[:len(tok)-2])
	collect(sign(keyB, header(kidA), googleClaims(now)))
	collect(sign(keyA, header(kidA), with(googleClaims(now), "aud", testSubject)))
	collect(sign(keyA, header(kidA), with(googleClaims(now), "sub", testEmail+" ")))
	collect(sign(keyA, header(kidA), with(googleClaims(now), "email", 1, "hd", testHD)))

	for _, err := range errs {
		if err == nil {
			t.Fatal("expected an error")
		}
		requireFixedError(t, err)
		for _, s := range []string{err.Error(), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
			requireNoLeak(t, "error", s, secrets...)
		}
	}
}

func TestErrorKindsAreDistinct(t *testing.T) {
	inv := invalid(ReasonExpired)
	if !errors.Is(inv, ErrInvalidToken) || errors.Is(inv, ErrUnavailable) {
		t.Error("invalid-token error matches the wrong sentinel")
	}
	un := &UnavailableError{Reason: failStatus}
	if !errors.Is(un, ErrUnavailable) || errors.Is(un, ErrInvalidToken) {
		t.Error("unavailable error matches the wrong sentinel")
	}
	wrapped := fmt.Errorf("auth: google sign-in: %w", inv)
	var got *InvalidTokenError
	if !errors.As(wrapped, &got) || got.Reason != ReasonExpired || !errors.Is(wrapped, ErrInvalidToken) {
		t.Error("wrapped invalid-token error not recognized")
	}
}

func TestFake(t *testing.T) {
	c := secretClaims()
	f := Fake{Tokens: map[string]Claims{"good-token": c}}

	got, err := f.Verify(context.Background(), "good-token")
	if err != nil || got.Subject() != testSubject {
		t.Fatalf("configured token: %q %v", got.Subject(), err)
	}
	got, err = f.Verify(context.Background(), "secret-unknown-token")
	requireReason(t, err, ReasonBadSignature)
	requireNoLeak(t, "error", err.Error(), "secret-unknown-token")
	if got.Subject() != "" {
		t.Error("claims returned for an unknown token")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Verify(ctx, "good-token"); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: err = %v", err)
	}

	f.Err = &UnavailableError{Reason: failTimeout}
	if _, err := f.Verify(context.Background(), "good-token"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("configured error: err = %v", err)
	}
}
