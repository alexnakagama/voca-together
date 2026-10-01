package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vocatogether/backend/internal/auth"
	"vocatogether/backend/internal/config"
	"vocatogether/backend/internal/googleid"
)

// testClientID is shaped like a real Web client ID; its prefix is a marker
// that no log or error may show.
const testClientID = "987654321098-clientidmarker.apps.googleusercontent.com"

// googleVerifier calls newGoogleVerifier and checks that nothing it logs or
// returns shows the client ID.
func googleVerifier(t *testing.T, cfg config.Config, opts googleid.Options) (googleid.Verifier, string, error) {
	t.Helper()
	var buf bytes.Buffer
	v, err := newGoogleVerifier(cfg, slog.New(slog.NewJSONHandler(&buf, nil)), opts)
	out := buf.String()
	if err != nil {
		out += err.Error()
	}
	if strings.Contains(out, "clientidmarker") {
		t.Errorf("output shows the client ID: %s", out)
	}
	return v, buf.String(), err
}

func TestNewGoogleVerifier(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  config.Config
		want string // "enabled", "disabled", or "" when an error is expected
	}{
		{"production", config.Config{Env: "production", GoogleClientID: testClientID}, "enabled"},
		{"development with ID", config.Config{Env: "development", GoogleClientID: testClientID}, "enabled"},
		{"development without ID", config.Config{Env: "development"}, "disabled"},
		// config.Load always clears it in test; a stray value must not enable it.
		{"test with ID", config.Config{Env: "test", GoogleClientID: testClientID}, "disabled"},
		{"test without ID", config.Config{Env: "test"}, "disabled"},
		// config.Load rejects these; newGoogleVerifier must not fall back to disabled.
		{"production without ID", config.Config{Env: "production"}, ""},
		{"production invalid ID", config.Config{Env: "production", GoogleClientID: "GOCSPX-clientidmarker"}, ""},
		{"development invalid ID", config.Config{Env: "development",
			GoogleClientID: "clientidmarker.googleusercontent.com"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, logs, err := googleVerifier(t, tc.cfg, googleid.Options{})
			switch tc.want {
			case "":
				if err == nil {
					t.Fatalf("got verifier %T, want an error", v)
				}
				if v != nil {
					t.Errorf("verifier %T returned with an error", v)
				}
				if logs != "" {
					t.Errorf("logged a status despite the error: %s", logs)
				}
				return
			case "disabled":
				// An untyped nil: a typed nil *TokenVerifier would look
				// configured to auth.Service.
				if err != nil || v != nil {
					t.Fatalf("got %T, %v; want an untyped nil", v, err)
				}
			case "enabled":
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if _, ok := v.(*googleid.TokenVerifier); !ok {
					t.Fatalf("verifier = %T, want *googleid.TokenVerifier", v)
				}
			}
			if !strings.Contains(logs, `"status":"`+tc.want+`"`) {
				t.Errorf("startup log doesn't say %q: %s", tc.want, logs)
			}
		})
	}
}

// googleKeys is a local TLS stand-in for Google's key set, publishing one
// test key, so tests never reach Google.
type googleKeys struct {
	*httptest.Server
	key      *rsa.PrivateKey
	requests atomic.Int64
}

const testKid = "main-test-kid"

func newGoogleKeys(t *testing.T) *googleKeys {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"keys": []map[string]any{{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": testKid,
		"n": b64(key.N.Bytes()), "e": b64(big.NewInt(int64(key.E)).Bytes()),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	gk := &googleKeys{key: key}
	gk.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		gk.requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(body)
	}))
	t.Cleanup(gk.Close)
	return gk
}

func (gk *googleKeys) options() googleid.Options {
	return googleid.Options{CertsURL: gk.URL, HTTPClient: gk.Client()}
}

// token returns an ID token for aud, valid now, signed with the test key.
func (gk *googleKeys) token(t *testing.T, aud string) string {
	t.Helper()
	now := time.Now()
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": testKid, "typ": "JWT"})
	payload, _ := json.Marshal(map[string]any{
		"iss": "https://accounts.google.com", "aud": aud, "sub": "110169484474386276334",
		"email": "main.test@example.com", "email_verified": true,
		"iat": now.Add(-10 * time.Second).Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	input := b64(header) + "." + b64(payload)
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, gk.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + b64(sig)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// The verifier main builds accepts exactly the configured audience.
func TestGoogleVerifierUsesConfiguredAudience(t *testing.T) {
	gk := newGoogleKeys(t)
	v, _, err := googleVerifier(t, config.Config{Env: "production", GoogleClientID: testClientID}, gk.options())
	if err != nil {
		t.Fatal(err)
	}

	claims, err := v.Verify(context.Background(), gk.token(t, testClientID))
	if err != nil {
		t.Fatalf("token for the configured audience rejected: %v", err)
	}
	if claims.Subject() != "110169484474386276334" {
		t.Error("wrong claims returned")
	}

	for _, aud := range []string{
		"111111111111-other.apps.googleusercontent.com", // another client
		strings.ToUpper(testClientID),                   // exact match only
		testClientID + " ",
	} {
		_, err := v.Verify(context.Background(), gk.token(t, aud))
		var invalid *googleid.InvalidTokenError
		if !errors.As(err, &invalid) || invalid.Reason != googleid.ReasonWrongAudience {
			t.Errorf("aud %q: err = %v, want %s", aud, err, googleid.ReasonWrongAudience)
		}
	}
	if n := gk.requests.Load(); n != 1 {
		t.Errorf("key set fetched %d times, want 1", n)
	}
}

// auth.Service, wired as run wires it, verifies with the real verifier: a
// token for another audience is rejected by the verifier (wrong_audience),
// not as "not configured". The verifier rejects it before any database work,
// so no pool is needed.
func TestAuthServiceUsesConfiguredVerifier(t *testing.T) {
	gk := newGoogleKeys(t)
	for _, tc := range []struct {
		name       string
		cfg        config.Config
		wantReason string
	}{
		{"configured", config.Config{Env: "production", GoogleClientID: testClientID}, "wrong_audience"},
		{"disabled", config.Config{Env: "development"}, "not_configured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := gk.requests.Load()
			v, _, err := googleVerifier(t, tc.cfg, gk.options())
			if err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			svc := auth.NewService(nil, nil, nil, slog.New(slog.NewJSONHandler(&logs, nil)), auth.AccountLimits{}, v)

			_, err = svc.SignInWithGoogle(context.Background(), gk.token(t, "111111111111-other.apps.googleusercontent.com"), "ua")
			if !errors.Is(err, auth.ErrInvalidGoogleToken) {
				t.Fatalf("err = %v, want ErrInvalidGoogleToken", err)
			}
			if !strings.Contains(logs.String(), `"reason":"`+tc.wantReason+`"`) {
				t.Errorf("log reason isn't %s: %s", tc.wantReason, logs.String())
			}
			fetched := gk.requests.Load() > before
			if fetched != (tc.wantReason == "wrong_audience") {
				t.Errorf("key set fetched: %v", fetched)
			}
		})
	}
}

// run refuses to start production without a usable GOOGLE_CLIENT_ID, before
// it connects to the database (DATABASE_URL points nowhere), and its error
// never echoes the value.
func TestRunRefusesProductionWithoutGoogleClientID(t *testing.T) {
	for _, tc := range []struct{ name, id string }{
		{"missing", ""},
		{"client secret", "GOCSPX-clientidmarker"},
		{"wrong suffix", "987654321098-clientidmarker.googleusercontent.com"},
		{"trailing newline", testClientID + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range map[string]string{
				"ENV": "production", "DATABASE_URL": "postgres://u:p@192.0.2.1:5432/voca?connect_timeout=1",
				"APP_BASE_URL": "https://api.example.com", "TRUSTED_PROXY_HOPS": "1", "HTTP_ADDR": "127.0.0.1:0",
				"RESEND_API_KEY": testKey, "EMAIL_FROM": testFrom, "GOOGLE_CLIENT_ID": tc.id,
			} {
				t.Setenv(k, v)
			}
			var logs bytes.Buffer
			start := time.Now()
			err := run(slog.New(slog.NewJSONHandler(&logs, nil)))
			if err == nil {
				t.Fatal("run started production without a usable GOOGLE_CLIENT_ID")
			}
			if !strings.Contains(err.Error(), "GOOGLE_CLIENT_ID") {
				t.Errorf("error doesn't name GOOGLE_CLIENT_ID: %v", err)
			}
			if strings.Contains(err.Error()+logs.String(), "clientidmarker") {
				t.Errorf("value echoed: %v %s", err, logs.String())
			}
			if d := time.Since(start); d > 500*time.Millisecond {
				t.Errorf("run took %v: it reached for the database", d)
			}
		})
	}
}
