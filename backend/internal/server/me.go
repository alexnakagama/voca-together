package server

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"vocatogether/backend/internal/auth"
)

// meResponse is the caller's own identity. It lists its fields explicitly so
// nothing secret (password hash, tokens, session data) can leak into it.
type meResponse struct {
	ID              string    `json:"id"`
	Email           string    `json:"email"`
	EmailVerifiedAt time.Time `json:"email_verified_at"`
	CreatedAt       time.Time `json:"created_at"`
}

// errNoIdentity means a handler that needs an identity was mounted without
// requireAccessToken: a programming error, answered with an opaque 500.
var errNoIdentity = errors.New("server: handler not behind requireAccessToken")

// handleMe answers 200 with the authenticated user's identity. It must run
// behind requireAccessToken, which also sets no-store. The body is never read.
func handleMe(logger *slog.Logger, svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := identityFrom(r.Context())
		if !ok {
			writeServiceError(w, r, logger, errNoIdentity)
			return
		}
		u, err := svc.User(r.Context(), id.UserID)
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, meResponse{
			ID:              u.ID,
			Email:           u.Email,
			EmailVerifiedAt: u.EmailVerifiedAt,
			CreatedAt:       u.CreatedAt,
		})
	}
}
