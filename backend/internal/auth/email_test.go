package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeEmailValid(t *testing.T) {
	tests := map[string]string{
		"ana@example.com":           "ana@example.com",
		"  Ana@Example.COM \n":      "ana@example.com",
		"ana.lopez+tag@example.com": "ana.lopez+tag@example.com",
		"ana_99@mail.example.co.uk": "ana_99@mail.example.co.uk",
		"a@b-c.io":                  "a@b-c.io",
		"ana@123.example.com":       "ana@123.example.com", // digits are fine in non-final labels
		"ana@example.c0m":           "ana@example.c0m",     // a final label that is only partly digits is fine
		"o'brien@example.ie":        "o'brien@example.ie",
	}
	for in, want := range tests {
		got, err := NormalizeEmail(in)
		if err != nil {
			t.Errorf("NormalizeEmail(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeEmailLengthLimits(t *testing.T) {
	domain := "@example.com" // 12 chars

	if _, err := NormalizeEmail(strings.Repeat("a", 64) + domain); err != nil {
		t.Errorf("64-char local part rejected: %v", err)
	}
	if _, err := NormalizeEmail(strings.Repeat("a", 65) + domain); !errors.Is(err, ErrEmailInvalid) {
		t.Errorf("65-char local part: err = %v, want ErrEmailInvalid", err)
	}

	// 254 chars total is the maximum (matches the DB CHECK).
	label := strings.Repeat("d", 62)
	max := "a@" + label + "." + label + "." + label + "." + strings.Repeat("d", 59) + ".com"
	if len(max) != 254 {
		t.Fatalf("test setup: len = %d", len(max))
	}
	if _, err := NormalizeEmail(max); err != nil {
		t.Errorf("254-char address rejected: %v", err)
	}
	if _, err := NormalizeEmail("a" + max); !errors.Is(err, ErrEmailInvalid) {
		t.Errorf("255-char address: err = %v, want ErrEmailInvalid", err)
	}
}

func TestNormalizeEmailInvalid(t *testing.T) {
	tests := map[string]string{
		"empty":                  "",
		"whitespace only":        "   ",
		"no at":                  "ana.example.com",
		"no domain":              "ana@",
		"no local part":          "@example.com",
		"two ats":                "ana@@example.com",
		"single-label domain":    "ana@localhost",
		"display name":           "Ana <ana@example.com>",
		"angle brackets":         "<ana@example.com>",
		"quoted local part":      `"ana lopez"@example.com`,
		"space in local part":    "an a@example.com",
		"space in domain":        "ana@exa mple.com",
		"ip literal":             "ana@[127.0.0.1]",
		"comment":                "ana@example.com (Ana)",
		"comment without spaces": "(x)ana@example.com",
		"comment after local":    "ana(x)@example.com",
		"header injection":       "ana@example.com\r\nBcc: evil@example.com",
		"consecutive dots":       "ana@example..com",
		"leading dot in domain":  "ana@.example.com",
		"trailing dot":           "ana@example.com.",
		"hyphen-edged label":     "ana@-example.com",
		"label over 63 chars":    "ana@" + strings.Repeat("d", 64) + ".com",
		"non-ascii local part":   "ñandú@example.com",
		"non-ascii domain":       "ana@correo.españa.es",
		"list of addresses":      "ana@example.com, ben@example.com",
		"ip-like domain":         "ana@1.2.3.4",
		"all-digit final label":  "ana@example.123",
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := NormalizeEmail(in)
			if !errors.Is(err, ErrEmailInvalid) {
				t.Errorf("NormalizeEmail(%q) = %q, %v; want ErrEmailInvalid", in, got, err)
			}
		})
	}
}

func TestEmailErrorIsFieldError(t *testing.T) {
	var fe *FieldError
	if !errors.As(ErrEmailInvalid, &fe) || fe.Field != "email" || fe.Code != "invalid" {
		t.Errorf("ErrEmailInvalid = %#v, want FieldError{email, invalid}", ErrEmailInvalid)
	}
}
