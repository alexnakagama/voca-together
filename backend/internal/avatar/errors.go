package avatar

import (
	"errors"
	"strings"
)

// FieldError is a validation failure of the upload. Field and Code are
// stable identifiers the API returns so clients can show their own messages.
type FieldError struct {
	Field string
	Code  string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Code }

// The ways an upload is refused. There is one field, the picture itself.
var (
	// ErrRequired: the upload is empty.
	ErrRequired = &FieldError{Field: "avatar", Code: "required"}
	// ErrTooLarge: more than MaxUploadBytes.
	ErrTooLarge = &FieldError{Field: "avatar", Code: "too_large"}
	// ErrUnsupportedType: the content is neither a JPEG nor a PNG.
	ErrUnsupportedType = &FieldError{Field: "avatar", Code: "unsupported_type"}
	// ErrInvalidImage: it starts as a JPEG or PNG but can't be decoded.
	ErrInvalidImage = &FieldError{Field: "avatar", Code: "invalid_image"}
	// ErrDimensionsTooLarge: wider or taller than MaxDimension.
	ErrDimensionsTooLarge = &FieldError{Field: "avatar", Code: "dimensions_too_large"}
)

// ValidationError reports why an upload was refused. It has the shape of the
// other domain packages' validation errors so the API answers it the same
// way. Its message names the field and the code only: never a byte of the
// upload, and never what a decoder said about it.
type ValidationError struct {
	Fields []*FieldError
}

func (e *ValidationError) Error() string {
	codes := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		codes[i] = f.Error()
	}
	return "avatar: invalid input: " + strings.Join(codes, ", ")
}

// refused returns the validation error for one reason.
func refused(reason *FieldError) error {
	return &ValidationError{Fields: []*FieldError{reason}}
}

// ErrOverloaded means the upload waited decodeQueueTimeout for a decode slot
// without getting one. Nothing was decoded or stored, so repeating the
// upload is safe.
var ErrOverloaded = errors.New("avatar: overloaded")
