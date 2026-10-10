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
	if opts.UserLimits.LanguagesWrite == nil {
		t.Fatal("the per-user languages write limit is not wired")
	}
	if opts.UserLimits.MemberRead == nil {
		t.Fatal("the per-user member read limit is not wired")
	}
	if opts.UserLimits.AvatarWrite == nil {
		t.Fatal("the per-user avatar write limit is not wired")
	}
	if opts.UserLimits.BlockWrite == nil {
		t.Fatal("the per-user block write limit is not wired")
	}
	ip := opts.IPLimits
	if ip.Login == nil || ip.Register == nil || ip.Email == nil || ip.Token == nil || ip.Refresh == nil {
		t.Error("a per-IP limit is not wired")
	}
	if opts.TrustedProxyHops != 2 || !opts.HSTS {
		t.Errorf("options = hops %d, HSTS %v; want 2, true", opts.TrustedProxyHops, opts.HSTS)
	}

	// The wired limits really limit: each bucket empties, on its own.
	allowed, languagesAllowed, readsAllowed, avatarsAllowed, blocksAllowed := 0, 0, 0, 0, 0
	for range 100 {
		if ok, _ := opts.UserLimits.BlockWrite.Allow("user"); ok {
			blocksAllowed++
		}
	}
	if blocksAllowed != 10 {
		t.Errorf("block writes allowed in a burst = %d, want 10 of their own", blocksAllowed)
	}
	for range 100 {
		if ok, _ := opts.UserLimits.AvatarWrite.Allow("user"); ok {
			avatarsAllowed++
		}
	}
	if avatarsAllowed != 5 {
		t.Errorf("avatar writes allowed in a burst = %d, want 5 of their own", avatarsAllowed)
	}
	for range 100 {
		if ok, _ := opts.UserLimits.ProfileWrite.Allow("user"); ok {
			allowed++
		}
	}
	for range 100 {
		if ok, _ := opts.UserLimits.LanguagesWrite.Allow("user"); ok {
			languagesAllowed++
		}
	}
	for range 100 {
		if ok, _ := opts.UserLimits.MemberRead.Allow("user"); ok {
			readsAllowed++
		}
	}
	if readsAllowed != 60 {
		t.Errorf("member reads allowed in a burst = %d, want 60 of their own", readsAllowed)
	}
	if allowed != 10 {
		t.Errorf("profile writes allowed in a burst = %d, want 10", allowed)
	}
	if languagesAllowed != 10 {
		t.Errorf("languages writes allowed in a burst = %d, want 10 of their own", languagesAllowed)
	}
	if ok, _ := ip.Login.Allow(netip.MustParsePrefix("198.51.100.1/32")); !ok {
		t.Error("the first login from an address was refused")
	}

	if dev := serverOptions(config.Config{Env: "development"}, logger); dev.HSTS {
		t.Error("HSTS outside production")
	}
}
