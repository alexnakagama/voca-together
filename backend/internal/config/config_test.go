package config

import "testing"

const testDBURL = "postgres://u:p@localhost:5432/voca"

// env returns a getenv with DATABASE_URL set, overridden/extended by m. In
// production it also sets a valid RESEND_API_KEY and EMAIL_FROM unless m
// names them (even as ""), so tests of other settings don't fail on email.
func env(m map[string]string) func(string) string {
	vars := map[string]string{"DATABASE_URL": testDBURL}
	if m["ENV"] == "production" {
		vars["RESEND_API_KEY"] = testResendKey
		vars["EMAIL_FROM"] = testEmailFrom
	}
	for k, v := range m {
		vars[k] = v
	}
	return func(k string) string { return vars[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != "development" {
		t.Errorf("Env = %q, want development", cfg.Env)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.AppBaseURL != "http://localhost:8080" {
		t.Errorf("AppBaseURL = %q", cfg.AppBaseURL)
	}
	if cfg.DatabaseURL != testDBURL {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	if _, err := Load(env(map[string]string{"DATABASE_URL": ""})); err == nil {
		t.Fatal("expected error when DATABASE_URL is missing")
	}
}

func TestLoadRejectsUnknownEnv(t *testing.T) {
	if _, err := Load(env(map[string]string{"ENV": "staging-ish"})); err == nil {
		t.Fatal("expected error for unknown ENV")
	}
}

func TestLoadRejectsInvalidBaseURL(t *testing.T) {
	if _, err := Load(env(map[string]string{"APP_BASE_URL": "not a url"})); err == nil {
		t.Fatal("expected error for invalid APP_BASE_URL")
	}
}

func TestProductionRequiresHTTPS(t *testing.T) {
	_, err := Load(env(map[string]string{"ENV": "production", "APP_BASE_URL": "http://api.example.com",
		"TRUSTED_PROXY_HOPS": "1"}))
	if err == nil {
		t.Fatal("expected error: production must use https")
	}
	cfg, err := Load(env(map[string]string{"ENV": "production", "APP_BASE_URL": "https://api.example.com/",
		"TRUSTED_PROXY_HOPS": "1"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AppBaseURL != "https://api.example.com" {
		t.Errorf("trailing slash not trimmed: %q", cfg.AppBaseURL)
	}
	if !cfg.IsProduction() {
		t.Error("IsProduction() = false")
	}
}

func TestTrustedProxyHops(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TrustedProxyHops != 0 {
		t.Errorf("default TrustedProxyHops = %d, want 0", cfg.TrustedProxyHops)
	}

	cfg, err = Load(env(map[string]string{"TRUSTED_PROXY_HOPS": "2"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TrustedProxyHops != 2 {
		t.Errorf("TrustedProxyHops = %d, want 2", cfg.TrustedProxyHops)
	}

	for _, bad := range []string{"-1", "one", "1.5", " 1", "11"} {
		if _, err := Load(env(map[string]string{"TRUSTED_PROXY_HOPS": bad})); err == nil {
			t.Errorf("TRUSTED_PROXY_HOPS=%q accepted", bad)
		}
	}
}

func TestProductionRequiresExplicitTrustedProxyHops(t *testing.T) {
	prod := map[string]string{"ENV": "production", "APP_BASE_URL": "https://api.example.com"}
	if _, err := Load(env(prod)); err == nil {
		t.Fatal("production started without TRUSTED_PROXY_HOPS")
	}
	// An explicit 0 is a statement (no proxy adds X-Forwarded-For), so it is accepted.
	prod["TRUSTED_PROXY_HOPS"] = "0"
	if _, err := Load(env(prod)); err != nil {
		t.Fatalf("explicit 0 rejected: %v", err)
	}
}

func TestLoadRejectsBaseURLWithExtraParts(t *testing.T) {
	for _, bad := range []string{
		"https://user:pass@api.example.com",
		"https://user@api.example.com",
		"https://api.example.com?x=1",
		"https://api.example.com/?",
		"https://api.example.com#frag",
	} {
		if _, err := Load(env(map[string]string{"APP_BASE_URL": bad})); err == nil {
			t.Errorf("APP_BASE_URL=%q accepted", bad)
		}
	}
	if _, err := Load(env(map[string]string{"APP_BASE_URL": "https://example.com/api"})); err != nil {
		t.Errorf("base URL with a path rejected: %v", err)
	}
}

func TestProductionRejectsLoopbackBaseURL(t *testing.T) {
	for _, bad := range []string{"https://localhost", "https://LOCALHOST:8443", "https://127.0.0.1",
		"https://[::1]", "https://0.0.0.0", "https://app.localhost"} {
		_, err := Load(env(map[string]string{"ENV": "production", "APP_BASE_URL": bad, "TRUSTED_PROXY_HOPS": "1"}))
		if err == nil {
			t.Errorf("production accepted APP_BASE_URL=%q", bad)
		}
	}
	// Development keeps its localhost default.
	if _, err := Load(env(map[string]string{"APP_BASE_URL": "http://localhost:8080"})); err != nil {
		t.Errorf("development rejected localhost: %v", err)
	}
}
