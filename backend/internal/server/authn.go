package server

import (
	"context"
	"log/slog"
	"net/http"

	"vocatogether/backend/internal/auth"
)

type identityKey struct{}

// requireAccessToken returns middleware that runs next only for a request
// whose Bearer access token belongs to a live session, with that session's
// auth.Identity in the request context (see identityFrom). Every other
// request gets 401 invalid_access_token with WWW-Authenticate: Bearer, or an
// opaque 500 if the check itself fails (decision 016).
//
// Every response is marked no-store, errors included: protected responses
// are specific to the caller, and identical failure headers keep failures
// indistinguishable. Only the Authorization header counts; a token in the
// query string or body is ignored.
func requireAccessToken(logger *slog.Logger, svc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			id, err := svc.Authenticate(r.Context(), bearerToken(r))
			if err != nil {
				writeServiceError(w, r, logger, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
		})
	}
}

// identityFrom returns the identity requireAccessToken authenticated. ok is
// false only for a handler that isn't behind requireAccessToken.
func identityFrom(ctx context.Context) (id auth.Identity, ok bool) {
	id, ok = ctx.Value(identityKey{}).(auth.Identity)
	return id, ok
}
