package googleid

import (
	"strings"
	"testing"
)

func TestValidClientID(t *testing.T) {
	const suffix = ".apps.googleusercontent.com"
	for _, ok := range []string{
		testAud,
		"1234567890-abc123def456" + suffix, // Google's documented example
		"407408718192" + suffix,            // older IDs have no dash
		"123-ABCdef_ghi.jkl" + suffix,      // the prefix has no documented grammar
		"x" + suffix,                       // shortest
		strings.Repeat("1", 255-len(suffix)) + suffix, // longest
	} {
		if !ValidClientID(ok) {
			t.Errorf("ValidClientID(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{
		"",
		suffix,                       // no prefix
		"apps.googleusercontent.com", // no prefix, no dot
		"123-abc.apps.googleusercontent.com.evil.example",
		"123-abc.googleusercontent.com",
		"123-abc.APPS.GOOGLEUSERCONTENT.COM",
		"123-abc.apps.googleusercontent.com.",
		"123-abc",
		"GOCSPX-SuperSecretValue0123456789",
		"eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln",
		" 123-abc" + suffix,
		"123-abc" + suffix + " ",
		"123-abc" + suffix + "\n",
		"123 abc" + suffix,
		"123-abc\t" + suffix,
		"123-äbc" + suffix,
		strings.Repeat("1", 256-len(suffix)) + suffix, // 256 bytes
	} {
		if ValidClientID(bad) {
			t.Errorf("ValidClientID(%q) = true, want false", bad)
		}
	}
}
