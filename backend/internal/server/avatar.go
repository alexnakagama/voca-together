package server

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"vocatogether/backend/internal/avatar"
)

// writeImage answers 200 with a stored picture. Every stored picture is a
// JPEG the server produced itself (avatar.Service), so the type is constant
// and never taken from a request. The caller's middleware has set no-store,
// and securityHeaders nosniff.
func writeImage(w http.ResponseWriter, image []byte) {
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(image)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(image)
}

// handleGetAvatar answers 200 with the authenticated user's picture, as
// image/jpeg, or 404 avatar_not_found if they have none. It must run behind
// requireAccessToken, which also sets no-store. The body is never read.
func handleGetAvatar(logger *slog.Logger, svc *avatar.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := identityFrom(r.Context())
		if !ok {
			writeServiceError(w, r, logger, errNoIdentity)
			return
		}
		image, err := svc.Get(r.Context(), id.UserID)
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeImage(w, image)
	}
}

// handlePutAvatar makes the body the authenticated user's picture, replacing
// any earlier one, and answers 200 with the picture as stored, whether it
// was set, replaced or already made from these same bytes. It must run
// behind requireAccessToken. Whose picture it is comes only from the
// session; the request can't name another user.
//
// The body is the image itself, not a form. What it is is decided by its
// content alone (avatar.Service.Save): the Content-Type header is never
// read. At most one byte more than the largest accepted upload is read, so a
// longer body is recognised as too large without being buffered, and is
// answered like any other photo a member can replace with another one: 422
// too_large on the field avatar, not 400 (decision 031).
func handlePutAvatar(logger *slog.Logger, svc *avatar.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := identityFrom(r.Context())
		if !ok {
			writeServiceError(w, r, logger, errNoIdentity)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, avatar.MaxUploadBytes+1))
		var tooLong *http.MaxBytesError
		if err != nil && !errors.As(err, &tooLong) {
			// The body could not be read whole (the client went away or was
			// too slow): there is no upload to judge.
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
		image, err := svc.Save(r.Context(), id.UserID, raw)
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeImage(w, image)
	}
}

// handleDeleteAvatar removes the authenticated user's picture and answers
// 204, also when there was none. It must run behind requireAccessToken. The
// body is never read.
func handleDeleteAvatar(logger *slog.Logger, svc *avatar.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := identityFrom(r.Context())
		if !ok {
			writeServiceError(w, r, logger, errNoIdentity)
			return
		}
		if err := svc.Delete(r.Context(), id.UserID); err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
