package googleid

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"unicode/utf8"
)

// maxObjectMembers bounds the members of any object parsed here. Google ID
// tokens carry about 15 claims, key-set entries about 6 fields.
const maxObjectMembers = 64

var errBadJSON = errors.New("googleid: bad json")

// parseObject parses b as exactly one JSON object and returns its members as
// raw values, keyed by their unescaped names. It rejects invalid UTF-8,
// anything but an object (arrays, scalars), duplicate member names
// (encoding/json would silently keep the last, so "alg" could be given twice
// with different meanings), more than maxObjectMembers members, and any data
// after the object. Names match exactly: unlike decoding into a struct,
// "ALG" is not "alg". Nested values are kept raw and not inspected.
func parseObject(b []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(b) {
		return nil, errBadJSON
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errBadJSON
	}
	members := make(map[string]json.RawMessage)
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, errBadJSON
		}
		name, ok := tok.(string)
		if !ok {
			return nil, errBadJSON
		}
		if _, dup := members[name]; dup || len(members) == maxObjectMembers {
			return nil, errBadJSON
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, errBadJSON
		}
		members[name] = raw
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, errBadJSON
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errBadJSON // trailing data
	}
	return members, nil
}

// jsonString decodes raw as a JSON string; null, numbers and every other
// type are rejected rather than coerced.
func jsonString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// jsonBool decodes raw as a JSON boolean; "true" as a string, 1 and null are
// rejected.
func jsonBool(raw json.RawMessage) (bool, bool) {
	switch string(raw) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// maxUnixSeconds is the last second of year 9999. Larger timestamps are
// rejected, which keeps every time computation far from overflow.
const maxUnixSeconds = 253402300799

// jsonUnixSeconds decodes raw as a NumericDate (RFC 7519 2) restricted to a
// non-negative integer of at most maxUnixSeconds: no sign, fraction,
// exponent or string form. Google issues plain integers.
func jsonUnixSeconds(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || len(raw) > 12 {
		return 0, false
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || n > maxUnixSeconds {
		return 0, false
	}
	return n, true
}

// printableASCII reports whether s is non-empty, at most maxLen bytes, and
// only visible ASCII (0x21–0x7e): no spaces, controls or non-ASCII.
func printableASCII(s string, maxLen int) bool {
	if s == "" || len(s) > maxLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}
