package config

import "testing"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
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
	_, err := Load(env(map[string]string{"ENV": "production", "APP_BASE_URL": "http://api.example.com"}))
	if err == nil {
		t.Fatal("expected error: production must use https")
	}
	cfg, err := Load(env(map[string]string{"ENV": "production", "APP_BASE_URL": "https://api.example.com/"}))
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
