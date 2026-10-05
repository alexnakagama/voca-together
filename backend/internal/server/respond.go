package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"vocatogether/backend/internal/auth"
	"vocatogether/backend/internal/language"
	"vocatogether/backend/internal/profile"
)

// Error responses have the shape
//
//	{"error":{"code":"<code>","fields":[{"field":"<field>","code":"<code>"}]}}
//
// where fields is present only for validation_failed. Codes are stable
// identifiers; clients show their own messages.
const (
	codeInvalidRequest   = "invalid_request"
	codeValidationFailed = "validation_failed"
	codeInternalError    = "internal_error"

	codeInvalidCredentials = "invalid_credentials"
	codeEmailNotVerified   = "email_not_verified"

	codeInvalidRefreshToken = "invalid_refresh_token"
	codeInvalidAccessToken  = "invalid_access_token"

	codeInvalidGoogleToken  = "invalid_google_token"
	codeGoogleEmailUnusable = "google_email_unusable"
	codeAccountExists       = "account_exists"

	codeProfileNotFound = "profile_not_found"

	codeRateLimited        = "rate_limited"
	codeServiceUnavailable = "service_unavailable"
)

// retryAfterUnavailable is the Retry-After of a 503: about one argon2 queue
// timeout, after which a retry meets a fresh queue (or Google's keys may be
// fetchable again).
const retryAfterUnavailable = 5 * time.Second

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code   string       `json:"code"`
	Fields []fieldError `json:"fields,omitempty"`
}

type fieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code}})
}

// writeRateLimited answers 429 rate_limited. Nothing was done, so retrying
// after retryAfter is always safe, including with the same refresh token.
func writeRateLimited(w http.ResponseWriter, retryAfter time.Duration) {
	w.Header().Set("Cache-Control", "no-store")
	setRetryAfter(w, retryAfter)
	writeError(w, http.StatusTooManyRequests, codeRateLimited)
}

// writeUnavailable answers 503 service_unavailable: the server is overloaded
// or the request ran out of time. Unlike a 429, work may have been committed
// (see requestTimeout).
func writeUnavailable(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	setRetryAfter(w, retryAfterUnavailable)
	writeError(w, http.StatusServiceUnavailable, codeServiceUnavailable)
}

// decodeJSON reads exactly one JSON value of at most maxBytes into dst,
// rejecting unknown fields and any data after the value.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after JSON value")
	}
	return nil
}

// writeServiceError maps a service error (auth, profile or language) to a
// response:
// validation errors to 422 with their fields, login, refresh, access-token
// and Google sign-in outcomes to 401/403/409, a missing profile to 404, a
// spent per-account limit to 429, overload, unavailable Google keys or the
// request deadline to 503, anything else to an opaque 500 whose details go
// only to the log. Service errors never contain secrets, profile text or a
// member's languages.
func writeServiceError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	var verr *auth.ValidationError
	var profileErr *profile.ValidationError
	var languageErr *language.ValidationError
	var limited *auth.RateLimitedError
	switch {
	case errors.As(err, &verr):
		writeValidationFailed(w, fieldErrors(verr.Fields))
		return
	case errors.As(err, &profileErr):
		writeValidationFailed(w, fieldErrors(profileErr.Fields))
		return
	case errors.As(err, &languageErr):
		writeValidationFailed(w, fieldErrors(languageErr.Fields))
		return
	case errors.As(err, &limited):
		writeRateLimited(w, limited.RetryAfter)
		return
	case unavailable(err):
		logger.WarnContext(r.Context(), "request unavailable", "route", r.Pattern, "err", err)
		writeUnavailable(w)
		return
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, codeInvalidCredentials)
		return
	case errors.Is(err, auth.ErrEmailNotVerified):
		writeError(w, http.StatusForbidden, codeEmailNotVerified)
		return
	case errors.Is(err, auth.ErrInvalidRefreshToken):
		writeError(w, http.StatusUnauthorized, codeInvalidRefreshToken)
		return
	case errors.Is(err, profile.ErrNotFound):
		writeError(w, http.StatusNotFound, codeProfileNotFound)
		return
	// ErrUserGone: the user was deleted after authentication, and their
	// sessions with them, so the credential is dead (decision 016).
	case errors.Is(err, auth.ErrInvalidAccessToken), errors.Is(err, profile.ErrUserGone),
		errors.Is(err, language.ErrUserGone):
		// The HTTP Bearer scheme (RFC 6750 3), unlike login's credential form.
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, codeInvalidAccessToken)
		return
	case errors.Is(err, auth.ErrInvalidGoogleToken):
		// A credential in the body, like login's: no WWW-Authenticate.
		writeError(w, http.StatusUnauthorized, codeInvalidGoogleToken)
		return
	case errors.Is(err, auth.ErrGoogleEmailUnusable):
		writeError(w, http.StatusForbidden, codeGoogleEmailUnusable)
		return
	case errors.Is(err, auth.ErrAccountExists):
		writeError(w, http.StatusConflict, codeAccountExists)
		return
	}
	logger.ErrorContext(r.Context(), "request failed", "route", r.Pattern, "err", err)
	writeError(w, http.StatusInternalServerError, codeInternalError)
}

// unavailable reports whether err means the server couldn't serve the request
// in time: the argon2 queue was full, Google's signing keys couldn't be
// fetched, or the request deadline passed (requestDeadline; a client
// disconnect is Canceled, not DeadlineExceeded).
func unavailable(err error) bool {
	return errors.Is(err, auth.ErrOverloaded) || errors.Is(err, auth.ErrGoogleUnavailable) ||
		errors.Is(err, context.DeadlineExceeded)
}

// fieldErrors converts a domain package's field errors, which all have the
// same shape, to the response's.
func fieldErrors[F auth.FieldError | profile.FieldError | language.FieldError](in []*F) []fieldError {
	out := make([]fieldError, len(in))
	for i, f := range in {
		out[i] = fieldError(*f)
	}
	return out
}

// writeValidationFailed answers 422 validation_failed with every failing
// field.
func writeValidationFailed(w http.ResponseWriter, fields []fieldError) {
	writeJSON(w, http.StatusUnprocessableEntity,
		errorResponse{Error: errorBody{Code: codeValidationFailed, Fields: fields}})
}
