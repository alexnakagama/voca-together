package language

import (
	"errors"
	"strings"
)

// The codes of a FieldError. The field is always a Kind: errors are reported
// per list, not per item.
const (
	codeTooMany         = "too_many"
	codeUnknownLanguage = "unknown_language"
	codeInvalidLevel    = "invalid_level"
	codeDuplicate       = "duplicate"
)

// FieldError is a validation failure on one input field. Field and Code are
// stable identifiers the API returns so clients can show their own messages.
type FieldError struct {
	Field string
	Code  string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Code }

// ValidationError reports every invalid field of a request at once, so
// clients can show all problems together. Its message names fields and codes
// only, never the languages or levels submitted.
type ValidationError struct {
	Fields []*FieldError
}

func (e *ValidationError) Error() string {
	codes := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		codes[i] = f.Error()
	}
	return "language: invalid input: " + strings.Join(codes, ", ")
}

// ErrUserGone means the user no longer exists, so no languages can be saved
// for it. Deleting a user deletes its sessions, so the credential that
// authenticated the request is dead.
var ErrUserGone = errors.New("language: user gone")
