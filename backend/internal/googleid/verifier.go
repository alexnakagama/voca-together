package googleid

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// maxTokenBytes bounds a raw token before any decoding (Google's are
	// about 1 KiB). It also bounds every segment and parsed object.
	maxTokenBytes = 4096
	// clockSkew is tolerated on exp, iat and nbf.
	clockSkew = time.Minute
	// maxSubjectBytes is Google's documented maximum for sub.
	maxSubjectBytes = 255
	// maxAudienceBytes bounds configured audiences (client IDs are ~72 bytes).
	maxAudienceBytes = 255
)

// googleIssuers are the two spellings Google documents for iss.
var googleIssuers = map[string]bool{"https://accounts.google.com": true, "accounts.google.com": true}

// Options configures NewTokenVerifier. The zero value is production: Google's
// key set over the default hardened client and the real clock. Tests inject
// a local key server, its client and a fake clock.
type Options struct {
	// CertsURL is the key set (JWKS) URL; it must be https. Default:
	// DefaultCertsURL. It is fixed configuration: nothing in a token can
	// point the verifier at another URL (jku, x5u and jwk are rejected).
	CertsURL string
	// HTTPClient is copied and hardened (see newHTTPClient); nil for the
	// default.
	HTTPClient *http.Client
	// Now is the clock; nil for time.Now.
	Now func() time.Time
}

// TokenVerifier is the production Verifier. It is safe for concurrent use.
type TokenVerifier struct {
	audiences map[string]bool
	keys      *keyCache
	now       func() time.Time
}

var _ Verifier = (*TokenVerifier)(nil)

// NewTokenVerifier returns a verifier accepting tokens whose aud is exactly
// one of audiences (the OAuth client IDs tokens are minted for: for Android,
// the Web client ID passed as serverClientId). It does no network work;
// keys are fetched on first use. Errors never echo the arguments.
func NewTokenVerifier(audiences []string, opts Options) (*TokenVerifier, error) {
	if len(audiences) == 0 {
		return nil, errors.New("googleid: at least one audience is required")
	}
	set := make(map[string]bool, len(audiences))
	for _, aud := range audiences {
		if !printableASCII(aud, maxAudienceBytes) {
			return nil, errors.New("googleid: invalid audience")
		}
		set[aud] = true
	}

	certsURL := opts.CertsURL
	if certsURL == "" {
		certsURL = DefaultCertsURL
	}
	u, err := url.Parse(certsURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, errors.New("googleid: certs URL must be an https URL without credentials")
	}

	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &TokenVerifier{
		audiences: set,
		keys:      &keyCache{url: certsURL, client: newHTTPClient(opts.HTTPClient), now: now, timeout: fetchTimeout},
		now:       now,
	}, nil
}

// Verify checks raw and returns its claims. The order of work bounds what
// an attacker can make it do: size, structure, header and payload syntax are
// checked before any key lookup (so malformed tokens never cause a fetch),
// the signature before any claim is trusted, and only then the claims.
func (v *TokenVerifier) Verify(ctx context.Context, raw string) (Claims, error) {
	if err := ctx.Err(); err != nil {
		return Claims{}, err
	}
	tok, err := parseToken(raw)
	if err != nil {
		return Claims{}, err
	}
	key, err := v.keys.key(ctx, tok.kid)
	if err != nil {
		return Claims{}, err
	}
	// The algorithm is fixed here, never taken from the token: RSASSA-PKCS1-v1_5
	// with SHA-256 over the exact received "header.payload" bytes.
	digest := sha256.Sum256([]byte(tok.signingInput))
	if len(tok.signature) != key.Size() || rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], tok.signature) != nil {
		return Claims{}, invalid(ReasonBadSignature)
	}
	return v.claims(tok.payload)
}

// parsedToken is a structurally valid token whose signature isn't checked yet.
type parsedToken struct {
	kid          string
	signingInput string
	payload      map[string]json.RawMessage
	signature    []byte
}

// parseToken checks everything that needs no key: the compact serialization
// (exactly three non-empty segments of strict unpadded base64url), a header
// that is exactly {"alg":"RS256","kid":…[,"typ":"JWT"]} (any other member,
// such as crit, jku, x5u, jwk, x5c, zip or b64, is rejected rather than
// ignored), a payload that is one JSON object, and a signature whose length
// fits an RSA key we would accept.
func parseToken(raw string) (parsedToken, error) {
	if raw == "" || len(raw) > maxTokenBytes {
		return parsedToken{}, invalid(ReasonMalformed)
	}
	// Count first, so a token of many dots costs no allocations.
	if strings.Count(raw, ".") != 2 {
		return parsedToken{}, invalid(ReasonMalformed)
	}
	parts := strings.Split(raw, ".")
	if parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return parsedToken{}, invalid(ReasonMalformed)
	}
	b64 := base64.RawURLEncoding.Strict()
	headerJSON, err1 := b64.DecodeString(parts[0])
	payloadJSON, err2 := b64.DecodeString(parts[1])
	signature, err3 := b64.DecodeString(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return parsedToken{}, invalid(ReasonMalformed)
	}

	header, err := parseObject(headerJSON)
	if err != nil {
		return parsedToken{}, invalid(ReasonMalformed)
	}
	for name := range header {
		if name != "alg" && name != "kid" && name != "typ" {
			return parsedToken{}, invalid(ReasonMalformed)
		}
	}
	if alg, ok := jsonString(header["alg"]); !ok || alg != "RS256" {
		return parsedToken{}, invalid(ReasonUnsupportedAlg)
	}
	kid, ok := jsonString(header["kid"])
	if !ok || !printableASCII(kid, maxKidBytes) {
		return parsedToken{}, invalid(ReasonMalformed)
	}
	if raw, present := header["typ"]; present {
		if typ, ok := jsonString(raw); !ok || typ != "JWT" {
			return parsedToken{}, invalid(ReasonMalformed)
		}
	}

	payload, err := parseObject(payloadJSON)
	if err != nil {
		return parsedToken{}, invalid(ReasonMalformed)
	}
	if len(signature) < minRSABits/8 || len(signature) > maxRSABits/8 {
		return parsedToken{}, invalid(ReasonBadSignature)
	}
	return parsedToken{
		kid:          kid,
		signingInput: raw[:len(parts[0])+1+len(parts[1])],
		payload:      payload,
		signature:    signature,
	}, nil
}

// claims validates the signed payload. Every claim must have exactly the
// expected JSON type: nothing is coerced (no "true" for true, no "123" for
// 123, no array for aud). Unknown claims, including azp and nonce, are
// ignored and never returned.
func (v *TokenVerifier) claims(p map[string]json.RawMessage) (Claims, error) {
	if iss, ok := jsonString(p["iss"]); !ok || !googleIssuers[iss] {
		return Claims{}, invalid(ReasonWrongIssuer)
	}
	// Exact match: no trimming, case folding or prefix matching.
	if aud, ok := jsonString(p["aud"]); !ok || !v.audiences[aud] {
		return Claims{}, invalid(ReasonWrongAudience)
	}

	exp, ok1 := jsonUnixSeconds(p["exp"])
	iat, ok2 := jsonUnixSeconds(p["iat"])
	if !ok1 || !ok2 || exp <= iat {
		return Claims{}, invalid(ReasonBadClaims)
	}
	var nbf int64
	rawNbf, hasNbf := p["nbf"]
	if hasNbf {
		var ok bool
		if nbf, ok = jsonUnixSeconds(rawNbf); !ok {
			return Claims{}, invalid(ReasonBadClaims)
		}
	}
	// Timestamps are at most year 9999, so these never overflow. A token is
	// valid until exp + clockSkew, exclusive.
	now := v.now()
	if !now.Before(time.Unix(exp, 0).Add(clockSkew)) {
		return Claims{}, invalid(ReasonExpired)
	}
	notAfter := now.Add(clockSkew)
	if time.Unix(iat, 0).After(notAfter) || (hasNbf && time.Unix(nbf, 0).After(notAfter)) {
		return Claims{}, invalid(ReasonIssuedInFuture)
	}

	// Kept exactly as issued: sub is the identity key, never normalized.
	sub, ok := jsonString(p["sub"])
	if !ok || !printableASCII(sub, maxSubjectBytes) {
		return Claims{}, invalid(ReasonBadSubject)
	}

	var email, hd string
	var emailVerified bool
	if raw, present := p["email"]; present {
		if email, ok = jsonString(raw); !ok {
			return Claims{}, invalid(ReasonBadClaims)
		}
	}
	if raw, present := p["email_verified"]; present {
		if emailVerified, ok = jsonBool(raw); !ok {
			return Claims{}, invalid(ReasonBadClaims)
		}
	}
	if raw, present := p["hd"]; present {
		if hd, ok = jsonString(raw); !ok {
			return Claims{}, invalid(ReasonBadClaims)
		}
	}
	return NewClaims(sub, email, emailVerified, hd), nil
}
