package server

import (
	"log/slog"
	"net/http"
	"time"

	"vocatogether/backend/internal/profile"
)

// maxProfileBodyBytes is deliberately far above the longest valid profile
// (under 7 KiB even with every character \u-escaped as a surrogate pair).
// Text that is too long must be answered by the field's own rule, 422
// too_long, so the body limit has to sit well above anything a person
// pastes: a limit close to the field limits would refuse a long bio as a
// malformed request instead (decision 027). It still bounds what one request
// can make the server read and normalize; writes are also limited per user.
const maxProfileBodyBytes = 64 << 10

// profileRequest lists everything a profile save may set. Anything else in
// the body, an id or a timestamp included, is an unknown field and the
// request is refused (decodeJSON), so nothing becomes writable by accident.
type profileRequest struct {
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
}

// profileResponse is the caller's own profile. It lists its fields
// explicitly: no user id, no email, nothing about the account. id is the
// profile's public identifier (decision 031), not the account's.
type profileResponse struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Bio         string    `json:"bio"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func writeProfile(w http.ResponseWriter, p profile.Profile) {
	writeJSON(w, http.StatusOK, profileResponse{
		ID:          p.PublicID,
		DisplayName: p.DisplayName,
		Bio:         p.Bio,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	})
}

// handleGetProfile answers 200 with the authenticated user's profile, or 404
// profile_not_found if they haven't saved one. It must run behind
// requireAccessToken, which also sets no-store. The body is never read.
func handleGetProfile(logger *slog.Logger, svc *profile.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := identityFrom(r.Context())
		if !ok {
			writeServiceError(w, r, logger, errNoIdentity)
			return
		}
		p, err := svc.Get(r.Context(), id.UserID)
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeProfile(w, p)
	}
}

// handlePutProfile replaces the authenticated user's whole profile with the
// body, creating it if there is none, and answers 200 with the profile as
// stored, whether it was created, changed or already the same. It must run
// behind requireAccessToken. Whose profile it is comes only from the
// session; the request can't name another user.
func handlePutProfile(logger *slog.Logger, svc *profile.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := identityFrom(r.Context())
		if !ok {
			writeServiceError(w, r, logger, errNoIdentity)
			return
		}
		var req profileRequest
		if err := decodeJSON(w, r, &req, maxProfileBodyBytes); err != nil {
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
		p, err := svc.Save(r.Context(), id.UserID, profile.Input{DisplayName: req.DisplayName, Bio: req.Bio})
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeProfile(w, p)
	}
}
