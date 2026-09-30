package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// Error responses have the shape
//
//	{"error":{"code":"<code>","fields":[{"field":"<field>","code":"<code>"}]}}
//
// where fields is present only for validation_failed. Codes are stable
// identifiers; clients show their own messages.
const (
	codeInvalidRequest   = "invalid_request"
	codeValidationFailed = "validation_failed"
	codeInternalError    = "internal_error"

	codeInvalidCredentials = "invalid_credentials"
	codeEmailNotVerified   = "email_not_verified"
)

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code   string       `json:"code"`
	Fields []fieldError `json:"fields,omitempty"`
}

type fieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code}})
}

// decodeJSON reads exactly one JSON value of at most maxBytes into dst,
// rejecting unknown fields and any data after the value.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after JSON value")
	}
	return nil
}
