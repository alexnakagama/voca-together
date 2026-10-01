package googleid

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultCertsURL is Google's signing key set (JWKS) for ID tokens.
const DefaultCertsURL = "https://www.googleapis.com/oauth2/v3/certs"

const (
	// fetchTimeout bounds one key-set download end to end.
	fetchTimeout = 5 * time.Second
	// maxJWKSBytes caps the key-set response (Google's is about 1 KiB).
	maxJWKSBytes = 64 << 10
	// maxJWKSKeys caps the entries of a key set (Google publishes 2 or 3).
	maxJWKSKeys = 16

	// A fetched set is fresh for the response's Cache-Control max-age,
	// clamped to [minCacheLifetime, maxCacheLifetime]; defaultCacheLifetime
	// without a usable max-age. The lower bound also bounds outbound traffic
	// should Google ever send max-age=0 or no-cache.
	minCacheLifetime     = 5 * time.Minute
	maxCacheLifetime     = 24 * time.Hour
	defaultCacheLifetime = time.Hour
	// staleGrace is how long past its freshness a set may still be used, and
	// only while refreshing it fails: it rides out a Google or network outage
	// without trusting withdrawn keys indefinitely.
	staleGrace = 6 * time.Hour
	// minRefetchInterval is the least time between two fetch attempts,
	// successful or not. Unknown kids are attacker-chosen, so without it every
	// junk token would make us call Google; with it, at most one call a minute.
	minRefetchInterval = time.Minute

	// RSA modulus bounds. The minimum is NIST's and what Google uses; the
	// maximum bounds the CPU cost of one verification.
	minRSABits = 2048
	maxRSABits = 8192
	// maxKidBytes bounds key ids (Google's are 40 hex digits).
	maxKidBytes = 256
)

// Fetch failure reasons, as reported in UnavailableError.
const (
	failTransport    = "transport"
	failTimeout      = "timeout"
	failStatus       = "status"
	failContentType  = "content_type"
	failTooLarge     = "too_large"
	failMalformed    = "malformed_jwks"
	failNoUsableKeys = "no_usable_keys"
)

// keyCache holds Google's signing keys. It fetches lazily, when a token
// needs a key it doesn't have fresh, and never in the background on its own.
//
// At most one fetch runs at a time. It runs in its own goroutine with its own
// timeout, detached from the request that started it: a client that
// disconnects stops waiting (its context ends) without aborting the fetch
// that other requests are waiting for. So there is at most one such
// goroutine, and it lives at most fetchTimeout.
//
// Durations are measured with the injected clock. time.Now carries a
// monotonic reading, so with the real clock wall-clock jumps don't affect
// freshness or throttling.
type keyCache struct {
	url     string
	client  *http.Client
	now     func() time.Time
	timeout time.Duration // fetchTimeout; shorter in tests

	mu         sync.Mutex
	keys       map[string]*rsa.PublicKey // nil until the first successful fetch
	freshUntil time.Time
	staleUntil time.Time // freshUntil + staleGrace
	attempted  bool
	// lastAttempt is when the last fetch started; lastFailure is its failure
	// reason, or "" if it succeeded (or none ran yet).
	lastAttempt time.Time
	lastFailure string
	inflight    chan struct{} // non-nil while a fetch runs; closed when it ends
}

// key returns Google's key with id kid:
//   - a fresh set that has the key answers at once;
//   - otherwise one fetch is started if the throttle allows (a stale or
//     missing set, or an unknown kid that might be a newly rotated key), and
//     the caller waits for it, or for its own context to end;
//   - then it decides from what the cache holds: a key from a set within its
//     stale grace is used; an unknown kid in a usable set is ReasonUnknownKey
//     if the set is current, or unavailable if refreshing it just failed
//     (the kid might be a rotated key we couldn't fetch); no usable set at
//     all is unavailable.
func (c *keyCache) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	waited := false
	for {
		c.mu.Lock()
		now := c.now()
		k, known := c.keys[kid]
		if known && now.Before(c.freshUntil) {
			c.mu.Unlock()
			return k, nil
		}
		if !waited {
			if c.inflight == nil && c.mayFetch(now) {
				c.startFetch(now)
			}
			if ch := c.inflight; ch != nil {
				c.mu.Unlock()
				select {
				case <-ch:
					waited = true
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		}
		usable := c.keys != nil && now.Before(c.staleUntil)
		failure := c.lastFailure
		c.mu.Unlock()

		switch {
		case known && usable:
			return k, nil
		case !usable && failure == "":
			return nil, &UnavailableError{Reason: failNoUsableKeys}
		case failure != "":
			return nil, &UnavailableError{Reason: failure}
		default:
			return nil, invalid(ReasonUnknownKey)
		}
	}
}

// mayFetch reports whether the throttle allows a fetch now. Callers hold mu.
func (c *keyCache) mayFetch(now time.Time) bool {
	return !c.attempted || now.Sub(c.lastAttempt) >= minRefetchInterval
}

// startFetch starts the single fetch goroutine. Callers hold mu.
func (c *keyCache) startFetch(now time.Time) {
	c.attempted = true
	c.lastAttempt = now
	done := make(chan struct{})
	c.inflight = done
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
		defer cancel()
		keys, lifetime, failure := c.download(ctx)

		c.mu.Lock()
		defer c.mu.Unlock()
		if failure == "" {
			// A successful fetch replaces the whole set: a key Google
			// withdrew stops verifying now.
			now := c.now()
			c.keys = keys
			c.freshUntil = now.Add(lifetime)
			c.staleUntil = c.freshUntil.Add(staleGrace)
		}
		c.lastFailure = failure
		c.inflight = nil
		close(done)
	}()
}

// download fetches and parses the key set. Failures are reported only as
// fixed reasons: transport errors name the URL, and bodies are the
// provider's, so neither is kept.
func (c *keyCache) download(ctx context.Context) (map[string]*rsa.PublicKey, time.Duration, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, 0, failTransport
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "vocatogether-backend")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, transportFailure(err)
	}
	defer resp.Body.Close()
	// Redirects are not followed (see newHTTPClient), so they end here too.
	if resp.StatusCode != http.StatusOK {
		return nil, 0, failStatus
	}
	if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return nil, 0, failContentType
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBytes+1))
	if err != nil {
		return nil, 0, transportFailure(err)
	}
	if len(body) > maxJWKSBytes {
		return nil, 0, failTooLarge
	}
	keys, failure := parseKeySet(body)
	if failure != "" {
		return nil, 0, failure
	}
	return keys, cacheLifetime(resp.Header.Values("Cache-Control")), ""
}

func transportFailure(err error) string {
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return failTimeout
	}
	return failTransport
}

// parseKeySet returns the usable RS256 keys of a JWKS document by kid.
// Entries for other key types or uses, and RSA keys this package wouldn't
// verify with (see parseKey), are skipped. The whole set is rejected if it
// isn't a well-formed {"keys":[…]} object, has more than maxJWKSKeys
// entries, gives one kid to two usable keys (which one would sign is
// ambiguous), or has no usable key.
func parseKeySet(body []byte) (map[string]*rsa.PublicKey, string) {
	top, err := parseObject(body)
	if err != nil {
		return nil, failMalformed
	}
	raw, ok := top["keys"]
	if !ok || len(raw) == 0 || raw[0] != '[' {
		return nil, failMalformed
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil || len(entries) > maxJWKSKeys {
		return nil, failMalformed
	}
	keys := make(map[string]*rsa.PublicKey, len(entries))
	for _, entry := range entries {
		members, err := parseObject(entry)
		if err != nil {
			return nil, failMalformed
		}
		kid, key, ok := parseKey(members)
		if !ok {
			continue
		}
		if _, dup := keys[kid]; dup {
			return nil, failMalformed
		}
		keys[kid] = key
	}
	if len(keys) == 0 {
		return nil, failNoUsableKeys
	}
	return keys, ""
}

// parseKey returns the key of a JWK if it is usable for RS256 verification:
// kty "RSA"; alg absent or "RS256"; use absent or "sig"; no key_ops (not
// interpreted, so not trusted); a kid of printable ASCII; an odd modulus of
// minRSABits to maxRSABits bits; and the exponent 65537 ("AQAB", the only
// one Google uses). Every value must be a string in strict base64url where
// encoded.
func parseKey(m map[string]json.RawMessage) (kid string, key *rsa.PublicKey, ok bool) {
	str := func(name string) (string, bool) { return jsonString(m[name]) }
	optional := func(name, want string) bool {
		raw, present := m[name]
		if !present {
			return true
		}
		s, ok := jsonString(raw)
		return ok && s == want
	}

	if kty, ok := str("kty"); !ok || kty != "RSA" {
		return "", nil, false
	}
	if !optional("alg", "RS256") || !optional("use", "sig") {
		return "", nil, false
	}
	if _, present := m["key_ops"]; present {
		return "", nil, false
	}
	kid, ok = str("kid")
	if !ok || !printableASCII(kid, maxKidBytes) {
		return "", nil, false
	}
	if e, ok := str("e"); !ok || e != "AQAB" {
		return "", nil, false
	}
	nb64, ok := str("n")
	if !ok || len(nb64) > base64.RawURLEncoding.EncodedLen(maxRSABits/8) {
		return "", nil, false
	}
	nb, err := base64.RawURLEncoding.Strict().DecodeString(nb64)
	if err != nil {
		return "", nil, false
	}
	n := new(big.Int).SetBytes(nb)
	if bits := n.BitLen(); bits < minRSABits || bits > maxRSABits || n.Bit(0) == 0 {
		return "", nil, false
	}
	return kid, &rsa.PublicKey{N: n, E: 65537}, true
}

// cacheLifetime returns how long a fetched set stays fresh, from the
// response's Cache-Control max-age, clamped to [minCacheLifetime,
// maxCacheLifetime]. A missing, repeated or malformed max-age (not plain
// digits) gives defaultCacheLifetime; one too large to parse is the maximum.
// Other directives are ignored.
func cacheLifetime(headers []string) time.Duration {
	seconds, found := int64(-1), false
	for _, h := range headers {
		for _, directive := range strings.Split(h, ",") {
			name, value, _ := strings.Cut(strings.TrimSpace(directive), "=")
			if !strings.EqualFold(strings.TrimSpace(name), "max-age") {
				continue
			}
			if found {
				return defaultCacheLifetime // conflicting or repeated
			}
			found = true
			value = strings.TrimSpace(value)
			if value == "" || strings.Trim(value, "0123456789") != "" {
				return defaultCacheLifetime
			}
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				n = int64(maxCacheLifetime / time.Second) // digits only, so out of range
			}
			seconds = n
		}
	}
	if !found {
		return defaultCacheLifetime
	}
	if seconds >= int64(maxCacheLifetime/time.Second) {
		return maxCacheLifetime
	}
	return max(time.Duration(seconds)*time.Second, minCacheLifetime)
}

// newHTTPClient returns a copy of base (or of a default client) hardened for
// key fetches: redirects are never followed (a 3xx fails the fetch, so a
// redirect can't send us to another host), no cookie jar, and a timeout of
// at most fetchTimeout. Proxy settings are the transport's, which for the
// default client means the standard environment variables.
func newHTTPClient(base *http.Client) *http.Client {
	var c http.Client
	if base != nil {
		c = *base
	} else {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSHandshakeTimeout = fetchTimeout
		transport.ResponseHeaderTimeout = fetchTimeout
		c.Transport = transport
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.Jar = nil
	if c.Timeout <= 0 || c.Timeout > fetchTimeout {
		c.Timeout = fetchTimeout
	}
	return &c
}
