package googleid

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestVerifyValidToken(t *testing.T) {
	f := newFixture(t)
	c, err := f.v.Verify(context.Background(), f.valid())
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject() != testSubject || c.Email() != testEmail || !c.EmailVerified() || c.HostedDomain() != testHD {
		t.Errorf("claims = %q %q %v %q", c.Subject(), c.Email(), c.EmailVerified(), c.HostedDomain())
	}
	// Both documented issuer spellings, and no typ header.
	h := map[string]any{"alg": "RS256", "kid": kidA}
	tok := sign(keyA, h, with(googleClaims(f.clock.Now()), "iss", "accounts.google.com"))
	if _, err := f.v.Verify(context.Background(), tok); err != nil {
		t.Errorf("iss accounts.google.com without typ: %v", err)
	}
}

// Absent optional claims are empty; they are not policy here.
func TestVerifyOptionalClaimsAbsent(t *testing.T) {
	f := newFixture(t)
	claims := with(googleClaims(f.clock.Now()), "email", nil, "email_verified", nil, "hd", nil)
	c, err := f.v.Verify(context.Background(), sign(keyA, header(kidA), claims))
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject() != testSubject || c.Email() != "" || c.EmailVerified() || c.HostedDomain() != "" {
		t.Errorf("claims = %q %q %v %q", c.Subject(), c.Email(), c.EmailVerified(), c.HostedDomain())
	}
	c, err = f.v.Verify(context.Background(), sign(keyA, header(kidA), with(claims, "email_verified", false)))
	if err != nil || c.EmailVerified() {
		t.Errorf("email_verified false: verified=%v err=%v", c.EmailVerified(), err)
	}
}

// azp and nonce are deliberately not checked (package doc) and never
// returned: any value, even of a strange type, leaves the result unchanged.
func TestVerifyIgnoresAzpAndNonce(t *testing.T) {
	f := newFixture(t)
	for _, extra := range []map[string]any{
		{"azp": "some-other-client.apps.googleusercontent.com"},
		{"azp": 42},
		{"nonce": "anything"},
	} {
		claims := googleClaims(f.clock.Now())
		for k, v := range extra {
			claims[k] = v
		}
		c, err := f.v.Verify(context.Background(), sign(keyA, header(kidA), claims))
		if err != nil || c.Subject() != testSubject {
			t.Errorf("%v: subject=%q err=%v", extra, c.Subject(), err)
		}
	}
}

// Everything checked before the key lookup: none of these may cause a key
// fetch, so the verifier here has an empty cache and the server must see no
// request at all.
func TestVerifyRejectsMalformedTokensWithoutFetching(t *testing.T) {
	f := newFixture(t)
	now := f.clock.Now()
	valid := f.valid()
	parts := strings.Split(valid, ".")
	claims := mustJSON(googleClaims(now))
	hdr := func(json string) string { return signRaw(keyA, json, claims) }
	payload := func(json string) string { return signRaw(keyA, mustJSON(header(kidA)), json) }

	// Changing the unused low bits of the last signature character gives the
	// same bytes in lenient base64; strict decoding must refuse it.
	sig := parts[2]
	last := strings.IndexByte(base64URLAlphabet, sig[len(sig)-1])
	nonCanonical := parts[0] + "." + parts[1] + "." + sig[:len(sig)-1] + string(base64URLAlphabet[last^1])

	tests := []struct {
		name  string
		token string
		want  Reason
	}{
		{"empty", "", ReasonMalformed},
		{"oversized", valid + strings.Repeat("A", maxTokenBytes), ReasonMalformed},
		{"just over 4096", strings.Repeat("a", maxTokenBytes+1), ReasonMalformed},
		{"two segments", parts[0] + "." + parts[1], ReasonMalformed},
		{"four segments", valid + ".AAAA", ReasonMalformed},
		{"empty header segment", "." + parts[1] + "." + parts[2], ReasonMalformed},
		{"empty payload segment", parts[0] + ".." + parts[2], ReasonMalformed},
		{"empty signature segment", parts[0] + "." + parts[1] + ".", ReasonMalformed},
		{"invalid base64 alphabet", parts[0] + "." + parts[1] + "*." + parts[2], ReasonMalformed},
		{"standard base64 alphabet", strings.ReplaceAll(valid, "-", "+"), ReasonMalformed},
		{"padded header", parts[0] + "==." + parts[1] + "." + parts[2], ReasonMalformed},
		{"padded signature", valid + "=", ReasonMalformed},
		{"non-canonical base64", nonCanonical, ReasonMalformed},
		{"whitespace", " " + valid, ReasonMalformed},
		{"JWE (five segments)", "a.b.c.d.e", ReasonMalformed},

		{"header not JSON", hdr(`{"alg":"RS256"`), ReasonMalformed},
		{"header array", hdr(`["RS256"]`), ReasonMalformed},
		{"header scalar", hdr(`"RS256"`), ReasonMalformed},
		{"header null", hdr(`null`), ReasonMalformed},
		{"header trailing data", hdr(`{"alg":"RS256","kid":"` + kidA + `"}{}`), ReasonMalformed},
		{"header trailing garbage", hdr(`{"alg":"RS256","kid":"` + kidA + `"}x`), ReasonMalformed},
		{"header invalid UTF-8", hdr("{\"alg\":\"RS256\",\"kid\":\"" + kidA + "\",\"typ\":\"\xff\"}"), ReasonMalformed},
		{"duplicate alg", hdr(`{"alg":"RS256","alg":"none","kid":"` + kidA + `"}`), ReasonMalformed},
		{"duplicate alg, same value", hdr(`{"alg":"RS256","alg":"RS256","kid":"` + kidA + `"}`), ReasonMalformed},
		{"duplicate alg via escape", hdr(`{"alg":"RS256","alg":"RS256","kid":"` + kidA + `"}`), ReasonMalformed},
		{"duplicate kid", hdr(`{"alg":"RS256","kid":"` + kidA + `","kid":"` + kidB + `"}`), ReasonMalformed},

		{"missing alg", hdr(mustJSON(map[string]any{"kid": kidA})), ReasonUnsupportedAlg},
		{"alg none", hdr(mustJSON(with(header(kidA), "alg", "none"))), ReasonUnsupportedAlg},
		{"alg HS256", hdr(mustJSON(with(header(kidA), "alg", "HS256"))), ReasonUnsupportedAlg},
		{"alg RS512", hdr(mustJSON(with(header(kidA), "alg", "RS512"))), ReasonUnsupportedAlg},
		{"alg ES256", hdr(mustJSON(with(header(kidA), "alg", "ES256"))), ReasonUnsupportedAlg},
		{"alg PS256", hdr(mustJSON(with(header(kidA), "alg", "PS256"))), ReasonUnsupportedAlg},
		{"alg lowercase", hdr(mustJSON(with(header(kidA), "alg", "rs256"))), ReasonUnsupportedAlg},
		{"alg with space", hdr(mustJSON(with(header(kidA), "alg", "RS256 "))), ReasonUnsupportedAlg},
		{"alg not a string", hdr(mustJSON(with(header(kidA), "alg", 256))), ReasonUnsupportedAlg},
		{"alg array", hdr(mustJSON(with(header(kidA), "alg", []string{"RS256"}))), ReasonUnsupportedAlg},
		{"alg wrong case key", hdr(`{"ALG":"RS256","kid":"` + kidA + `"}`), ReasonMalformed},

		{"missing kid", hdr(mustJSON(map[string]any{"alg": "RS256"})), ReasonMalformed},
		{"empty kid", hdr(mustJSON(with(header(kidA), "kid", ""))), ReasonMalformed},
		{"oversized kid", hdr(mustJSON(with(header(kidA), "kid", strings.Repeat("k", maxKidBytes+1)))), ReasonMalformed},
		{"kid with space", hdr(mustJSON(with(header(kidA), "kid", "kid a"))), ReasonMalformed},
		{"kid with control", hdr(mustJSON(with(header(kidA), "kid", "kid\x00a"))), ReasonMalformed},
		{"kid non-ASCII", hdr(mustJSON(with(header(kidA), "kid", "kíd"))), ReasonMalformed},
		{"kid not a string", hdr(mustJSON(with(header(kidA), "kid", 7))), ReasonMalformed},

		{"typ JOSE", hdr(mustJSON(with(header(kidA), "typ", "JOSE"))), ReasonMalformed},
		{"typ lowercase", hdr(mustJSON(with(header(kidA), "typ", "jwt"))), ReasonMalformed},
		{"typ not a string", hdr(mustJSON(with(header(kidA), "typ", true))), ReasonMalformed},
		{"crit", hdr(mustJSON(with(header(kidA), "crit", []string{"exp"}))), ReasonMalformed},
		{"jku", hdr(mustJSON(with(header(kidA), "jku", "https://evil.example/keys"))), ReasonMalformed},
		{"x5u", hdr(mustJSON(with(header(kidA), "x5u", "https://evil.example/cert"))), ReasonMalformed},
		{"jwk", hdr(mustJSON(with(header(kidA), "jwk", jwk(kidA, &keyA.PublicKey)))), ReasonMalformed},
		{"x5c", hdr(mustJSON(with(header(kidA), "x5c", []string{"MIIB"}))), ReasonMalformed},
		{"zip", hdr(mustJSON(with(header(kidA), "zip", "DEF"))), ReasonMalformed},
		{"b64", hdr(mustJSON(with(header(kidA), "b64", false))), ReasonMalformed},
		{"enc (JWE)", hdr(mustJSON(with(header(kidA), "enc", "A256GCM"))), ReasonMalformed},

		{"payload not JSON", payload(`{"sub":`), ReasonMalformed},
		{"payload array", payload(`[1,2]`), ReasonMalformed},
		{"payload scalar", payload(`42`), ReasonMalformed},
		{"payload string", payload(`"claims"`), ReasonMalformed},
		{"payload trailing data", payload(claims + `{}`), ReasonMalformed},
		{"payload invalid UTF-8", payload("{\"sub\":\"\xc3\x28\"}"), ReasonMalformed},
		{"duplicate claim", payload(strings.TrimSuffix(claims, "}") + `,"sub":"999"}`), ReasonMalformed},
		{"duplicate aud", payload(strings.TrimSuffix(claims, "}") + `,"aud":"other"}`), ReasonMalformed},
		{"too many claims", payload(manyMembers(maxObjectMembers + 1)), ReasonMalformed},

		{"signature too short", parts[0] + "." + parts[1] + "." + b64([]byte("short")), ReasonBadSignature},
		{"signature too long", parts[0] + "." + parts[1] + "." + b64(make([]byte, maxRSABits/8+1)), ReasonBadSignature},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := f.v.Verify(context.Background(), tt.token)
			requireReason(t, err, tt.want)
			if c.Subject() != "" {
				t.Error("claims returned with an error")
			}
		})
	}
	if n := f.ks.count(); n != 0 {
		t.Errorf("malformed tokens caused %d key fetches, want 0", n)
	}
}

const base64URLAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

func manyMembers(n int) string {
	var sb strings.Builder
	sb.WriteString("{")
	for i := range n {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`"c` + strings.Repeat("x", i) + `":1`)
	}
	sb.WriteString("}")
	return sb.String()
}

func TestVerifyRejectsBadSignatures(t *testing.T) {
	f := newFixture(t)
	f.prime(t)
	now := f.clock.Now()
	valid := f.valid()
	parts := strings.Split(valid, ".")
	input := parts[0] + "." + parts[1]

	sigBytes, _ := base64.RawURLEncoding.DecodeString(parts[2])
	flipped := append([]byte(nil), sigBytes...)
	flipped[len(flipped)/2] ^= 0x01

	sha512Digest := sha512.Sum512([]byte(input))
	sha512Sig, err := rsa.SignPKCS1v15(rand.Reader, keyA, crypto.SHA512, sha512Digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sha256Digest := sha256.Sum256([]byte(input))
	pssSig, err := rsa.SignPSS(rand.Reader, keyA, crypto.SHA256, sha256Digest[:], nil)
	if err != nil {
		t.Fatal(err)
	}
	otherPayload := b64([]byte(mustJSON(with(googleClaims(now), "sub", "attacker"))))

	tests := map[string]string{
		"flipped signature bit":  input + "." + b64(flipped),
		"other key, same kid":    sign(keyB, header(kidA), googleClaims(now)),
		"payload swapped":        parts[0] + "." + otherPayload + "." + parts[2],
		"signed with SHA-512":    input + "." + b64(sha512Sig),
		"signed with PSS":        input + "." + b64(pssSig),
		"all-zero signature":     input + "." + b64(make([]byte, 256)),
		"signature of wrong len": input + "." + b64(make([]byte, 512)),
		// HMAC with the public key as secret: the classic confusion. alg must
		// say RS256 here to get past the header check; the bytes are an HMAC.
		"HMAC bytes as signature": input + "." + b64(sha256Digest[:]),
	}
	for name, tok := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := f.v.Verify(context.Background(), tok)
			want := ReasonBadSignature
			requireReason(t, err, want)
		})
	}
}

func TestVerifyUnknownKid(t *testing.T) {
	f := newFixture(t)
	f.prime(t)
	_, err := f.v.Verify(context.Background(), sign(keyB, header(kidB), googleClaims(f.clock.Now())))
	requireReason(t, err, ReasonUnknownKey)
}

func TestVerifyClaims(t *testing.T) {
	f := newFixture(t)
	f.prime(t)
	now := f.clock.Now()
	base := googleClaims(now)
	unix := func(d time.Duration) int64 { return now.Add(d).Unix() }

	tests := []struct {
		name   string
		claims map[string]any
		want   Reason // "" means valid
	}{
		{"wrong issuer", with(base, "iss", "https://evil.example"), ReasonWrongIssuer},
		{"issuer with trailing slash", with(base, "iss", "https://accounts.google.com/"), ReasonWrongIssuer},
		{"issuer with space", with(base, "iss", "accounts.google.com "), ReasonWrongIssuer},
		{"issuer http", with(base, "iss", "http://accounts.google.com"), ReasonWrongIssuer},
		{"issuer upper case", with(base, "iss", "https://Accounts.Google.com"), ReasonWrongIssuer},
		{"issuer missing", with(base, "iss", nil), ReasonWrongIssuer},
		{"issuer not a string", with(base, "iss", 1), ReasonWrongIssuer},
		{"issuer array", with(base, "iss", []string{"https://accounts.google.com"}), ReasonWrongIssuer},

		{"wrong audience", with(base, "aud", "999-other.apps.googleusercontent.com"), ReasonWrongAudience},
		{"audience array", with(base, "aud", []string{testAud}), ReasonWrongAudience},
		{"audience trailing space", with(base, "aud", testAud+" "), ReasonWrongAudience},
		{"audience leading space", with(base, "aud", " "+testAud), ReasonWrongAudience},
		{"audience upper case", with(base, "aud", strings.ToUpper(testAud)), ReasonWrongAudience},
		{"audience prefix", with(base, "aud", testAud[:20]), ReasonWrongAudience},
		{"audience extended", with(base, "aud", testAud+".evil"), ReasonWrongAudience},
		{"audience missing", with(base, "aud", nil), ReasonWrongAudience},
		{"audience empty", with(base, "aud", ""), ReasonWrongAudience},

		{"exp missing", with(base, "exp", nil), ReasonBadClaims},
		{"exp string", with(base, "exp", "1790003590"), ReasonBadClaims},
		{"exp float", with(base, "exp", 1790003590.5), ReasonBadClaims},
		{"exp negative", with(base, "exp", -1), ReasonBadClaims},
		{"exp beyond year 9999", with(base, "exp", int64(maxUnixSeconds+1)), ReasonBadClaims},
		{"exp overflows int64", with(base, "exp", rawNumber("99999999999999999999")), ReasonBadClaims},
		{"exp exponent form", with(base, "exp", rawNumber("1.79e9")), ReasonBadClaims},
		{"exp bool", with(base, "exp", true), ReasonBadClaims},
		{"iat missing", with(base, "iat", nil), ReasonBadClaims},
		{"iat string", with(base, "iat", "1"), ReasonBadClaims},
		{"iat negative", with(base, "iat", -5), ReasonBadClaims},
		{"exp before iat", with(base, "iat", unix(time.Hour), "exp", unix(time.Minute)), ReasonBadClaims},
		{"exp equals iat", with(base, "iat", unix(0), "exp", unix(0)), ReasonBadClaims},
		{"nbf string", with(base, "nbf", "0"), ReasonBadClaims},

		{"expired", with(base, "iat", unix(-2*time.Hour), "exp", unix(-61*time.Second)), ReasonExpired},
		{"exactly at exp + skew", with(base, "iat", unix(-time.Hour), "exp", unix(-clockSkew)), ReasonExpired},
		{"one second before exp + skew", with(base, "iat", unix(-time.Hour), "exp", unix(-clockSkew+time.Second)), ""},
		{"at exp, within skew", with(base, "iat", unix(-time.Hour), "exp", unix(0)), ""},
		{"iat beyond skew", with(base, "iat", unix(61*time.Second), "exp", unix(time.Hour)), ReasonIssuedInFuture},
		{"iat at skew", with(base, "iat", unix(clockSkew), "exp", unix(time.Hour)), ""},
		{"iat far future", with(base, "iat", int64(maxUnixSeconds-1), "exp", int64(maxUnixSeconds)), ReasonIssuedInFuture},
		{"nbf beyond skew", with(base, "nbf", unix(61*time.Second)), ReasonIssuedInFuture},
		{"nbf now", with(base, "nbf", unix(0)), ""},

		{"sub missing", with(base, "sub", nil), ReasonBadSubject},
		{"sub empty", with(base, "sub", ""), ReasonBadSubject},
		{"sub 256 bytes", with(base, "sub", strings.Repeat("1", 256)), ReasonBadSubject},
		{"sub 255 bytes", with(base, "sub", strings.Repeat("1", 255)), ""},
		{"sub non-ASCII", with(base, "sub", "1101ñ"), ReasonBadSubject},
		{"sub with space", with(base, "sub", "1101 69"), ReasonBadSubject},
		{"sub with control", with(base, "sub", "1101\n69"), ReasonBadSubject},
		{"sub with DEL", with(base, "sub", "1101\x7f"), ReasonBadSubject},
		{"sub number", with(base, "sub", 110169484474386276334.0), ReasonBadSubject},
		{"sub null", with(base, "sub", rawNumber("null")), ReasonBadSubject},

		{"email number", with(base, "email", 5), ReasonBadClaims},
		{"email null", with(base, "email", rawNumber("null")), ReasonBadClaims},
		{"email array", with(base, "email", []string{testEmail}), ReasonBadClaims},
		{"email_verified string true", with(base, "email_verified", "true"), ReasonBadClaims},
		{"email_verified number", with(base, "email_verified", 1), ReasonBadClaims},
		{"email_verified null", with(base, "email_verified", rawNumber("null")), ReasonBadClaims},
		{"hd number", with(base, "hd", 1), ReasonBadClaims},
		{"hd null", with(base, "hd", rawNumber("null")), ReasonBadClaims},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := f.v.Verify(context.Background(), sign(keyA, header(kidA), tt.claims))
			if tt.want == "" {
				if err != nil {
					t.Fatalf("err = %v, want valid", err)
				}
				return
			}
			requireReason(t, err, tt.want)
			if c.Subject() != "" {
				t.Error("claims returned with an error")
			}
		})
	}
}

// rawNumber is JSON inserted verbatim by encoding/json (json.RawMessage).
func rawNumber(s string) any { return rawJSON(s) }

type rawJSON string

func (r rawJSON) MarshalJSON() ([]byte, error) { return []byte(r), nil }

// The clock is read at verification time, not at key fetch time.
func TestVerifyUsesClockAtVerification(t *testing.T) {
	f := newFixture(t)
	tok := f.valid() // expires about an hour after "now"
	f.prime(t)
	f.clock.Advance(time.Hour + clockSkew)
	_, err := f.v.Verify(context.Background(), tok)
	requireReason(t, err, ReasonExpired)
}

func TestVerifyHonorsCancelledContext(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.v.Verify(ctx, f.valid()); err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n := f.ks.count(); n != 0 {
		t.Errorf("fetched %d times for a cancelled request", n)
	}
}

func TestNewTokenVerifierValidatesOptions(t *testing.T) {
	const secretish = "GOCSPX-not-an-audience secret"
	tests := map[string]struct {
		audiences []string
		opts      Options
	}{
		"no audience":         {nil, Options{}},
		"empty audience":      {[]string{""}, Options{}},
		"audience with space": {[]string{secretish}, Options{}},
		"audience too long":   {[]string{strings.Repeat("a", maxAudienceBytes+1)}, Options{}},
		"http certs URL":      {[]string{testAud}, Options{CertsURL: "http://www.googleapis.com/oauth2/v3/certs"}},
		"relative certs URL":  {[]string{testAud}, Options{CertsURL: "/certs"}},
		"certs URL with user": {[]string{testAud}, Options{CertsURL: "https://user:" + secretish + "@example.com/certs"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			v, err := NewTokenVerifier(tt.audiences, tt.opts)
			if err == nil || v != nil {
				t.Fatalf("accepted: %v", err)
			}
			requireNoLeak(t, "error", err.Error(), secretish, "user")
		})
	}
	v, err := NewTokenVerifier([]string{testAud}, Options{})
	if err != nil || v.keys.url != DefaultCertsURL {
		t.Fatalf("defaults: %v", err)
	}
}

// Malformed input is rejected cheaply: a maximal token of dots must not
// allocate a slice of its ~4096 segments.
func TestMalformedTokenIsCheap(t *testing.T) {
	f := newFixture(t)
	dots := strings.Repeat(".", maxTokenBytes)
	res := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = f.v.Verify(context.Background(), dots)
		}
	})
	if bytes := res.AllocedBytesPerOp(); bytes > 1024 {
		t.Errorf("%d bytes allocated for a token of dots, want at most 1024", bytes)
	}
}
