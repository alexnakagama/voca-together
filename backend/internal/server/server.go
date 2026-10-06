// Package server assembles the HTTP routes of the API.
package server

import (
	"log/slog"
	"net/http"
	"net/netip"

	"vocatogether/backend/internal/auth"
	"vocatogether/backend/internal/avatar"
	"vocatogether/backend/internal/language"
	"vocatogether/backend/internal/profile"
	"vocatogether/backend/internal/ratelimit"
)

// Options configures New. The zero value (used by tests) trusts no proxy,
// sends no HSTS and applies no per-IP or per-user limits.
type Options struct {
	// TrustedProxyHops is config.Config.TrustedProxyHops (see clientKey).
	TrustedProxyHops int
	// HSTS sends Strict-Transport-Security (production).
	HSTS       bool
	IPLimits   IPLimits
	UserLimits UserLimits
}

// New returns the API's router. Dependencies are built by the caller (main).
func New(logger *slog.Logger, authSvc *auth.Service, profileSvc *profile.Service, languageSvc *language.Service,
	avatarSvc *avatar.Service, opts Options) http.Handler {
	lim := opts.IPLimits
	perIP := func(l *ratelimit.Limiter[netip.Prefix]) func(http.Handler) http.Handler {
		return limitByIP(l, opts.TrustedProxyHops, writeRateLimited)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	// Public auth routes, each behind its per-IP limit (decision 018).
	mux.Handle("POST /v1/auth/register", perIP(lim.Register)(handleRegister(logger, authSvc)))
	mux.Handle("POST /v1/auth/verify-email", perIP(lim.Token)(handleVerifyEmail(logger, authSvc)))
	mux.Handle("POST /v1/auth/resend-verification", perIP(lim.Email)(handleResendVerification(logger, authSvc)))
	mux.Handle("POST /v1/auth/forgot-password", perIP(lim.Email)(handleForgotPassword(logger, authSvc)))
	mux.Handle("POST /v1/auth/reset-password", perIP(lim.Token)(handleResetPassword(logger, authSvc)))
	mux.Handle("POST /v1/auth/login", perIP(lim.Login)(handleLogin(logger, authSvc)))
	mux.Handle("POST /v1/auth/google", perIP(lim.Login)(handleGoogleSignIn(logger, authSvc)))
	mux.Handle("POST /v1/auth/refresh", perIP(lim.Refresh)(handleRefresh(logger, authSvc)))
	mux.HandleFunc("POST /v1/auth/logout", handleLogout(logger, authSvc))

	// Pages opened from emailed links (decision 006). GET only renders; POST
	// calls the same service as the JSON API and shares its limit.
	mux.HandleFunc("GET /reset-password", handleResetPasswordPage())
	mux.Handle("POST /reset-password",
		limitByIP(lim.Token, opts.TrustedProxyHops, writeRateLimitedPage)(handleResetPasswordForm(logger, authSvc)))
	mux.HandleFunc("GET /verify-email", handleVerifyEmailPage())
	mux.Handle("POST /verify-email",
		limitByIP(lim.Token, opts.TrustedProxyHops, writeRateLimitedVerifyPage)(handleVerifyEmailForm(logger, authSvc)))

	// Protected routes: each is wrapped individually, so public routes never
	// require a token and a route can't become public by accident of order.
	authn := requireAccessToken(logger, authSvc)
	mux.Handle("GET /v1/me", authn(handleMe(logger, authSvc)))
	// The caller's own profile (decision 027): no id in the route, the
	// session decides whose it is. Writes are limited per user, after
	// authentication, so only the user's own requests spend their allowance.
	profileWrites := limitByUser(logger, opts.UserLimits.ProfileWrite)
	mux.Handle("GET /v1/me/profile", authn(handleGetProfile(logger, profileSvc)))
	mux.Handle("PUT /v1/me/profile", authn(profileWrites(handlePutProfile(logger, profileSvc))))
	// The catalog and the caller's own languages (decision 029), under the
	// same rules: signed-in members only, no id in a route, and the write
	// behind its own per-user limit.
	languageWrites := limitByUser(logger, opts.UserLimits.LanguagesWrite)
	mux.Handle("GET /v1/languages", authn(handleLanguageCatalog(logger, languageSvc)))
	mux.Handle("GET /v1/me/languages", authn(handleGetLanguages(logger, languageSvc)))
	mux.Handle("PUT /v1/me/languages", authn(languageWrites(handlePutLanguages(logger, languageSvc))))
	// The caller's own picture (decision 031), under the same rules. Setting
	// and removing it share one per-user limit.
	avatarWrites := limitByUser(logger, opts.UserLimits.AvatarWrite)
	mux.Handle("GET /v1/me/avatar", authn(handleGetAvatar(logger, avatarSvc)))
	mux.Handle("PUT /v1/me/avatar", authn(avatarWrites(handlePutAvatar(logger, avatarSvc))))
	mux.Handle("DELETE /v1/me/avatar", authn(avatarWrites(handleDeleteAvatar(logger, avatarSvc))))
	// Another member's public profile and picture (decision 031), named by
	// the profile's public id. A route that names a member is a GET and
	// nothing else: every write stays under /v1/me. Reads are limited per
	// reader, the two routes sharing one allowance.
	memberReads := limitByUser(logger, opts.UserLimits.MemberRead)
	mux.Handle("GET /v1/profiles/{id}",
		authn(memberReads(handleGetMemberProfile(logger, profileSvc, languageSvc, avatarSvc))))
	mux.Handle("GET /v1/profiles/{id}/avatar",
		authn(memberReads(handleGetMemberAvatar(logger, profileSvc, avatarSvc))))

	return securityHeaders(opts.HSTS)(requestDeadline(requestTimeout)(mux))
}
