// Package server assembles the HTTP routes of the API.
package server

import (
	"log/slog"
	"net/http"

	"vocatogether/backend/internal/auth"
)

// New returns the API's router. Dependencies are built by the caller (main).
func New(logger *slog.Logger, authSvc *auth.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/auth/register", handleRegister(logger, authSvc))
	mux.HandleFunc("POST /v1/auth/verify-email", handleVerifyEmail(logger, authSvc))
	mux.HandleFunc("POST /v1/auth/resend-verification", handleResendVerification(logger, authSvc))
	mux.HandleFunc("POST /v1/auth/login", handleLogin(logger, authSvc))
	mux.HandleFunc("POST /v1/auth/refresh", handleRefresh(logger, authSvc))
	mux.HandleFunc("POST /v1/auth/logout", handleLogout(logger, authSvc))

	// Protected routes: each is wrapped individually, so public routes never
	// require a token and a route can't become public by accident of order.
	authn := requireAccessToken(logger, authSvc)
	mux.Handle("GET /v1/me", authn(handleMe(logger, authSvc)))
	return mux
}
