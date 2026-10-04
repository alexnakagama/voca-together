// Package googleid verifies Google ID tokens locally (decision 020).
//
// It checks one narrow token profile, the one Google issues to Sign in with
// Google clients: a compact JWS signed with RS256 by a key from Google's
// published key set, issued by accounts.google.com for one of the configured
// audiences, unexpired, with a well-formed subject. Anything else is rejected.
// It is deliberately not a general JWT library.
//
// It returns only the claims the auth layer needs and knows nothing about
// users, sessions, databases or HTTP routes. Account policy (which emails may
// create accounts) belongs to the caller. A token verifies every time it is
// presented until it expires: nothing here or in the caller remembers it
// (docs/decisions.md 026).
//
// Not checked, on purpose:
//   - azp: for Android it names one of our own Android OAuth clients. Google
//     mints tokens with our audience only for clients registered in our
//     Google Cloud project, so azp would only choose among our own clients;
//     aud is the security boundary. This holds only while that project
//     contains VocaTogether's clients alone and its Web client has no
//     authorized JavaScript origins or redirect URIs. Adding any other client
//     to the project requires revisiting this. azp is never returned, so it
//     can't be used as an identity either.
//   - nonce: the official Flutter plugin sets one nonce per process, not per
//     sign-in, so a nonce couldn't bind a token to one request. The caller
//     enforces single use instead.
package googleid

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"
)

// Verifier verifies a raw Google ID token and returns its claims. Errors are
// an *InvalidTokenError (errors.Is ErrInvalidToken) for a token that must be
// rejected, an *UnavailableError (errors.Is ErrUnavailable) when Google's
// keys can't be obtained, or the context's error.
type Verifier interface {
	Verify(ctx context.Context, raw string) (Claims, error)
}

// Claims are the verified claims of a Google ID token that the auth layer
// uses. Subject is Google's stable account id ("sub"); Email, EmailVerified
// and HostedDomain are as Google asserted them, without normalization or
// policy, and are empty or false when absent. AcceptedUntil is when the
// verifier stops accepting the token (exp plus the clock skew it tolerates):
// Verify succeeds for it only while now is before AcceptedUntil. No caller
// uses it today; it states the window in which the token is accepted.
//
// Claims hold personal data, so they never print: every fmt verb, slog,
// JSON and text marshalling show [REDACTED]. The values sit behind a pointer
// so that fmt's reflection fallback (%p on a value, which fmt never passes to
// Format) finds only an address. Read them through the methods.
type Claims struct {
	v *claimValues
}

type claimValues struct {
	subject       string
	email         string
	emailVerified bool
	hostedDomain  string
	acceptedUntil time.Time
}

// NewClaims returns Claims holding the given values. Verify builds them from
// a verified token; tests use it to configure a Fake.
func NewClaims(subject, email string, emailVerified bool, hostedDomain string, acceptedUntil time.Time) Claims {
	return Claims{v: &claimValues{
		subject: subject, email: email, emailVerified: emailVerified,
		hostedDomain: hostedDomain, acceptedUntil: acceptedUntil,
	}}
}

func (c Claims) Subject() string {
	if c.v == nil {
		return ""
	}
	return c.v.subject
}

func (c Claims) Email() string {
	if c.v == nil {
		return ""
	}
	return c.v.email
}

func (c Claims) EmailVerified() bool {
	return c.v != nil && c.v.emailVerified
}

func (c Claims) HostedDomain() string {
	if c.v == nil {
		return ""
	}
	return c.v.hostedDomain
}

func (c Claims) AcceptedUntil() time.Time {
	if c.v == nil {
		return time.Time{}
	}
	return c.v.acceptedUntil
}

const redacted = "[REDACTED]"

func (Claims) Format(f fmt.State, _ rune)   { io.WriteString(f, "googleid.Claims"+redacted) }
func (Claims) String() string               { return redacted }
func (Claims) GoString() string             { return "googleid.Claims" + redacted }
func (Claims) LogValue() slog.Value         { return slog.StringValue(redacted) }
func (Claims) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }
func (Claims) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// Reason says why a token was rejected. Reasons are fixed strings, safe to
// log; they never carry token contents. Clients must not see them.
type Reason string

const (
	ReasonMalformed      Reason = "malformed"        // not a well-formed token of the expected profile
	ReasonUnsupportedAlg Reason = "unsupported_alg"  // alg missing or not RS256
	ReasonUnknownKey     Reason = "unknown_key"      // kid not in Google's current key set
	ReasonBadSignature   Reason = "bad_signature"    // signature doesn't verify
	ReasonWrongIssuer    Reason = "wrong_issuer"     // iss missing or not Google
	ReasonWrongAudience  Reason = "wrong_audience"   // aud missing, not a string, or not configured
	ReasonExpired        Reason = "expired"          // exp passed (with clock skew)
	ReasonIssuedInFuture Reason = "issued_in_future" // iat or nbf after now (with clock skew)
	ReasonBadSubject     Reason = "bad_subject"      // sub missing or malformed
	ReasonBadClaims      Reason = "bad_claims"       // a claim has the wrong type or range
)

// ErrInvalidToken is matched (errors.Is) by every *InvalidTokenError.
var ErrInvalidToken = errors.New("googleid: invalid token")

// InvalidTokenError rejects a token. Its message is the fixed reason only.
type InvalidTokenError struct {
	Reason Reason
}

func (e *InvalidTokenError) Error() string        { return "googleid: invalid token: " + string(e.Reason) }
func (e *InvalidTokenError) Is(target error) bool { return target == ErrInvalidToken }

func invalid(r Reason) error { return &InvalidTokenError{Reason: r} }

// ErrUnavailable is matched (errors.Is) by every *UnavailableError.
var ErrUnavailable = errors.New("googleid: unavailable")

// UnavailableError means no usable Google key could be obtained: the key set
// couldn't be fetched and no cached set is within its grace period. The
// token may be valid; nothing about it was decided. Reason names the last
// fetch failure with a fixed string ("transport", "timeout", "status",
// "content_type", "too_large", "malformed_jwks", "no_usable_keys"), never a
// URL, header, body or transport message.
type UnavailableError struct {
	Reason string
}

func (e *UnavailableError) Error() string        { return "googleid: unavailable: " + e.Reason }
func (e *UnavailableError) Is(target error) bool { return target == ErrUnavailable }
