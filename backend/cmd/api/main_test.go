package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"vocatogether/backend/internal/config"
	"vocatogether/backend/internal/email"
)

const (
	testKey  = "re_MainTestKey_SECRET42"
	testFrom = "VocaTogether <no-reply@mail.example.com>"
)

// resendConfig is what config.Load produces for a valid Resend setup.
func resendConfig(env string) config.Config {
	return config.Config{Env: env, ResendAPIKey: config.NewSecret(testKey), EmailFrom: testFrom}
}

// newSender calls newEmailSender and checks that nothing it logs or returns
// shows the API key or the sender address.
func newSender(t *testing.T, cfg config.Config) (email.Sender, string, error) {
	t.Helper()
	var buf bytes.Buffer
	sender, err := newEmailSender(cfg, slog.New(slog.NewJSONHandler(&buf, nil)))
	out := buf.String()
	if err != nil {
		out += err.Error()
	}
	for _, s := range []string{"SECRET42", "no-reply", "mail.example.com", "VocaTogether"} {
		if strings.Contains(out, s) {
			t.Errorf("output shows %q: %s", s, out)
		}
	}
	return sender, buf.String(), err
}

func TestNewEmailSender(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cfg      config.Config
		provider string // "" when an error is expected
	}{
		{"production with Resend", resendConfig("production"), "resend"},
		{"development with Resend", resendConfig("development"), "resend"},
		{"development without Resend", config.Config{Env: "development"}, "log"},
		{"development with EMAIL_FROM only", config.Config{Env: "development", EmailFrom: testFrom}, "log"},
		{"test with Resend settings", resendConfig("test"), "log"},
		{"test without Resend", config.Config{Env: "test"}, "log"},
		// config.Load rejects these; newEmailSender must not fall back to LogSender.
		{"production without key", config.Config{Env: "production", EmailFrom: testFrom}, ""},
		{"production without anything", config.Config{Env: "production"}, ""},
		{"production without EMAIL_FROM", config.Config{Env: "production",
			ResendAPIKey: config.NewSecret(testKey)}, ""},
		{"production invalid key", config.Config{Env: "production",
			ResendAPIKey: config.NewSecret("sk_SECRET42"), EmailFrom: testFrom}, ""},
		{"development key without EMAIL_FROM", config.Config{Env: "development",
			ResendAPIKey: config.NewSecret(testKey)}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender, logs, err := newSender(t, tc.cfg)
			if tc.provider == "" {
				if err == nil {
					t.Fatalf("got sender %T, want an error", sender)
				}
				if logs != "" {
					t.Errorf("logged a provider despite the error: %s", logs)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			switch tc.provider {
			case "resend":
				if _, ok := sender.(*email.ResendSender); !ok {
					t.Errorf("sender = %T, want *email.ResendSender", sender)
				}
			case "log":
				if _, ok := sender.(*email.LogSender); !ok {
					t.Errorf("sender = %T, want *email.LogSender", sender)
				}
			}
			if !strings.Contains(logs, `"provider":"`+tc.provider+`"`) {
				t.Errorf("startup log doesn't name provider %q: %s", tc.provider, logs)
			}
		})
	}
}

// TestNewEmailSenderFromEnvironment goes through config.Load, as main does.
func TestNewEmailSenderFromEnvironment(t *testing.T) {
	base := map[string]string{"DATABASE_URL": "postgres://u:p@localhost:5432/voca"}
	prod := map[string]string{"ENV": "production", "APP_BASE_URL": "https://api.example.com",
		"TRUSTED_PROXY_HOPS": "1"}
	resend := map[string]string{"RESEND_API_KEY": testKey, "EMAIL_FROM": testFrom}
	for _, tc := range []struct {
		name string
		vars []map[string]string
		want string // "resend", "log", or "" when config.Load must fail
	}{
		{"production with Resend", []map[string]string{prod, resend}, "resend"},
		{"production without Resend", []map[string]string{prod}, ""},
		{"development with Resend", []map[string]string{resend}, "resend"},
		{"development without Resend", nil, "log"},
		{"test with Resend", []map[string]string{{"ENV": "test"}, resend}, "log"},
		{"test without Resend", []map[string]string{{"ENV": "test"}}, "log"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vars := map[string]string{}
			for _, m := range append([]map[string]string{base}, tc.vars...) {
				for k, v := range m {
					vars[k] = v
				}
			}
			cfg, err := config.Load(func(k string) string { return vars[k] })
			if tc.want == "" {
				if err == nil {
					t.Fatal("config.Load accepted the environment")
				}
				return
			}
			if err != nil {
				t.Fatalf("config.Load: %v", err)
			}
			sender, _, err := newSender(t, cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			_, isResend := sender.(*email.ResendSender)
			if isResend != (tc.want == "resend") {
				t.Errorf("sender = %T, want provider %s", sender, tc.want)
			}
		})
	}
}
