package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

const (
	testResendKey = "re_TestKey_0123456789abcdef"
	testEmailFrom = "VocaTogether <no-reply@mail.example.com>"
)

// prodEnv is a valid production environment apart from the email settings.
func prodEnv(m map[string]string) map[string]string {
	vars := map[string]string{"ENV": "production", "APP_BASE_URL": "https://api.example.com",
		"TRUSTED_PROXY_HOPS": "1"}
	for k, v := range m {
		vars[k] = v
	}
	return vars
}

func TestEmailConfigValid(t *testing.T) {
	for _, tc := range []struct {
		name     string
		vars     map[string]string
		wantKey  string
		wantFrom string
	}{
		{"production", prodEnv(map[string]string{"RESEND_API_KEY": testResendKey, "EMAIL_FROM": testEmailFrom}),
			testResendKey, testEmailFrom},
		{"production bare address, trimmed",
			prodEnv(map[string]string{"RESEND_API_KEY": testResendKey, "EMAIL_FROM": "  no-reply@mail.example.com "}),
			testResendKey, "no-reply@mail.example.com"},
		{"development without Resend", map[string]string{"ENV": "development"}, "", ""},
		{"development default ENV without Resend", nil, "", ""},
		{"development with Resend", map[string]string{"RESEND_API_KEY": testResendKey, "EMAIL_FROM": testEmailFrom},
			testResendKey, testEmailFrom},
		{"development EMAIL_FROM alone", map[string]string{"EMAIL_FROM": testEmailFrom}, "", testEmailFrom},
		{"test ignores Resend", map[string]string{"ENV": "test", "RESEND_API_KEY": testResendKey,
			"EMAIL_FROM": testEmailFrom}, "", ""},
		{"test ignores invalid Resend", map[string]string{"ENV": "test", "RESEND_API_KEY": "not a key",
			"EMAIL_FROM": "<bad>"}, "", ""},
		{"test ignores key without EMAIL_FROM", map[string]string{"ENV": "test", "RESEND_API_KEY": testResendKey},
			"", ""},
		{"test without Resend", map[string]string{"ENV": "test"}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(env(tc.vars))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.ResendAPIKey.Reveal() != tc.wantKey {
				t.Errorf("ResendAPIKey mismatch (want set: %v)", tc.wantKey != "")
			}
			if cfg.EmailFrom != tc.wantFrom {
				t.Errorf("EmailFrom = %q, want %q", cfg.EmailFrom, tc.wantFrom)
			}
		})
	}
}

func TestEmailConfigInvalid(t *testing.T) {
	const secret = "re_SuperSecretValue_987654321"
	for _, tc := range []struct {
		name string
		vars map[string]string
	}{
		{"production without key", prodEnv(map[string]string{"RESEND_API_KEY": "", "EMAIL_FROM": testEmailFrom})},
		{"production without EMAIL_FROM", prodEnv(map[string]string{"RESEND_API_KEY": secret, "EMAIL_FROM": ""})},
		{"production without either", prodEnv(map[string]string{"RESEND_API_KEY": "", "EMAIL_FROM": ""})},
		{"production invalid key", prodEnv(map[string]string{"RESEND_API_KEY": "sk_" + secret,
			"EMAIL_FROM": testEmailFrom})},
		{"production invalid EMAIL_FROM", prodEnv(map[string]string{"RESEND_API_KEY": secret,
			"EMAIL_FROM": "no-reply@localhost"})},
		{"development key without EMAIL_FROM", map[string]string{"RESEND_API_KEY": secret}},
		{"development invalid key", map[string]string{"RESEND_API_KEY": secret + " x", "EMAIL_FROM": testEmailFrom}},
		{"development invalid EMAIL_FROM with key", map[string]string{"RESEND_API_KEY": secret,
			"EMAIL_FROM": `"Voca" <no-reply@mail.example.com>`}},
		{"development invalid EMAIL_FROM alone", map[string]string{"EMAIL_FROM": "not an address"}},
		// An operator who swaps the two variables must not see the key echoed.
		{"key in EMAIL_FROM", map[string]string{"RESEND_API_KEY": testResendKey, "EMAIL_FROM": secret}},
		{"key in both", map[string]string{"RESEND_API_KEY": secret + "\n", "EMAIL_FROM": secret}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(env(tc.vars))
			if err == nil {
				t.Fatal("expected an error")
			}
			if strings.Contains(err.Error(), "SuperSecret") {
				t.Errorf("error echoes the secret: %v", err)
			}
		})
	}
}

func TestInvalidEmailFromValues(t *testing.T) {
	for _, bad := range []string{
		"no-reply",
		"no-reply@example",
		"no-reply@[127.0.0.1]",
		"<no-reply@mail.example.com>",
		"Voca  Together <no-reply@mail.example.com>",
		"Voca, Inc <no-reply@mail.example.com>",
		"no-reply@mail.example.com (comment)",
		"a@mail.example.com, b@mail.example.com",
		"no-reply@mail.example.com\r\nBcc: x@example.com",
		"Vöca <no-reply@mail.example.com>",
	} {
		vars := map[string]string{"RESEND_API_KEY": testResendKey, "EMAIL_FROM": bad}
		if _, err := Load(env(vars)); err == nil {
			t.Errorf("EMAIL_FROM=%q accepted", bad)
		}
	}
}

func TestInvalidResendAPIKeyValues(t *testing.T) {
	for _, bad := range []string{"re_", "RE_abc", "abc", " re_abc", "re_abc ", "re_a\tb", "re_ä"} {
		vars := map[string]string{"RESEND_API_KEY": bad, "EMAIL_FROM": testEmailFrom}
		if _, err := Load(env(vars)); err == nil {
			t.Errorf("RESEND_API_KEY=%q accepted", bad)
		}
	}
}

func TestSecretIsRedacted(t *testing.T) {
	cfg, err := Load(env(map[string]string{"RESEND_API_KEY": testResendKey, "EMAIL_FROM": testEmailFrom,
		"GOOGLE_CLIENT_ID": testGoogleClientID}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GoogleClientID != testGoogleClientID {
		t.Fatalf("GoogleClientID = %q", cfg.GoogleClientID)
	}
	// The formats are variables so vet doesn't reject the deliberately wrong
	// ones: %p on a value skips Format and prints fields by reflection.
	var outputs []string
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "% x", "%d", "%+d", "%o", "%b",
		"%t", "%e", "%U", "%c", "%p", "%T", "%10.3s", "%-40v"} {
		outputs = append(outputs,
			fmt.Sprintf(verb, cfg), fmt.Sprintf(verb, &cfg),
			fmt.Sprintf(verb, cfg.ResendAPIKey), fmt.Sprintf(verb, &cfg.ResendAPIKey),
			fmt.Sprintf(verb, []Secret{cfg.ResendAPIKey}), fmt.Sprintf(verb, struct{ C *Config }{&cfg}))
	}
	outputs = append(outputs, fmt.Sprint(cfg), fmt.Sprintln(cfg.ResendAPIKey))

	js, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	jsMap, err := json.Marshal(map[Secret]string{cfg.ResendAPIKey: "v"})
	if err != nil {
		t.Fatal(err)
	}
	outputs = append(outputs, string(js), string(jsMap))

	for _, newHandler := range []func(*bytes.Buffer) slog.Handler{
		func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
		func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
	} {
		var buf bytes.Buffer
		slog.New(newHandler(&buf)).Info("config", "key", cfg.ResendAPIKey, "cfg", cfg)
		outputs = append(outputs, buf.String())
	}

	for _, out := range outputs {
		if strings.Contains(out, "TestKey") || strings.Contains(out, fmt.Sprintf("%x", testResendKey)) ||
			strings.Contains(out, fmt.Sprintf("%X", testResendKey)) {
			t.Errorf("secret leaked: %s", out)
		}
	}
	// The safe forms are exactly the marker.
	key := cfg.ResendAPIKey
	for name, got := range map[string]string{"%v": fmt.Sprintf("%v", key), "%s": fmt.Sprintf("%s", key),
		"%#v": fmt.Sprintf("%#v", key), "String": key.String(), "GoString": key.GoString(),
		"LogValue": key.LogValue().String()} {
		if got != "[REDACTED]" {
			t.Errorf("%s = %q, want [REDACTED]", name, got)
		}
	}
	if b, err := json.Marshal(key); err != nil || string(b) != `"[REDACTED]"` {
		t.Errorf("JSON = %s, %v", b, err)
	}
	if key.Reveal() != testResendKey {
		t.Error("Reveal() lost the value")
	}
}

func TestSecretZeroValue(t *testing.T) {
	var s Secret
	if s.Reveal() != "" {
		t.Errorf("zero Secret reveals %q", s.Reveal())
	}
	if fmt.Sprint(s) != "[REDACTED]" {
		t.Errorf("zero Secret formats as %q", fmt.Sprint(s))
	}
	if NewSecret("").Reveal() != "" {
		t.Error(`NewSecret("") is not empty`)
	}
}
