package auth

import (
	"errors"
	"strings"
	"time"
)

// FieldError is a validation failure on one input field. Field and Code are
// stable identifiers the API returns so clients can show their own messages.
type FieldError struct {
	Field string
	Code  string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Code }

var (
	ErrEmailInvalid        = &FieldError{Field: "email", Code: "invalid"}
	ErrPasswordRequired    = &FieldError{Field: "password", Code: "required"}
	ErrPasswordTooShort    = &FieldError{Field: "password", Code: "too_short"}
	ErrPasswordTooLong     = &FieldError{Field: "password", Code: "too_long"}
	ErrPasswordTooCommon   = &FieldError{Field: "password", Code: "too_common"}
	ErrPasswordSameAsEmail = &FieldError{Field: "password", Code: "same_as_email"}
	// ErrTokenInvalid covers malformed, unknown, expired, used and
	// wrong-purpose tokens alike, so responses don't reveal token history.
	ErrTokenInvalid = &FieldError{Field: "token", Code: "invalid"}
	// ErrRefreshTokenRequired is the only field error refresh returns; every
	// unusable non-empty token is ErrInvalidRefreshToken instead.
	ErrRefreshTokenRequired = &FieldError{Field: "refresh_token", Code: "required"}
	// ErrIDTokenRequired is the only field error Google sign-in returns.
	ErrIDTokenRequired = &FieldError{Field: "id_token", Code: "required"}
)

// Login outcomes other than success and invalid input.
var (
	// ErrInvalidCredentials covers an unknown email and a wrong password
	// alike (and any other reason the password can't be accepted), so the
	// result doesn't reveal which addresses have accounts.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrEmailNotVerified is returned only after the password was verified.
	ErrEmailNotVerified = errors.New("auth: email not verified")
)

// ErrInvalidRefreshToken covers every refresh token that can't be used:
// malformed, unknown, expired, revoked, and reused (which also revokes its
// session). One error for all keeps the response from revealing which.
var ErrInvalidRefreshToken = errors.New("auth: invalid refresh token")

// ErrInvalidAccessToken means a request carried no usable access token.
// Logout returns it only when there is no well-formed one (missing, another
// scheme, or not in the vt_at_ format), which reveals nothing about sessions
// since the format is public. Authenticate also returns it for a token that
// is unknown, rotated out, revoked or expired, so protected endpoints can't
// tell those apart either.
var ErrInvalidAccessToken = errors.New("auth: invalid access token")

// Google sign-in outcomes other than success, invalid input and the shared
// rate-limit and context errors (decision 020).
var (
	// ErrInvalidGoogleToken covers every ID token that can't be used:
	// rejected by the verifier for any reason, or Google sign-in not
	// configured. One error for all keeps the response from
	// revealing which.
	ErrInvalidGoogleToken = errors.New("auth: invalid google token")
	// ErrGoogleEmailUnusable means the token is valid and its identity is not
	// linked to any account, but its email can't create one: missing, not
	// verified by Google, or rejected by NormalizeEmail.
	ErrGoogleEmailUnusable = errors.New("auth: google email unusable")
	// ErrAccountExists means the token is valid and its identity is not
	// linked, but its email already belongs to an account. Nothing is linked
	// automatically.
	ErrAccountExists = errors.New("auth: account exists")
	// ErrGoogleUnavailable means Google's signing keys couldn't be obtained,
	// so nothing about the token was decided and nothing was written: a retry
	// with the same token is safe.
	ErrGoogleUnavailable = errors.New("auth: google unavailable")
)

// ValidationError reports every invalid field of a request at once, so
// clients can show all problems together. Its message names fields and codes
// only, never the submitted values.
type ValidationError struct {
	Fields []*FieldError
}

func (e *ValidationError) Error() string {
	codes := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		codes[i] = f.Error()
	}
	return "auth: invalid input: " + strings.Join(codes, ", ")
}

// validationError collects the field errors among errs. It returns nil if
// every err is nil, and any error that is not a *FieldError unchanged.
func validationError(errs ...error) error {
	var fields []*FieldError
	for _, err := range errs {
		if err == nil {
			continue
		}
		var fe *FieldError
		if !errors.As(err, &fe) {
			return err
		}
		fields = append(fields, fe)
	}
	if len(fields) == 0 {
		return nil
	}
	return &ValidationError{Fields: fields}
}

// RateLimitedError means a per-account limit refused the request before any
// work was done (decision 018). It is returned identically for every address,
// whether or not it has an account, so it reveals nothing about accounts.
// RetryAfter is when the next attempt will be allowed.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string { return "auth: rate limited" }

// ErrOverloaded means the request waited hashQueueTimeout for an argon2 slot
// without getting one. Every flow that hashes shares one queue, so it says
// nothing about the account either.
var ErrOverloaded = errors.New("auth: overloaded")
