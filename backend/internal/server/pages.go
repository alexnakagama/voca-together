package server

import (
	"bytes"
	"embed"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"vocatogether/backend/internal/auth"
)

// Pages opened from emailed links (decision 006). GET only renders, so mail
// scanners that prefetch links change nothing; POST calls the same service as
// the JSON API.

//go:embed pages/*.html
var pageFiles embed.FS

var resetPasswordPage = template.Must(template.ParseFS(pageFiles, "pages/reset_password.html"))

// Page states of resetPasswordPage.
const (
	pageForm        = "form"
	pageDone        = "done"
	pageInvalid     = "invalid"
	pageBadRequest  = "bad_request"
	pageRateLimited = "rate_limited"
	pageUnavailable = "unavailable"
	pageError       = "error"
)

type resetPasswordPageData struct {
	State          string
	Token          string // well-formed only: rendered into the hidden form field
	PasswordErrors []string
	ConfirmError   string
	MinLength      int
	MaxLength      int
}

// passwordErrorText is the page's message for each password field code.
var passwordErrorText = map[string]string{
	auth.ErrPasswordTooShort.Code:    "The password is too short.",
	auth.ErrPasswordTooLong.Code:     "The password is too long.",
	auth.ErrPasswordTooCommon.Code:   "This password is too common. Choose one that is harder to guess.",
	auth.ErrPasswordSameAsEmail.Code: "The password must not be your email address.",
}

// handleResetPasswordPage renders the form for the token in the emailed link.
// It never touches the database or uses the token: only POST does. A token
// that isn't well-formed gets the invalid-link page, and is not echoed.
func handleResetPasswordPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if !auth.WellFormedResetToken(token) {
			writePage(w, http.StatusBadRequest, resetPasswordPageData{State: pageInvalid})
			return
		}
		writePage(w, http.StatusOK, formPage(token))
	}
}

// handleResetPasswordForm resets the password from the submitted form. Only
// the form body counts: a token in the query string is ignored. CSRF
// protection isn't needed: the form carries the secret token itself, and
// making a victim's browser reset the attacker's own account gains nothing.
func handleResetPasswordForm(logger *slog.Logger, svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
		if err := r.ParseForm(); err != nil {
			writePage(w, http.StatusBadRequest, resetPasswordPageData{State: pageBadRequest})
			return
		}
		token := r.PostForm.Get("token")
		password := r.PostForm.Get("password")
		if !auth.WellFormedResetToken(token) {
			writePage(w, http.StatusBadRequest, resetPasswordPageData{State: pageInvalid})
			return
		}
		// A typing mistake is a UI concern: the service isn't called.
		if password != r.PostForm.Get("password_confirm") {
			page := formPage(token)
			page.ConfirmError = "The passwords do not match."
			writePage(w, http.StatusUnprocessableEntity, page)
			return
		}

		err := svc.ResetPassword(r.Context(), token, password)
		var verr *auth.ValidationError
		switch {
		case err == nil:
			writePage(w, http.StatusOK, resetPasswordPageData{State: pageDone})
		case errors.As(err, &verr):
			page := formPage(token)
			for _, f := range verr.Fields {
				if f == auth.ErrTokenInvalid {
					writePage(w, http.StatusBadRequest, resetPasswordPageData{State: pageInvalid})
					return
				}
				if text, ok := passwordErrorText[f.Code]; ok && f.Field == "password" {
					page.PasswordErrors = append(page.PasswordErrors, text)
				}
			}
			writePage(w, http.StatusUnprocessableEntity, page)
		case unavailable(err):
			logger.WarnContext(r.Context(), "request unavailable", "route", r.Pattern, "err", err)
			setRetryAfter(w, retryAfterUnavailable)
			writePage(w, http.StatusServiceUnavailable, resetPasswordPageData{State: pageUnavailable})
		default:
			logger.ErrorContext(r.Context(), "request failed", "route", r.Pattern, "err", err)
			writePage(w, http.StatusInternalServerError, resetPasswordPageData{State: pageError})
		}
	}
}

// writeRateLimitedPage is the reset form's 429, for the per-IP limit it
// shares with the JSON API. Nothing was done: the link is still usable.
func writeRateLimitedPage(w http.ResponseWriter, retryAfter time.Duration) {
	setRetryAfter(w, retryAfter)
	writePage(w, http.StatusTooManyRequests, resetPasswordPageData{State: pageRateLimited})
}

func formPage(token string) resetPasswordPageData {
	return resetPasswordPageData{
		State:     pageForm,
		Token:     token,
		MinLength: auth.PasswordMinLength,
		MaxLength: auth.PasswordMaxLength,
	}
}

// writePage renders the reset page with headers for a page whose URL and
// form carry a secret: never cached, never sent as a referrer, never framed,
// and allowed to load nothing but its own inline styles and post only to
// this origin.
func writePage(w http.ResponseWriter, status int, data resetPasswordPageData) {
	var buf bytes.Buffer
	if err := resetPasswordPage.Execute(&buf, data); err != nil {
		// Only a programming error in the template can get here.
		status = http.StatusInternalServerError
		buf.Reset()
		buf.WriteString("Internal Server Error\n")
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}
