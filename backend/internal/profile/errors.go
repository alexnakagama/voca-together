package profile

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
	ErrDisplayNameRequired = &FieldError{Field: "display_name", Code: "required"}
	ErrDisplayNameTooLong  = &FieldError{Field: "display_name", Code: "too_long"}
	// ErrDisplayNameInvalid covers characters a name may not contain and
	// names with nothing to read (no letter or digit).
	ErrDisplayNameInvalid = &FieldError{Field: "display_name", Code: "invalid"}
	ErrBioTooLong         = &FieldError{Field: "bio", Code: "too_long"}
	ErrBioInvalid         = &FieldError{Field: "bio", Code: "invalid"}
)

// ValidationError reports every invalid field of a request at once, so
// clients can show all problems together. Its message names fields and codes
// only, never the submitted text.
type ValidationError struct {
	Fields []*FieldError
}

func (e *ValidationError) Error() string {
	codes := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		codes[i] = f.Error()
	}
	return "profile: invalid input: " + strings.Join(codes, ", ")
}

// ErrNotFound means the user has not saved a profile yet.
var ErrNotFound = errors.New("profile: not found")

// ErrUserGone means the user no longer exists, so no profile can be saved for
// it. Deleting a user deletes its sessions, so the credential that
// authenticated the request is dead.
var ErrUserGone = errors.New("profile: user gone")
