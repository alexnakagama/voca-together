package server

import (
	"log/slog"
	"net/http"
	"strings"

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

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type googleSignInRequest struct {
	IDToken string `json:"id_token"`
}

// tokenResponse is the body of a successful login or refresh. It follows the
// OAuth 2.0 token response shape (RFC 6749 5.1). expires_in is relative
// seconds, so client clock skew doesn't matter.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// handleRegister always answers 202 for valid input, whether or not the
// address already has an account (no account enumeration). 202 means
// accepted, not that an email was delivered (decision 018).
func handleRegister(logger *slog.Logger, svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
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
		w.Header().Set("Cache-Control", "no-store")
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
		w.Header().Set("Cache-Control", "no-store")
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

// handleForgotPassword always answers 202 for a valid address, whether or
// not it has an account (no account enumeration). Every response is marked
// no-store, so all outcomes carry identical headers.
func handleForgotPassword(logger *slog.Logger, svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		var req forgotPasswordRequest
		if err := decodeJSON(w, r, &req, maxAuthBodyBytes); err != nil {
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
		if err := svc.ForgotPassword(r.Context(), req.Email); err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	}
}

// handleResetPassword answers 200 once the new password is committed and all
// sessions are revoked (decision 017). It does not log the user in. Every
// unusable token gets the same 422 token:invalid. Every response, errors
// included, is marked no-store: the request carries a secret and a password.
func handleResetPassword(logger *slog.Logger, svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		var req resetPasswordRequest
		if err := decodeJSON(w, r, &req, maxAuthBodyBytes); err != nil {
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
		if err := svc.ResetPassword(r.Context(), req.Token, req.Password); err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "password_reset"})
	}
}

// handleLogin answers 200 with new session credentials. Unknown email and
// wrong password get the same 401 (no account enumeration); 403
// email_not_verified comes only after a correct password.
//
// Every response, including errors, is marked no-store: success carries
// credentials, and identical headers on all failures keep them
// indistinguishable. Any credentials the request carries are ignored: each
// login gets new server-generated tokens (no session fixation).
func handleLogin(logger *slog.Logger, svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		var req loginRequest
		if err := decodeJSON(w, r, &req, maxAuthBodyBytes); err != nil {
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
		res, err := svc.Login(r.Context(), req.Email, req.Password, r.UserAgent())
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeTokens(w, res)
	}
}

// handleRefresh answers 200 with the session's rotated credentials. Every
// unusable token (malformed, unknown, expired, revoked, reused) gets the same
// 401, so clients can't tell them apart and simply log in again. Any
// Authorization header is ignored: refreshing must work once the access
// token has expired. Every response is marked no-store, as for login.
func handleRefresh(logger *slog.Logger, svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		var req refreshRequest
		if err := decodeJSON(w, r, &req, maxAuthBodyBytes); err != nil {
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
		res, err := svc.Refresh(r.Context(), req.RefreshToken)
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeTokens(w, res)
	}
}

// handleGoogleSignIn answers 200 with new session credentials for the
// account of a Google ID token, created first if needed (decision 020); the
// body is the same as login's. Every unusable token (rejected by the
// verifier, or Google sign-in not configured) gets the same 401
// invalid_google_token, so clients can't tell them apart. A token that
// verifies is accepted every time it is presented (decision 026). 403 google_email_unusable and 409
// account_exists come only after the token verified. The 401 carries no
// WWW-Authenticate: as for login, the token is a credential in the body, not
// an HTTP authentication scheme.
//
// Only the body carries the ID token; the Authorization header and the query
// string are ignored. Every response is marked no-store, as for login. A 503
// or 500 may come after the session was committed; a retry, with the same ID
// token or a new one, creates another session (requestTimeout).
func handleGoogleSignIn(logger *slog.Logger, svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		var req googleSignInRequest
		if err := decodeJSON(w, r, &req, maxAuthBodyBytes); err != nil {
			writeError(w, http.StatusBadRequest, codeInvalidRequest)
			return
		}
		res, err := svc.SignInWithGoogle(r.Context(), req.IDToken, r.UserAgent())
		if err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		writeTokens(w, res)
	}
}

// handleLogout revokes the session of the request's Bearer access token and
// answers 204 (decision 015). Any well-formed access token gets the same 204,
// whether its session was revoked now, already revoked, expired, or unknown,
// so logout is idempotent and reveals nothing. A missing or malformed
// credential gets 401, so a client bug can't pass for a logout. The body is
// never read. Every response is marked no-store, as for login.
func handleLogout(logger *slog.Logger, svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if err := svc.Logout(r.Context(), bearerToken(r)); err != nil {
			writeServiceError(w, r, logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// bearerToken returns the token of the request's single Authorization header
// if it uses the Bearer scheme (RFC 6750 2.1, scheme case-insensitive per
// RFC 7235), else "". The token is not trimmed or otherwise validated: the
// service rejects anything that isn't exactly a well-formed token.
func bearerToken(r *http.Request) string {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return ""
	}
	scheme, token, found := strings.Cut(values[0], " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return token
}

// writeTokens writes credentials as a 200 tokenResponse. expires_in is
// rounded down, so a client never believes a token lives longer than it does.
func writeTokens(w http.ResponseWriter, c auth.Credentials) {
	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  c.AccessToken.Raw,
		TokenType:    "Bearer",
		ExpiresIn:    int(c.ExpiresIn.Seconds()),
		RefreshToken: c.RefreshToken.Raw,
	})
}
