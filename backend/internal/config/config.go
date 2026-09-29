// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"net/url"
	"strings"
)

type Config struct {
	Env        string // development | test | production
	HTTPAddr   string
	AppBaseURL string // public base URL used in emailed links; no trailing slash
}

func (c Config) IsProduction() bool { return c.Env == "production" }

// Load reads configuration through getenv (os.Getenv in main, a map in tests).
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Env:        withDefault(getenv("ENV"), "development"),
		HTTPAddr:   withDefault(getenv("HTTP_ADDR"), ":8080"),
		AppBaseURL: strings.TrimRight(withDefault(getenv("APP_BASE_URL"), "http://localhost:8080"), "/"),
	}

	switch cfg.Env {
	case "development", "test", "production":
	default:
		return Config{}, fmt.Errorf("ENV must be development, test or production, got %q", cfg.Env)
	}

	u, err := url.Parse(cfg.AppBaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Config{}, fmt.Errorf("APP_BASE_URL must be an absolute http(s) URL, got %q", cfg.AppBaseURL)
	}
	// Emailed links carry secret tokens; never send them over plain HTTP in production.
	if cfg.IsProduction() && u.Scheme != "https" {
		return Config{}, fmt.Errorf("APP_BASE_URL must use https in production")
	}

	return cfg, nil
}

func withDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
