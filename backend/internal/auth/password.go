package auth

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/unicode/norm"
)

// Password policy (NIST SP 800-63B): length limits and a blocklist, no
// composition rules. Lengths count Unicode code points after normalization.
const (
	PasswordMinLength = 10
	PasswordMaxLength = 128 // also bounds hashing cost
)

var ErrMalformedHash = errors.New("auth: malformed password hash")

type argonParams struct {
	memoryKiB   uint32
	iterations  uint32
	parallelism uint8
	saltLen     uint32
	keyLen      uint32
}

// OWASP baseline for argon2id. Raising these later is safe: VerifyPassword
// reports needsRehash for hashes made with older parameters.
var defaultParams = argonParams{memoryKiB: 19 * 1024, iterations: 2, parallelism: 1, saltLen: 16, keyLen: 32}

// Upper bounds accepted when parsing a stored hash, so a corrupted or
// tampered row can't make a login attempt allocate unbounded memory or CPU.
const (
	maxMemoryKiB   = 1024 * 1024 // 1 GiB
	maxIterations  = 16
	maxParallelism = 16
)

// normalizePassword applies NFKC so the same password typed on different
// keyboards (precomposed "ñ" vs "n" + combining tilde) hashes identically.
func normalizePassword(password string) string {
	return norm.NFKC.String(password)
}

// HashPassword returns an argon2id hash in PHC string format:
// $argon2id$v=19$m=<KiB>,t=<iterations>,p=<parallelism>$<salt>$<key>
func HashPassword(password string) string {
	return hashPasswordWith(defaultParams, password)
}

func hashPasswordWith(p argonParams, password string) string {
	salt := make([]byte, p.saltLen)
	rand.Read(salt) // never fails: since Go 1.24 it crashes the program rather than return weak randomness
	key := argon2.IDKey([]byte(normalizePassword(password)), salt, p.iterations, p.memoryKiB, p.parallelism, p.keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.memoryKiB, p.iterations, p.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

// VerifyPassword reports whether password matches the encoded hash, and
// whether the hash should be recomputed because its parameters are outdated.
// needsRehash is only true when the password matched.
func VerifyPassword(encoded, password string) (ok, needsRehash bool, err error) {
	p, salt, key, err := parseHash(encoded)
	if err != nil {
		return false, false, err
	}
	candidate := argon2.IDKey([]byte(normalizePassword(password)), salt, p.iterations, p.memoryKiB, p.parallelism, p.keyLen)
	if subtle.ConstantTimeCompare(candidate, key) != 1 {
		return false, false, nil
	}
	return true, p != defaultParams, nil
}

func parseHash(encoded string) (p argonParams, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return p, nil, nil, ErrMalformedHash
	}

	// Parameters are exactly "m=<n>,t=<n>,p=<n>", in that order.
	fields := strings.Split(parts[3], ",")
	if len(fields) != 3 {
		return p, nil, nil, ErrMalformedHash
	}
	m, okM := parseHashParam(fields[0], "m")
	t, okT := parseHashParam(fields[1], "t")
	par, okP := parseHashParam(fields[2], "p")
	if !okM || !okT || !okP {
		return p, nil, nil, ErrMalformedHash
	}
	if par < 1 || par > maxParallelism || t < 1 || t > maxIterations || m < 8*par || m > maxMemoryKiB {
		return p, nil, nil, ErrMalformedHash
	}

	salt, err = base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return p, nil, nil, ErrMalformedHash
	}
	key, err = base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return p, nil, nil, ErrMalformedHash
	}

	p = argonParams{
		memoryKiB:   uint32(m),
		iterations:  uint32(t),
		parallelism: uint8(par),
		saltLen:     uint32(len(salt)),
		keyLen:      uint32(len(key)),
	}
	return p, salt, key, nil
}

// parseHashParam parses one "<name>=<unsigned integer>" field of a PHC hash.
func parseHashParam(field, name string) (uint64, bool) {
	value, found := strings.CutPrefix(field, name+"=")
	if !found {
		return 0, false
	}
	n, err := strconv.ParseUint(value, 10, 32)
	return n, err == nil
}

// ValidatePassword checks a new password against the policy. email must
// already be normalized (see NormalizeEmail).
func ValidatePassword(password, email string) error {
	normalized := normalizePassword(password)
	switch n := utf8.RuneCountInString(normalized); {
	case n < PasswordMinLength:
		return ErrPasswordTooShort
	case n > PasswordMaxLength:
		return ErrPasswordTooLong
	}

	lower := strings.ToLower(normalized)
	if _, found := commonPasswords[lower]; found {
		return ErrPasswordTooCommon
	}
	if lower == strings.ToLower(email) {
		return ErrPasswordSameAsEmail
	}
	return nil
}

//go:embed data/common_passwords.txt
var commonPasswordsFile string

// commonPasswords holds lowercase entries; lines starting with # are comments.
var commonPasswords = func() map[string]struct{} {
	set := make(map[string]struct{}, 10000)
	sc := bufio.NewScanner(strings.NewReader(commonPasswordsFile))
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		set[line] = struct{}{}
	}
	return set
}()
