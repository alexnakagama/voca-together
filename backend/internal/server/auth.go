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

type verifyEmailRequest struct {
	Token string `json:"token"`
}

type resendVerificationRequest struct {
	Email string `json:"email"`
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
		if err := svc.Register(r.Context(), req.Email, req.Password); err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	}
}

// handleVerifyEmail answers 200 once the email is verified. It does not log
// the user in.
func handleVerifyEmail(logger *slog.Logger, svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req verifyEmailRequest
		if err := decodeJSON(w, r, &req, maxAuthBodyBytes); err != nil {
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
		if err := svc.VerifyEmail(r.Context(), req.Token); err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "verified"})
	}
}

// handleResendVerification always answers 202 for a valid address, whether
// or not it has an account and whatever its state (no account enumeration).
func handleResendVerification(logger *slog.Logger, svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req resendVerificationRequest
		if err := decodeJSON(w, r, &req, maxAuthBodyBytes); err != nil {
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
		if err := svc.ResendVerification(r.Context(), req.Email); err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	}
}

// writeServiceError maps an auth service error to a response: validation
// errors to 422 with their fields, anything else to an opaque 500 whose
// details go only to the log. Service errors never contain secrets.
func writeServiceError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	var verr *auth.ValidationError
	if errors.As(err, &verr) {
		writeValidationError(w, verr)
		return
	}
	logger.ErrorContext(r.Context(), "request failed", "route", r.Pattern, "err", err)
	writeError(w, http.StatusInternalServerError, codeInternalError)
}

func writeValidationError(w http.ResponseWriter, verr *auth.ValidationError) {
	fields := make([]fieldError, len(verr.Fields))
	for i, f := range verr.Fields {
		fields[i] = fieldError{Field: f.Field, Code: f.Code}
	}
	writeJSON(w, http.StatusUnprocessableEntity,
		errorResponse{Error: errorBody{Code: codeValidationFailed, Fields: fields}})
}
