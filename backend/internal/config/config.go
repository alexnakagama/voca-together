// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"vocatogether/backend/internal/email"
	"vocatogether/backend/internal/googleid"
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
	// ResendAPIKey and EmailFrom configure the Resend sender. ResendAPIKey is
	// empty when Resend isn't configured; both are always empty in the test
	// environment, which ignores them. EmailFrom is in canonical form
	// ("local@domain" or "Name <local@domain>").
	ResendAPIKey Secret
	EmailFrom    string
	// GoogleClientID is the Web OAuth client ID that Google ID tokens must
	// name as their audience (decision 020). It is public, not a Secret.
	// Empty means Google sign-in is disabled, which only development and
	// test allow; test always leaves it empty.
	GoogleClientID string
}

// Secret is a string that is redacted whenever it is formatted, logged or
// marshalled to JSON. Reveal returns the value, for the one place that uses it.
// The zero value is the empty secret.
type Secret struct {
	// v is a pointer so that fmt's fallback, which skips Format and prints
	// fields by reflection (%p on a value), shows an address, not the value.
	v *string
}

const redacted = "[REDACTED]"

func NewSecret(v string) Secret { return Secret{v: &v} }

func (s Secret) Reveal() string {
	if s.v == nil {
		return ""
	}
	return *s.v
}

// Format, String, GoString, LogValue, MarshalJSON and MarshalText never show
// the value. Format handles every fmt verb (String alone wouldn't cover %d or
// %x on a struct field), including inside a printed Config; the one verb fmt
// never passes to Format, %p on a value, finds only a pointer (see v).
func (s Secret) Format(f fmt.State, _ rune) { io.WriteString(f, redacted) }
func (s Secret) String() string             { return redacted }
func (s Secret) GoString() string           { return redacted }
func (s Secret) LogValue() slog.Value       { return slog.StringValue(redacted) }
func (s Secret) MarshalJSON() ([]byte, error) {
	return []byte(`"` + redacted + `"`), nil
}
func (s Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

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

	if err := loadEmail(&cfg, getenv); err != nil {
		return Config{}, err
	}
	if err := loadGoogle(&cfg, getenv); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// loadEmail reads the Resend settings for cfg.Env:
//   - production: RESEND_API_KEY and EMAIL_FROM are both required;
//   - development: both optional, but RESEND_API_KEY requires EMAIL_FROM
//     (without a key, development uses LogSender);
//   - test: both are ignored, even if set, so tests never reach the provider.
//
// Errors never echo either value: the key is a secret, and a misplaced key
// could just as well end up in EMAIL_FROM.
func loadEmail(cfg *Config, getenv func(string) string) error {
	if cfg.Env == "test" {
		return nil
	}
	key, from := getenv("RESEND_API_KEY"), getenv("EMAIL_FROM")

	if cfg.IsProduction() && key == "" {
		return errors.New("RESEND_API_KEY is required in production")
	}
	if key != "" && !email.ValidResendAPIKey(key) {
		return errors.New("RESEND_API_KEY is invalid: want printable ASCII without spaces, starting with re_")
	}
	if from == "" {
		if cfg.IsProduction() {
			return errors.New("EMAIL_FROM is required in production")
		}
		if key != "" {
			return errors.New("EMAIL_FROM is required when RESEND_API_KEY is set")
		}
		return nil
	}
	canonical, ok := email.CanonicalSender(from)
	if !ok {
		return errors.New(`EMAIL_FROM is invalid: want "local@domain" or "Name <local@domain>" ` +
			"with a domain name, and a display name of ASCII letters, digits and single spaces")
	}

	cfg.ResendAPIKey, cfg.EmailFrom = NewSecret(key), canonical
	return nil
}

// loadGoogle reads GOOGLE_CLIENT_ID for cfg.Env:
//   - production: required, so Google sign-in is never silently disabled;
//   - development: optional (unset disables Google sign-in), validated if set;
//   - test: ignored, even if set or invalid, so tests never reach Google.
//
// Only the shape is checked (googleid.ValidClientID); nothing is trimmed,
// since the token's aud must match exactly. Errors never echo the value: a
// client secret or token pasted into the variable must not reach the logs.
func loadGoogle(cfg *Config, getenv func(string) string) error {
	if cfg.Env == "test" {
		return nil
	}
	id := getenv("GOOGLE_CLIENT_ID")
	if id == "" {
		if cfg.IsProduction() {
			return errors.New("GOOGLE_CLIENT_ID is required in production")
		}
		return nil
	}
	if !googleid.ValidClientID(id) {
		return errors.New("GOOGLE_CLIENT_ID is invalid: want the Web OAuth client ID, " +
			"ending in .apps.googleusercontent.com (not a client secret)")
	}
	cfg.GoogleClientID = id
	return nil
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
