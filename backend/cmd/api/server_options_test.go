package main

import (
	"log/slog"
	"net/netip"
	"testing"

	"vocatogether/backend/internal/config"
)

// A limit left out of the options is the zero value, which disables it
// without any error: production must be wired with every one of them.
func TestServerOptionsWireEveryRateLimit(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	opts := serverOptions(config.Config{Env: "production", TrustedProxyHops: 2}, logger)

	if opts.UserLimits.ProfileWrite == nil {
		t.Error("the per-user profile write limit is not wired")
	}
	ip := opts.IPLimits
	if ip.Login == nil || ip.Register == nil || ip.Email == nil || ip.Token == nil || ip.Refresh == nil {
		t.Error("a per-IP limit is not wired")
	}
	if opts.TrustedProxyHops != 2 || !opts.HSTS {
		t.Errorf("options = hops %d, HSTS %v; want 2, true", opts.TrustedProxyHops, opts.HSTS)
	}

	// The wired limits really limit: the profile bucket empties.
	allowed := 0
	for range 100 {
		if ok, _ := opts.UserLimits.ProfileWrite.Allow("user"); ok {
			allowed++
		}
	}
	if allowed != 10 {
		t.Errorf("profile writes allowed in a burst = %d, want 10", allowed)
	}
	if ok, _ := ip.Login.Allow(netip.MustParsePrefix("198.51.100.1/32")); !ok {
		t.Error("the first login from an address was refused")
	}

	if dev := serverOptions(config.Config{Env: "development"}, logger); dev.HSTS {
		t.Error("HSTS outside production")
	}
}
