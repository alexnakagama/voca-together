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
	ErrPasswordTooShort    = &FieldError{Field: "password", Code: "too_short"}
	ErrPasswordTooLong     = &FieldError{Field: "password", Code: "too_long"}
	ErrPasswordTooCommon   = &FieldError{Field: "password", Code: "too_common"}
	ErrPasswordSameAsEmail = &FieldError{Field: "password", Code: "same_as_email"}
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
