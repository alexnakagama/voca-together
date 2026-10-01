package config

import (
	"strings"
	"testing"
)

const testGoogleClientID = "123456789012-abcdefghijklmnopqrstuvwxyz012345.apps.googleusercontent.com"

func TestGoogleConfigValid(t *testing.T) {
	for _, tc := range []struct {
		name string
		vars map[string]string
		want string
	}{
		{"production", prodEnv(map[string]string{"GOOGLE_CLIENT_ID": testGoogleClientID}), testGoogleClientID},
		{"production legacy ID without dash",
			prodEnv(map[string]string{"GOOGLE_CLIENT_ID": "407408718192.apps.googleusercontent.com"}),
			"407408718192.apps.googleusercontent.com"},
		{"development with ID", map[string]string{"GOOGLE_CLIENT_ID": testGoogleClientID}, testGoogleClientID},
		{"development without ID", map[string]string{"ENV": "development"}, ""},
		{"development default ENV without ID", nil, ""},
		{"test ignores ID", map[string]string{"ENV": "test", "GOOGLE_CLIENT_ID": testGoogleClientID}, ""},
		{"test ignores invalid ID", map[string]string{"ENV": "test", "GOOGLE_CLIENT_ID": "GOCSPX-not an id"}, ""},
		{"test without ID", map[string]string{"ENV": "test"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(env(tc.vars))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.GoogleClientID != tc.want {
				t.Errorf("GoogleClientID = %q, want %q", cfg.GoogleClientID, tc.want)
			}
		})
	}
}

// Production never starts with Google sign-in disabled, and no environment
// but test starts with a malformed ID. Errors name the variable but never echo
// its value: a client secret or token pasted there must not reach the logs.
func TestGoogleConfigInvalid(t *testing.T) {
	const secret = "GOCSPX-SuperSecretValue_987654321"
	for _, tc := range []struct {
		name string
		vars map[string]string
	}{
		// getenv can't tell unset from empty: both are missing.
		{"production missing", prodEnv(map[string]string{"GOOGLE_CLIENT_ID": ""})},
		{"production client secret", prodEnv(map[string]string{"GOOGLE_CLIENT_ID": secret})},
		{"production secret with suffix lookalike",
			prodEnv(map[string]string{"GOOGLE_CLIENT_ID": secret + ".apps.googleusercontent.com.evil"})},
		{"production trailing newline",
			prodEnv(map[string]string{"GOOGLE_CLIENT_ID": "SuperSecret-1.apps.googleusercontent.com\n"})},
		{"production surrounding spaces",
			prodEnv(map[string]string{"GOOGLE_CLIENT_ID": " SuperSecret-1.apps.googleusercontent.com "})},
		{"production bare suffix", prodEnv(map[string]string{"GOOGLE_CLIENT_ID": ".apps.googleusercontent.com"})},
		{"development client secret", map[string]string{"GOOGLE_CLIENT_ID": secret}},
		{"development wrong suffix", map[string]string{"GOOGLE_CLIENT_ID": "SuperSecret-1.googleusercontent.com"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(env(tc.vars))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), "GOOGLE_CLIENT_ID") {
				t.Errorf("error doesn't name GOOGLE_CLIENT_ID: %v", err)
			}
			if strings.Contains(err.Error(), "SuperSecret") {
				t.Errorf("error echoes the value: %v", err)
			}
		})
	}
}
