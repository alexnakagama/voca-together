package auth

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestHashPasswordUsesArgon2idPHCFormat(t *testing.T) {
	const pw = "correct horse battery staple"
	encoded := HashPassword(pw)

	wantPrefix := "$argon2id$v=19$m=19456,t=2,p=1$"
	if !strings.HasPrefix(encoded, wantPrefix) {
		t.Fatalf("hash %q does not start with %q", encoded, wantPrefix)
	}
	if strings.Contains(encoded, pw) {
		t.Fatal("hash contains the plaintext password")
	}

	parts := strings.Split(encoded, "$") // "", alg, version, params, salt, key
	if len(parts) != 6 {
		t.Fatalf("hash has %d $-separated parts, want 6", len(parts))
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		t.Errorf("salt: %d bytes, err %v; want 16 bytes", len(salt), err)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) != 32 {
		t.Errorf("key: %d bytes, err %v; want 32 bytes", len(key), err)
	}
}

func TestHashPasswordUsesRandomSalt(t *testing.T) {
	a := HashPassword("correct horse battery staple")
	b := HashPassword("correct horse battery staple")
	if a == b {
		t.Fatal("hashing the same password twice produced identical hashes; salt is not random")
	}
}

func TestVerifyPassword(t *testing.T) {
	encoded := HashPassword("correct horse battery staple")

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"correct password", "correct horse battery staple", true},
		{"wrong password", "correct horse battery stapler", false},
		{"different case", "Correct horse battery staple", false},
		{"surrounding whitespace is significant", " correct horse battery staple", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, _, err := VerifyPassword(encoded, tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tt.want {
				t.Errorf("VerifyPassword = %v, want %v", ok, tt.want)
			}
		})
	}
}

// Keyboards differ in how they encode accented characters ("ñ" as one code
// point vs "n" + combining tilde). Both must log in to the same account.
func TestVerifyPasswordNormalizesUnicode(t *testing.T) {
	composed := "contraseña segura"    // ñ as U+00F1
	decomposed := "contraseña segura" // n + U+0303 COMBINING TILDE
	encoded := HashPassword(composed)
	ok, _, err := VerifyPassword(encoded, decomposed)
	if err != nil || !ok {
		t.Fatalf("decomposed form did not verify: ok=%v err=%v", ok, err)
	}
}

func TestVerifyPasswordReportsOutdatedParams(t *testing.T) {
	const pw = "correct horse battery staple"

	current := HashPassword(pw)
	if _, rehash, _ := VerifyPassword(current, pw); rehash {
		t.Error("hash with current params reported as needing rehash")
	}

	weak := argonParams{memoryKiB: 8 * 1024, iterations: 1, parallelism: 1, saltLen: 16, keyLen: 32}
	old := hashPasswordWith(weak, pw)
	ok, rehash, err := VerifyPassword(old, pw)
	if err != nil || !ok {
		t.Fatalf("old-params hash did not verify: ok=%v err=%v", ok, err)
	}
	if !rehash {
		t.Error("hash with outdated params not reported as needing rehash")
	}
	if ok, rehash, _ := VerifyPassword(old, "wrong password!!"); ok || rehash {
		t.Errorf("wrong password: ok=%v rehash=%v, want both false", ok, rehash)
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	const salt = "c29tZXNhbHRzb21lc2FsdA" // 16 bytes
	const key = "a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2U"
	tests := map[string]string{
		"empty":                  "",
		"plaintext":              "correct horse battery staple",
		"bcrypt":                 "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy",
		"argon2i":                "$argon2i$v=19$m=19456,t=2,p=1$" + salt + "$" + key,
		"old version":            "$argon2id$v=16$m=19456,t=2,p=1$" + salt + "$" + key,
		"missing part":           "$argon2id$v=19$m=19456,t=2,p=1$" + salt,
		"non-numeric params":     "$argon2id$v=19$m=abc,t=2,p=1$" + salt + "$" + key,
		"zero iterations":        "$argon2id$v=19$m=19456,t=0,p=1$" + salt + "$" + key,
		"absurd memory":          "$argon2id$v=19$m=99999999,t=2,p=1$" + salt + "$" + key,
		"bad salt base64":        "$argon2id$v=19$m=19456,t=2,p=1$!!!$" + key,
		"short key":              "$argon2id$v=19$m=19456,t=2,p=1$" + salt + "$a2V5",
		"missing param":          "$argon2id$v=19$m=19456,t=2$" + salt + "$" + key,
		"params out of order":    "$argon2id$v=19$t=2,m=19456,p=1$" + salt + "$" + key,
		"extra param":            "$argon2id$v=19$m=19456,t=2,p=1,x=1$" + salt + "$" + key,
		"param with doubled '='": "$argon2id$v=19$m==19456,t=2,p=1$" + salt + "$" + key,
	}
	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			ok, rehash, err := VerifyPassword(encoded, "correct horse battery staple")
			if !errors.Is(err, ErrMalformedHash) {
				t.Errorf("err = %v, want ErrMalformedHash", err)
			}
			if ok || rehash {
				t.Errorf("ok=%v rehash=%v, want both false", ok, rehash)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	const email = "ana@example.com"
	tests := []struct {
		name     string
		password string
		want     error
	}{
		{"10 characters is enough", "mango-kiwi", nil},
		{"9 characters is too short", "mango-kiw", ErrPasswordTooShort},
		{"empty", "", ErrPasswordTooShort},
		{"128 characters is allowed", strings.Repeat("x", 127) + "y", nil},
		{"129 characters is too long", strings.Repeat("x", 128) + "y", ErrPasswordTooLong},
		{"length counts characters, not bytes", strings.Repeat("ñ", 10), nil},
		{"multi-byte but too short", strings.Repeat("漢", 9), ErrPasswordTooShort},
		{"combining marks count as one character", strings.Repeat("ñ", 9), ErrPasswordTooShort},
		{"no composition rules: lowercase letters only", "lowercaseonlyphrase", nil},
		{"spaces allowed", "correct horse battery staple", nil},
		{"common password", "password123", ErrPasswordTooCommon},
		{"common password, different case", "PassWord123", ErrPasswordTooCommon},
		{"common password from keyboard walk", "qwertyuiop", ErrPasswordTooCommon},
		{"equal to email", "ana@example.com", ErrPasswordSameAsEmail},
		{"equal to email, different case", "Ana@Example.COM", ErrPasswordSameAsEmail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password, email)
			if !errors.Is(err, tt.want) {
				t.Errorf("ValidatePassword(%q) = %v, want %v", tt.password, err, tt.want)
			}
		})
	}
}

func TestPasswordErrorsAreFieldErrors(t *testing.T) {
	for _, err := range []error{ErrPasswordTooShort, ErrPasswordTooLong, ErrPasswordTooCommon, ErrPasswordSameAsEmail} {
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != "password" || fe.Code == "" {
			t.Errorf("%v is not a password FieldError with a code", err)
		}
	}
	codes := map[error]string{
		ErrPasswordTooShort:    "too_short",
		ErrPasswordTooLong:     "too_long",
		ErrPasswordTooCommon:   "too_common",
		ErrPasswordSameAsEmail: "same_as_email",
	}
	for err, want := range codes {
		if got := err.(*FieldError).Code; got != want {
			t.Errorf("code = %q, want %q", got, want)
		}
	}
}

func TestCommonPasswordListLoaded(t *testing.T) {
	if n := len(commonPasswords); n < 5000 {
		t.Fatalf("common password list has %d entries, expected thousands", n)
	}
	for pw := range commonPasswords {
		if strings.HasPrefix(pw, "#") || pw == "" {
			t.Fatalf("list contains comment or blank entry %q", pw)
		}
	}
}
