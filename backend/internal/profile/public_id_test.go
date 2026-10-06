package profile

import (
	"strings"
	"testing"
)

func TestParsePublicID(t *testing.T) {
	const id = "3f2b8c1e-9a4d-4e7f-8b6a-0c1d2e3f4a5b"

	for name, s := range map[string]string{
		"canonical":  id,
		"all digits": "00000000-0000-0000-0000-000000000000",
		"all a to f": "abcdefab-cdef-abcd-efab-cdefabcdefab",
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := ParsePublicID(s)
			if !ok || got != s {
				t.Errorf("ParsePublicID(%q) = %q, %v; want it unchanged", s, got, ok)
			}
		})
	}

	for name, s := range map[string]string{
		"empty":             "",
		"upper case":        strings.ToUpper(id),
		"one upper letter":  "3F2b8c1e-9a4d-4e7f-8b6a-0c1d2e3f4a5b",
		"braces":            "{" + id + "}",
		"urn":               "urn:uuid:" + id,
		"no hyphens":        strings.ReplaceAll(id, "-", ""),
		"35 characters":     id[:35],
		"37 characters":     id + "0",
		"leading space":     " " + id,
		"trailing space":    id + " ",
		"space for a digit": " " + id[1:],
		"trailing newline":  id + "\n",
		"newline in place":  id[:35] + "\n",
		"hyphen moved":      "3f2b8c1-e9a4d-4e7f-8b6a-0c1d2e3f4a5b",
		"hyphen for a hex":  "3f2b8c1e-9a4d-4e7f-8b6a-0c1d2e3f4a5-",
		"hex for a hyphen":  "3f2b8c1e09a4d-4e7f-8b6a-0c1d2e3f4a5b",
		"g":                 "gf2b8c1e-9a4d-4e7f-8b6a-0c1d2e3f4a5b",
		"fullwidth digit":   "３f2b8c1e-9a4d-4e7f-8b6a-0c1d2e3f4a",
		"null byte":         id[:35] + "\x00",
		"not an id":         "not-an-id",
		"36 other letters":  strings.Repeat("z", 36),
	} {
		t.Run(name, func(t *testing.T) {
			if got, ok := ParsePublicID(s); ok || got != "" {
				t.Errorf("ParsePublicID(%q) = %q, %v; want refused", s, got, ok)
			}
		})
	}
}
