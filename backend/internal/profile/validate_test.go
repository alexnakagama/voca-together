package profile

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeDisplayName(t *testing.T) {
	tests := []struct {
		name, in, want string
		wantErr        *FieldError
	}{
		{name: "plain", in: "Ana", want: "Ana"},
		{name: "trimmed", in: "  Ana \t", want: "Ana"},
		{name: "inner whitespace collapsed", in: "Ana  María\u00a0\n López", want: "Ana María López"},
		{name: "composed to NFC", in: "Jose\u0301", want: "José"},
		{name: "compatibility characters kept (not NFKC)", in: "ｱﾅ", want: "ｱﾅ"},
		{name: "japanese", in: "田中 太郎", want: "田中 太郎"},
		{name: "arabic", in: "محمد", want: "محمد"},
		{name: "devanagari with a joiner", in: "क\u094d\u200dष", want: "क\u094d\u200dष"},
		{name: "persian with a non-joiner", in: "می\u200cخواهم", want: "می\u200cخواهم"},
		{name: "thai", in: "สมชาย", want: "สมชาย"},
		{name: "emoji beside a letter", in: "Ana 👩\u200d💻", want: "Ana 👩\u200d💻"},
		{name: "digits count as a name", in: "42", want: "42"},
		{name: "punctuation inside", in: "O'Neil-Smith Jr.", want: "O'Neil-Smith Jr."},
		{name: "one character", in: "A", want: "A"},
		{name: "fifty characters", in: strings.Repeat("a", 50), want: strings.Repeat("a", 50)},
		{name: "fifty astral characters", in: strings.Repeat("𠀀", 50), want: strings.Repeat("𠀀", 50)},
		{name: "fifty after normalization", in: strings.Repeat("e\u0301", 50), want: strings.Repeat("é", 50)},
		{name: "fifty after trimming", in: " " + strings.Repeat("a", 50) + " ", want: strings.Repeat("a", 50)},

		{name: "empty", in: "", wantErr: ErrDisplayNameRequired},
		{name: "only whitespace", in: " \t\n\u00a0\u3000", wantErr: ErrDisplayNameRequired},
		{name: "fifty-one characters", in: strings.Repeat("a", 51), wantErr: ErrDisplayNameTooLong},
		{name: "fifty-one multi-byte characters", in: strings.Repeat("あ", 51), wantErr: ErrDisplayNameTooLong},
		{name: "control character", in: "An\u0000a", wantErr: ErrDisplayNameInvalid},
		{name: "escape", in: "An\u001ba", wantErr: ErrDisplayNameInvalid},
		{name: "bidi override", in: "Ana\u202egro", wantErr: ErrDisplayNameInvalid},
		{name: "bidi isolate", in: "Ana\u2066x", wantErr: ErrDisplayNameInvalid},
		{name: "right-to-left mark", in: "Ana\u200f", wantErr: ErrDisplayNameInvalid},
		{name: "zero-width space", in: "An\u200ba", wantErr: ErrDisplayNameInvalid},
		{name: "byte order mark", in: "\ufeffAna", wantErr: ErrDisplayNameInvalid},
		{name: "private use", in: "Ana\ue000", wantErr: ErrDisplayNameInvalid},
		{name: "replacement character", in: "An\ufffda", wantErr: ErrDisplayNameInvalid},
		{name: "invalid UTF-8", in: "An\xffa", wantErr: ErrDisplayNameInvalid},
		{name: "no letter or digit", in: "...", wantErr: ErrDisplayNameInvalid},
		{name: "only emoji", in: "🙂", wantErr: ErrDisplayNameInvalid},
		{name: "only joiners", in: "\u200d\u200c", wantErr: ErrDisplayNameInvalid},
		{name: "stacked combining marks", in: "a" + strings.Repeat("\u0335", 9), wantErr: ErrDisplayNameInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeDisplayName(tt.in)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// Eight marks on one base are accepted (some scripts stack several); the
// limit only stops text that overflows its line.
func TestNormalizeDisplayNameAllowsEightCombiningMarks(t *testing.T) {
	in := "a" + strings.Repeat("\u0335", 8) // a mark that doesn't compose
	if _, err := normalizeDisplayName(in); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestNormalizeBio(t *testing.T) {
	tests := []struct {
		name, in, want string
		wantErr        *FieldError
	}{
		{name: "empty", in: "", want: ""},
		{name: "only whitespace is empty", in: " \n\t ", want: ""},
		{name: "plain", in: "Learning Japanese.", want: "Learning Japanese."},
		{name: "trimmed", in: "\n Hi \n", want: "Hi"},
		{name: "line breaks kept", in: "One\nTwo\n\nThree", want: "One\nTwo\n\nThree"},
		{name: "windows and old mac line breaks", in: "One\r\nTwo\rThree", want: "One\nTwo\nThree"},
		{name: "blank lines collapsed", in: "One\n\n\n\n\nTwo", want: "One\n\nTwo"},
		{name: "tab becomes a space", in: "One\tTwo", want: "One Two"},
		{name: "inner spaces kept", in: "One  Two", want: "One  Two"},
		{name: "composed to NFC", in: "cafe\u0301", want: "café"},
		{name: "no letter needed", in: "🙂", want: "🙂"},
		{name: "emoji sequence", in: "👩\u200d💻 dev", want: "👩\u200d💻 dev"},
		{name: "five hundred characters", in: strings.Repeat("あ", 500), want: strings.Repeat("あ", 500)},
		{name: "five hundred astral characters", in: strings.Repeat("𠀀", 500), want: strings.Repeat("𠀀", 500)},

		{name: "five hundred and one characters", in: strings.Repeat("a", 501), wantErr: ErrBioTooLong},
		{name: "control character", in: "Hi\u0007", wantErr: ErrBioInvalid},
		{name: "bidi override", in: "Hi \u202eolleh", wantErr: ErrBioInvalid},
		{name: "line separator", in: "Hi\u2028there", wantErr: ErrBioInvalid},
		{name: "replacement character", in: "Hi\ufffd", wantErr: ErrBioInvalid},
		{name: "invalid UTF-8", in: "Hi\xff", wantErr: ErrBioInvalid},
		{name: "stacked combining marks", in: "a" + strings.Repeat("\u0335", 9), wantErr: ErrBioInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeBio(tt.in)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// Normalizing twice changes nothing, so a client that sends back what the
// server returned stores the same profile.
func TestNormalizationIsIdempotent(t *testing.T) {
	name, err := normalizeDisplayName("  Jose\u0301   Luis ")
	if err != nil {
		t.Fatal(err)
	}
	if again, err := normalizeDisplayName(name); err != nil || again != name {
		t.Errorf("display name: %q, %v; want %q", again, err, name)
	}
	bio, err := normalizeBio("One\r\n\r\n\r\n\tTwo ")
	if err != nil {
		t.Fatal(err)
	}
	if again, err := normalizeBio(bio); err != nil || again != bio {
		t.Errorf("bio: %q, %v; want %q", again, err, bio)
	}
}

func TestNormalizeReportsEveryInvalidField(t *testing.T) {
	_, err := normalize(Input{DisplayName: " ", Bio: strings.Repeat("a", 501)})
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("err = %v, want a *ValidationError", err)
	}
	if len(verr.Fields) != 2 || verr.Fields[0] != ErrDisplayNameRequired || verr.Fields[1] != ErrBioTooLong {
		t.Errorf("fields = %v", verr.Fields)
	}
}

func TestNormalizeReturnsBothNormalizedFields(t *testing.T) {
	got, err := normalize(Input{DisplayName: " Ana ", Bio: " Hi\r\n"})
	if err != nil {
		t.Fatal(err)
	}
	if got != (Input{DisplayName: "Ana", Bio: "Hi"}) {
		t.Errorf("got %+v", got)
	}
}

// Errors name fields and codes only: profile text never reaches a log
// through an error.
func TestValidationErrorsNeverContainTheInput(t *testing.T) {
	const marker = "SECRETNAME"
	_, err := normalize(Input{DisplayName: marker + "\u202e", Bio: marker + "\u0000"})
	if err == nil {
		t.Fatal("accepted")
	}
	if strings.Contains(err.Error(), marker) {
		t.Errorf("error contains the input: %v", err)
	}
	if got, want := err.Error(), "profile: invalid input: display_name: invalid, bio: invalid"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

// r is the one-character string for code point cp, so tests can name
// characters that are invisible in source.
func r(cp rune) string { return string(cp) }

// Characters that draw nothing although Unicode files them under letters,
// symbols or marks: none may appear, so a name can't be blank or padded.
var invisibleCharacters = map[string]rune{
	"combining grapheme joiner": 0x034F,
	"hangul choseong filler":    0x115F,
	"hangul jungseong filler":   0x1160,
	"khmer vowel inherent aq":   0x17B4,
	"khmer vowel inherent aa":   0x17B5,
	"braille pattern blank":     0x2800,
	"hangul filler":             0x3164,
	"halfwidth hangul filler":   0xFFA0,
}

func TestInvisibleCharactersAreRejected(t *testing.T) {
	for name, cp := range invisibleCharacters {
		t.Run(name, func(t *testing.T) {
			for _, in := range []string{r(cp), r(cp) + r(cp), "Ana" + r(cp), r(cp) + "Ana", "A" + r(cp) + "na"} {
				if _, err := normalizeDisplayName(in); err != ErrDisplayNameInvalid {
					t.Errorf("display name %q: err = %v, want invalid", in, err)
				}
			}
			for _, in := range []string{r(cp), "Hi" + r(cp), "Hi\n" + r(cp) + "\nthere"} {
				if _, err := normalizeBio(in); err != ErrBioInvalid {
					t.Errorf("bio %q: err = %v, want invalid", in, err)
				}
			}
		})
	}
}

// What each whitespace or control character does in a display name: spaces
// of any width, tabs and line breaks separate words and become one space;
// every other control character is refused, never silently dropped.
func TestDisplayNameWhitespaceAndControlCharacters(t *testing.T) {
	separators := map[string]string{
		"space":             " ",
		"tab":               "\t",
		"line feed":         "\n",
		"carriage return":   "\r",
		"CRLF":              "\r\n",
		"no-break space":    r(0x00A0),
		"em space":          r(0x2003),
		"ideographic space": r(0x3000),
		"mixed run":         " \t\r\n" + r(0x00A0),
	}
	for name, sep := range separators {
		t.Run(name+" becomes one space", func(t *testing.T) {
			got, err := normalizeDisplayName(sep + "Ana" + sep + sep + "López" + sep)
			if err != nil || got != "Ana López" {
				t.Errorf("got %q, %v; want %q", got, err, "Ana López")
			}
		})
	}

	refused := map[string]rune{
		"vertical tab":        0x000B,
		"form feed":           0x000C,
		"next line":           0x0085,
		"line separator":      0x2028,
		"paragraph separator": 0x2029,
		"null":                0x0000,
		"delete":              0x007F,
	}
	for name, cp := range refused {
		t.Run(name+" is refused", func(t *testing.T) {
			for _, in := range []string{"Ana" + r(cp) + "López", r(cp) + "Ana", "Ana" + r(cp)} {
				if _, err := normalizeDisplayName(in); err != ErrDisplayNameInvalid {
					t.Errorf("%q: err = %v, want invalid", in, err)
				}
			}
		})
	}
}

// The bio treats the same characters the same way, except that line breaks
// stay line breaks.
func TestBioWhitespaceAndControlCharacters(t *testing.T) {
	for name, cp := range map[string]rune{
		"vertical tab": 0x000B, "form feed": 0x000C, "next line": 0x0085,
		"line separator": 0x2028, "paragraph separator": 0x2029, "null": 0x0000,
	} {
		t.Run(name+" is refused", func(t *testing.T) {
			// At the ends too: trimming must not drop it silently.
			for _, in := range []string{"Hi" + r(cp) + "there", r(cp) + "Hi", "Hi" + r(cp)} {
				if _, err := normalizeBio(in); err != ErrBioInvalid {
					t.Errorf("%q: err = %v, want invalid", in, err)
				}
			}
		})
	}

	tests := []struct{ name, in, want string }{
		{"lines of only spaces are blank lines", "One\n \n\t\n" + r(0x3000) + "\nTwo", "One\n\nTwo"},
		{"trailing spaces on a line are dropped", "One  \nTwo\t\nThree" + r(0x00A0), "One\nTwo\nThree"},
		{"leading spaces on a line are kept", "List:\n  - one\n  - two", "List:\n  - one\n  - two"},
		{"leading and trailing blank lines are dropped", " \n\n  One\n \n", "One"},
		{"only spaces and line breaks is empty", " \n \r\n\t\n" + r(0x00A0), ""},
		{"a no-break space inside a line is kept", "10" + r(0x00A0) + "km", "10" + r(0x00A0) + "km"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeBio(tt.in)
			if err != nil || got != tt.want {
				t.Errorf("got %q, %v; want %q", got, err, tt.want)
			}
			if again, err := normalizeBio(got); err != nil || again != got {
				t.Errorf("not idempotent: %q, %v", again, err)
			}
		})
	}
}

// Joiners don't restart the count of combining marks, so they can't be used
// to stack marks without limit or to pad a name invisibly.
func TestCombiningMarkLimitCountsJoiners(t *testing.T) {
	mark, zwj, zwnj := r(0x0335), r(0x200D), r(0x200C)
	eight := strings.Repeat(mark, 8)
	for name, in := range map[string]string{
		"marks, joiner, marks":     "a" + eight + zwj + eight,
		"marks, non-joiner, marks": "a" + eight + zwnj + eight,
		"marks then a joiner":      "a" + eight + zwj,
		"only joiners":             "a" + strings.Repeat(zwj, 9),
		"alternating":              "a" + strings.Repeat(mark+zwj, 5),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeDisplayName(in); err != ErrDisplayNameInvalid {
				t.Errorf("display name: err = %v, want invalid", err)
			}
			if _, err := normalizeBio(in); err != ErrBioInvalid {
				t.Errorf("bio: err = %v, want invalid", err)
			}
		})
	}

	// Real sequences stay well inside the limit.
	for name, in := range map[string]string{
		"family emoji":        "Ana " + r(0x1F468) + zwj + r(0x1F469) + zwj + r(0x1F467) + zwj + r(0x1F466),
		"heart on fire":       "Ana " + r(0x2764) + r(0xFE0F) + zwj + r(0x1F525),
		"devanagari conjunct": r(0x0915) + r(0x094D) + zwj + r(0x0937),
		"seven marks, joiner": "a" + strings.Repeat(mark, 7) + zwj + "b",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeDisplayName(in); err != nil {
				t.Errorf("err = %v", err)
			}
		})
	}
}
