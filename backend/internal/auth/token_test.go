package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestNewTokenFormat(t *testing.T) {
	for _, prefix := range []string{AccessTokenPrefix, RefreshTokenPrefix, ""} {
		tok := NewToken(prefix)

		if !strings.HasPrefix(tok.Raw, prefix) {
			t.Errorf("raw %q lacks prefix %q", tok.Raw, prefix)
		}
		body := strings.TrimPrefix(tok.Raw, prefix)
		if len(body) != 43 {
			t.Errorf("token body length = %d, want 43 (32 bytes, unpadded base64url)", len(body))
		}
		decoded, err := base64.RawURLEncoding.DecodeString(body)
		if err != nil || len(decoded) != 32 {
			t.Errorf("token body is not 32 bytes of base64url: %d bytes, err %v", len(decoded), err)
		}

		sum := sha256.Sum256([]byte(tok.Raw))
		if !bytes.Equal(tok.Hash, sum[:]) {
			t.Error("Hash is not SHA-256 of the full raw token")
		}
		if len(tok.Hash) != 32 {
			t.Errorf("hash length = %d, want 32 (matches the DB CHECK)", len(tok.Hash))
		}
	}
}

// Tokens must be safe to put in URLs without escaping.
func TestNewTokenIsURLSafe(t *testing.T) {
	for range 200 {
		raw := NewToken("").Raw
		if strings.ContainsAny(raw, "+/=%?&#") {
			t.Fatalf("token %q contains URL-unsafe characters", raw)
		}
	}
}

func TestNewTokenIsUnique(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for range 1000 {
		raw := NewToken(RefreshTokenPrefix).Raw
		if seen[raw] {
			t.Fatalf("duplicate token generated: %q", raw)
		}
		seen[raw] = true
	}
}

func TestHashToken(t *testing.T) {
	tok := NewToken(AccessTokenPrefix)
	if !bytes.Equal(HashToken(tok.Raw), tok.Hash) {
		t.Error("HashToken(raw) differs from the hash returned at creation")
	}
	if !bytes.Equal(HashToken("abc"), HashToken("abc")) {
		t.Error("HashToken is not deterministic")
	}
	if bytes.Equal(HashToken("abc"), HashToken("abd")) {
		t.Error("different inputs produced the same hash")
	}
}

func TestTokenPrefixesAreDistinct(t *testing.T) {
	if AccessTokenPrefix == RefreshTokenPrefix {
		t.Fatal("access and refresh prefixes must differ so one can't be mistaken for the other")
	}
	if AccessTokenPrefix != "vt_at_" || RefreshTokenPrefix != "vt_rt_" {
		t.Errorf("prefixes = %q, %q; want vt_at_, vt_rt_", AccessTokenPrefix, RefreshTokenPrefix)
	}
}

// Accidentally logging a Token (fmt or slog) must not leak the secret.
func TestTokenIsRedactedWhenFormattedOrLogged(t *testing.T) {
	tok := NewToken(RefreshTokenPrefix)
	nested := struct{ Tok Token }{tok}

	formats := map[string]string{
		"Sprint":      fmt.Sprint(tok),
		"%v":          fmt.Sprintf("%v", tok),
		"%+v":         fmt.Sprintf("%+v", tok),
		"%s":          fmt.Sprintf("%s", tok),
		"%#v":         fmt.Sprintf("%#v", tok),
		"%#v pointer": fmt.Sprintf("%#v", &tok),
		"%+v nested":  fmt.Sprintf("%+v", nested),
		"%#v nested":  fmt.Sprintf("%#v", nested),
	}
	for verb, s := range formats {
		if strings.Contains(s, tok.Raw) {
			t.Errorf("%s leaks the raw token: %q", verb, s)
		}
	}

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("issued", "token", tok)
	if strings.Contains(buf.String(), tok.Raw) {
		t.Errorf("slog output leaks raw token: %s", buf.String())
	}
}

func TestWellFormedTokenAcceptsGeneratedTokens(t *testing.T) {
	for _, prefix := range []string{AccessTokenPrefix, RefreshTokenPrefix, ""} {
		for range 100 {
			if tok := NewToken(prefix); !wellFormedToken(tok.Raw, prefix) {
				t.Fatalf("rejected generated token with prefix %q", prefix)
			}
		}
	}
}

func TestWellFormedTokenRejectsMalformed(t *testing.T) {
	valid := NewToken("").Raw
	tests := map[string]string{
		"empty":              "",
		"one char short":     valid[:42],
		"one char long":      valid + "A",
		"padded":             valid + "=",
		"standard base64":    "+" + valid[1:],
		"slash":              "/" + valid[1:],
		"space":              " " + valid[1:],
		"non-ASCII":          "ñ" + valid[2:],
		"hex SHA-256":        strings.Repeat("ab", 32),
		"non-canonical bits": valid[:42] + "B", // last char must encode 2 zero padding bits
		"wrong prefix":       AccessTokenPrefix + valid,
	}
	for name, raw := range tests {
		if wellFormedToken(raw, "") {
			t.Errorf("%s: %q accepted", name, raw)
		}
	}
	if wellFormedToken(valid, AccessTokenPrefix) {
		t.Error("token without the required prefix accepted")
	}
}
