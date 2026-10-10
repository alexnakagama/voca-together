package auth

import (
	"net/mail"
	"strings"
)

const (
	maxEmailLength = 254 // RFC 5321 path limit; matches the users_email_length CHECK
	maxLocalLength = 64
	maxLabelLength = 63
)

// NormalizeEmail trims and lowercases an address and validates it as a plain
// "local@domain" address. It returns ErrEmailInvalid otherwise.
//
// Deliberately stricter than RFC 5322: no display names, quoted local parts,
// comments, IP-literal or IP-like domains, and only printable ASCII.
// ASCII-only is a product decision (docs/decisions.md), because:
//   - delivery: non-ASCII addresses need SMTPUTF8 support end to end, which
//     email providers support unevenly, and verification must be deliverable;
//   - look-alikes: Unicode confusables (Cyrillic "а" vs Latin "a") would allow
//     distinct accounts that look identical;
//   - canonicalization: Unicode addresses have several normalization and
//     case-folding forms, so one mailbox could map to several stored strings.
//
// Rejecting whitespace and control characters (below) is what keeps
// addresses safe to place in email headers; that holds independently of
// the ASCII restriction.
func NormalizeEmail(input string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(input))
	if email == "" || len(email) > maxEmailLength {
		return "", ErrEmailInvalid
	}
	for i := 0; i < len(email); i++ {
		if c := email[i]; c <= ' ' || c > '~' || c == '"' {
			return "", ErrEmailInvalid
		}
	}

	// net/mail validates the local part's dot-atom syntax. Requiring the
	// parsed address to equal the input states "exactly one plain address";
	// it is defence in depth, as the checks above/below also reject the
	// wrapped forms net/mail accepts ("<a@b.c>", "a@b.c (comment)").
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Name != "" || addr.Address != email {
		return "", ErrEmailInvalid
	}

	local, domain, _ := strings.Cut(email, "@")
	if len(local) > maxLocalLength || !validDomain(domain) {
		return "", ErrEmailInvalid
	}
	return email, nil
}

// validDomain requires at least two dot-separated labels of letters, digits
// and inner hyphens. The final label (TLD) must not be all digits, which
// rejects IP-like domains such as "1.2.3.4".
func validDomain(domain string) bool {
	labels := strings.Split(domain, ".")
	if len(labels) < 2 || allDigits(labels[len(labels)-1]) {
		return false
	}
	for _, label := range labels {
		if label == "" || len(label) > maxLabelLength || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
