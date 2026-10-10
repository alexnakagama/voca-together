package safety

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
	// ErrMemberSelf: the member a request names is the one making it.
	ErrMemberSelf = &FieldError{Field: "member", Code: "self"}
	// ErrBlocksTooMany: the member already blocks MaxBlocks members.
	ErrBlocksTooMany = &FieldError{Field: "blocks", Code: "too_many"}

	ErrReasonRequired = &FieldError{Field: "reason", Code: "required"}
	// ErrReasonInvalid: the reason is not one of Reasons, written exactly.
	ErrReasonInvalid  = &FieldError{Field: "reason", Code: "invalid"}
	ErrDetailsTooLong = &FieldError{Field: "details", Code: "too_long"}
	// ErrDetailsInvalid: the details are not valid text, or hold a control
	// character that is neither a line break nor a tab.
	ErrDetailsInvalid = &FieldError{Field: "details", Code: "invalid"}
)

// ValidationError reports every invalid field of a request at once, so
// clients can show all problems together. Its message names fields and codes
// only, never a member and nothing of what was sent.
type ValidationError struct {
	Fields []*FieldError
}

func (e *ValidationError) Error() string {
	codes := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		codes[i] = f.Error()
	}
	return "safety: invalid input: " + strings.Join(codes, ", ")
}

// ErrUserGone means the user no longer exists, so nothing can be stored for
// it. Deleting a user deletes its sessions, so the credential that
// authenticated the request is dead.
var ErrUserGone = errors.New("safety: user gone")
