package auth

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
