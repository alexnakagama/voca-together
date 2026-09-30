package auth

import (
	"errors"
	"strings"
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
