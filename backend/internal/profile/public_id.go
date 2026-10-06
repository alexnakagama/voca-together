package profile

// publicIDLength is the length of a UUID in its canonical text form.
const publicIDLength = 36

// ParsePublicID reports whether s is a well-formed public identifier and
// returns it. The only accepted spelling is the canonical one PostgreSQL
// prints: 36 characters, lowercase hexadecimal, hyphens in place.
//
// An identifier is not text: nothing is trimmed or lower-cased, so every
// profile has exactly one spelling and anything else names nothing
// (decision 031).
func ParsePublicID(s string) (string, bool) {
	if len(s) != publicIDLength {
		return "", false
	}
	for i := range len(s) {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return "", false
			}
		default:
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return "", false
			}
		}
	}
	return s, true
}
