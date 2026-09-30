// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// maxTrustedProxyHops is a sanity bound: real deployments have one or two.
const maxTrustedProxyHops = 10

type Config struct {
	Env        string // development | test | production
	HTTPAddr   string
	AppBaseURL string // public base URL used in emailed links; no trailing slash
	// DatabaseURL is a secret (it contains credentials): never log it.
	DatabaseURL string
	// TrustedProxyHops is how many reverse proxies in front of the server
	// append to X-Forwarded-For; the client IP is the entry that many places
	// from the right (0: use the TCP peer address). Valid only while the
	// server is reachable exclusively through those proxies: exposed
	// directly, clients could spoof their IP (decision 018).
	TrustedProxyHops int
}

func (c Config) IsProduction() bool { return c.Env == "production" }

// Load reads configuration through getenv (os.Getenv in main, a map in tests).
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Env:         withDefault(getenv("ENV"), "development"),
		HTTPAddr:    withDefault(getenv("HTTP_ADDR"), ":8080"),
		AppBaseURL:  strings.TrimRight(withDefault(getenv("APP_BASE_URL"), "http://localhost:8080"), "/"),
		DatabaseURL: getenv("DATABASE_URL"),
	}

	hops, hopsSet := getenv("TRUSTED_PROXY_HOPS"), true
	if hops == "" {
		hops, hopsSet = "0", false
	}
	n, err := strconv.Atoi(hops)
	if err != nil || n < 0 || n > maxTrustedProxyHops {
		return Config{}, fmt.Errorf("TRUSTED_PROXY_HOPS must be an integer from 0 to %d, got %q", maxTrustedProxyHops, hops)
	}
	cfg.TrustedProxyHops = n

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
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
	// Emailed links are built by appending a path and ?token= to the base.
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" {
		return Config{}, fmt.Errorf("APP_BASE_URL must not contain credentials, a query or a fragment")
	}

	if cfg.IsProduction() {
		// Emailed links carry secret tokens; never send them over plain HTTP.
		if u.Scheme != "https" {
			return Config{}, fmt.Errorf("APP_BASE_URL must use https in production")
		}
		if isLoopback(u.Hostname()) {
			return Config{}, fmt.Errorf("APP_BASE_URL must be a public host in production")
		}
		// The server has no TLS, so production always runs behind something;
		// the operator must state what, rather than get a silent default.
		if !hopsSet {
			return Config{}, fmt.Errorf("TRUSTED_PROXY_HOPS must be set explicitly in production")
		}
	}

	return cfg, nil
}

// isLoopback reports whether host names this machine: localhost (and its
// subdomains, RFC 6761), a loopback address or the unspecified address.
func isLoopback(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && (ip.IsLoopback() || ip.IsUnspecified())
}

func withDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
