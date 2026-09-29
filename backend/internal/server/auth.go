package server

import (
	"errors"
	"log/slog"
	"net/http"

	"vocatogether/backend/internal/auth"
)

// maxAuthBodyBytes comfortably fits a 254-byte email and a 128-code-point
// password even fully \u-escaped, and bounds the work done on the password.
const maxAuthBodyBytes = 8 << 10

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleRegister always answers 202 for valid input, whether or not the
// address already has an account (no account enumeration).
func handleRegister(logger *slog.Logger, svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req registerRequest
		if err := decodeJSON(w, r, &req, maxAuthBodyBytes); err != nil {
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}

		err := svc.Register(r.Context(), req.Email, req.Password)
		var verr *auth.ValidationError
		switch {
		case err == nil:
			writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
		case errors.As(err, &verr):
			writeValidationError(w, verr)
		default:
			logger.ErrorContext(r.Context(), "register failed", "err", err)
			writeError(w, http.StatusInternalServerError, codeInternalError)
		}
	}
}

func writeValidationError(w http.ResponseWriter, verr *auth.ValidationError) {
	fields := make([]fieldError, len(verr.Fields))
	for i, f := range verr.Fields {
		fields[i] = fieldError{Field: f.Field, Code: f.Code}
	}
	writeJSON(w, http.StatusUnprocessableEntity,
		errorResponse{Error: errorBody{Code: codeValidationFailed, Fields: fields}})
}
