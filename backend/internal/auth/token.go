package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"strings"
)

// Prefixes make leaked tokens recognizable (grep, secret scanners) and let
// the server reject a refresh token presented as an access token, and vice versa.
const (
	AccessTokenPrefix  = "vt_at_"
	RefreshTokenPrefix = "vt_rt_"
)

const tokenBytes = 32 // 256 bits of entropy

// Token is a freshly generated secret. Raw goes to the client (response body
// or emailed link) exactly once; only Hash is stored.
type Token struct {
	Raw  string
	Hash []byte
}

// NewToken returns prefix followed by 32 random bytes as unpadded base64url
// (URL-safe without escaping).
func NewToken(prefix string) Token {
	b := make([]byte, tokenBytes)
	rand.Read(b) // never fails: since Go 1.24 it crashes the program rather than return weak randomness
	raw := prefix + base64.RawURLEncoding.EncodeToString(b)
	return Token{Raw: raw, Hash: HashToken(raw)}
}

// wellFormedToken reports whether raw has exactly the shape NewToken(prefix)
// produces: the prefix, then 32 bytes as canonical unpadded base64url. Callers
// use it to reject junk before any database lookup; the format is public, so
// this reveals nothing.
func wellFormedToken(raw, prefix string) bool {
	body, found := strings.CutPrefix(raw, prefix)
	if !found || len(body) != base64.RawURLEncoding.EncodedLen(tokenBytes) {
		return false
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(body)
	return err == nil && len(b) == tokenBytes
}

// HashToken returns the SHA-256 of the full raw token. A fast hash is
// appropriate here (unlike passwords) because the input has 256 bits of entropy,
// so brute-forcing a leaked hash is infeasible.
func HashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// String ..., GoString, LogValue and MarshalJSON keep the secret out of logs,
// error messages and debug output (%v, %+v, %s, %#v, slog and JSON). JSON
// needs its own method: encoding/json ignores String, and slog's JSON handler
// encodes values nested in structs with encoding/json.
func (Token) String() string { return "[REDACTED]" }

func (Token) GoString() string { return "auth.Token{[REDACTED]}" }

func (Token) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

func (Token) MarshalJSON() ([]byte, error) { return []byte(`"[REDACTED]"`), nil }
