package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
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

// HashToken returns the SHA-256 of the full raw token. A fast hash is
// appropriate here (unlike passwords) because the input has 256 bits of entropy,
// so brute-forcing a leaked hash is infeasible.
func HashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// String, GoString and LogValue keep the secret out of logs, error messages
// and debug output (%v, %+v, %s, %#v and slog).
func (Token) String() string { return "[REDACTED]" }

func (Token) GoString() string { return "auth.Token{[REDACTED]}" }

func (Token) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }
