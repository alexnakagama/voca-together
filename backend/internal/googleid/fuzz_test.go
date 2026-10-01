package googleid

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// FuzzVerify feeds arbitrary strings to a verifier with a primed cache and
// a frozen clock. Properties:
//   - no panic, and only the fixed error messages (so nothing of the input
//     is echoed);
//   - success only for the seed tokens validly signed by keyA: nobody can
//     forge RSA, so any other success would be a signature bypass;
//   - bounded network: priming is one fetch, an unknown kid at most one
//     refresh while the clock stands still.
func FuzzVerify(f *testing.F) {
	fx := newFixture(f)
	fx.prime(f)
	now := fx.clock.Now()
	valid := map[string]bool{}
	for _, claims := range []map[string]any{
		googleClaims(now),
		with(googleClaims(now), "email", nil, "email_verified", nil, "hd", nil),
	} {
		tok := sign(keyA, header(kidA), claims)
		valid[tok] = true
		f.Add(tok)
	}
	f.Add("")
	f.Add("a.b.c")
	f.Add(sign(keyA, header("unknown"), googleClaims(now)))
	f.Add(sign(keyB, header(kidA), googleClaims(now)))
	f.Add(signRaw(keyA, `{"alg":"RS256","alg":"none","kid":"`+kidA+`"}`, mustJSON(googleClaims(now))))
	f.Add(signRaw(keyA, mustJSON(header(kidA)), `{"sub":1,"aud":[1],"exp":"x"}`))

	f.Fuzz(func(t *testing.T, raw string) {
		c, err := fx.v.Verify(context.Background(), raw)
		if err == nil {
			if !valid[raw] {
				t.Fatalf("accepted a token that is not a validly signed seed: %q", raw)
			}
			if c.Subject() != testSubject {
				t.Fatalf("wrong subject for a valid seed")
			}
			return
		}
		requireFixedError(t, err)
		if c.Subject() != "" {
			t.Fatal("claims returned with an error")
		}
		if n := fx.ks.count(); n > 2 {
			t.Fatalf("%d key fetches; malformed input must not cause more than one refresh", n)
		}
	})
}

// FuzzParseObject checks the strict JSON object parser: no panic, and
// whatever it accepts is valid JSON with unique names within the bounds.
func FuzzParseObject(f *testing.F) {
	for _, s := range []string{`{}`, `{"a":1}`, `{"a":1,"a":2}`, `{"a":1}{}`, `[]`, `1`, `{"a":1,"a":2}`,
		`{"a":{"b":[1,2,{"c":null}]}}`, "{\"a\":\"\xff\"}", ` { "a" : true } `, `{"a":1,}`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > maxTokenBytes {
			return // parseObject only sees decoded token segments, at most this long
		}
		m, err := parseObject(b)
		if err != nil {
			return
		}
		if !json.Valid(b) {
			t.Fatalf("accepted invalid JSON %q", b)
		}
		if len(m) > maxObjectMembers {
			t.Fatalf("%d members, above the bound", len(m))
		}
		var generic any
		if json.Unmarshal(b, &generic) != nil {
			t.Fatal("encoding/json disagrees")
		}
		if _, ok := generic.(map[string]any); !ok {
			t.Fatalf("accepted a non-object %q", b)
		}
	})
}

// FuzzClaims runs arbitrary payload JSON through claim validation, as if
// its signature had verified. No panic, only fixed errors, and anything
// accepted satisfies the required checks.
func FuzzClaims(f *testing.F) {
	fx := newFixture(f)
	now := fx.clock.Now()
	for _, claims := range []map[string]any{
		googleClaims(now),
		with(googleClaims(now), "email_verified", "true"),
		with(googleClaims(now), "aud", []string{testAud}),
		with(googleClaims(now), "exp", 1e30),
		with(googleClaims(now), "sub", strings.Repeat("9", 300)),
	} {
		f.Add([]byte(mustJSON(claims)))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > maxTokenBytes {
			return
		}
		p, err := parseObject(b)
		if err != nil {
			return
		}
		c, err := fx.v.claims(p)
		if err != nil {
			requireFixedError(t, err)
			return
		}
		if iss, _ := jsonString(p["iss"]); !googleIssuers[iss] {
			t.Fatal("accepted a wrong issuer")
		}
		if aud, _ := jsonString(p["aud"]); aud != testAud {
			t.Fatal("accepted a wrong audience")
		}
		if exp, ok := jsonUnixSeconds(p["exp"]); !ok || !now.Before(unixTime(exp).Add(clockSkew)) {
			t.Fatal("accepted an expired or malformed exp")
		}
		if !printableASCII(c.Subject(), maxSubjectBytes) {
			t.Fatal("accepted a malformed subject")
		}
	})
}

func unixTime(s int64) time.Time { return time.Unix(s, 0) }
