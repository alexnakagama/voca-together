package profile

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Limits in characters (code points) after normalization. The database
// enforces the same numbers with char_length (migration 00005).
const (
	DisplayNameMaxLength = 50
	BioMaxLength         = 500
)

// maxCombiningMarks is how many combining marks may follow one another. Real
// writing stacks a few; more only makes text overflow its line.
const maxCombiningMarks = 8

const (
	zeroWidthNonJoiner = 0x200C // needed by Persian and Indic scripts
	zeroWidthJoiner    = 0x200D // needed by Indic scripts and emoji sequences
)

// invisible lists characters that draw nothing although Unicode classes them
// as letters, symbols or marks, so unicode.IsGraphic lets them through. They
// are refused everywhere: otherwise a name could be blank, or two names could
// look the same and differ.
var invisible = map[rune]bool{
	0x034F: true, // combining grapheme joiner
	0x115F: true, // hangul choseong filler
	0x1160: true, // hangul jungseong filler
	0x17B4: true, // khmer vowel inherent aq
	0x17B5: true, // khmer vowel inherent aa
	0x2800: true, // braille pattern blank
	0x3164: true, // hangul filler
	0xFFA0: true, // halfwidth hangul filler
}

// normalize returns in as it will be stored, or a *ValidationError naming
// every invalid field.
func normalize(in Input) (Input, error) {
	var fields []*FieldError
	name, err := normalizeDisplayName(in.DisplayName)
	if err != nil {
		fields = append(fields, err)
	}
	bio, err := normalizeBio(in.Bio)
	if err != nil {
		fields = append(fields, err)
	}
	if len(fields) > 0 {
		return Input{}, &ValidationError{Fields: fields}
	}
	return Input{DisplayName: name, Bio: bio}, nil
}

// The whitespace rules, shared by both fields:
//
//   - a space is a tab or any Unicode space separator (no-break, em,
//     ideographic and so on);
//   - a line break is LF, CR or CRLF;
//   - every other control or separator character (vertical tab, form feed,
//     NEL, the line and paragraph separators, NUL and the rest) is not
//     whitespace here: it is never dropped or converted, and the text that
//     contains it is refused as invalid.
func isSpace(r rune) bool {
	return r == '\t' || unicode.Is(unicode.Zs, r)
}

func isSpaceOrLineBreak(r rune) bool {
	return isSpace(r) || r == '\n' || r == '\r'
}

// normalizeDisplayName returns the name as stored: NFC, without leading or
// trailing spaces, and with every run of spaces and line breaks as one
// space, so a name pasted over two lines is kept as one line. NFC rather
// than NFKC, so the name keeps the characters its owner chose. It must then
// be 1 to DisplayNameMaxLength characters of printable text in any script,
// with at least one letter or digit.
func normalizeDisplayName(s string) (string, *FieldError) {
	if !utf8.ValidString(s) {
		return "", ErrDisplayNameInvalid
	}
	s = strings.Join(strings.FieldsFunc(norm.NFC.String(s), isSpaceOrLineBreak), " ")
	if s == "" {
		return "", ErrDisplayNameRequired
	}
	if utf8.RuneCountInString(s) > DisplayNameMaxLength {
		return "", ErrDisplayNameTooLong
	}
	if !printable(s, false) || !strings.ContainsFunc(s, isLetterOrDigit) {
		return "", ErrDisplayNameInvalid
	}
	return s, nil
}

// normalizeBio returns the bio as stored: NFC, line breaks as "\n", tabs as
// spaces, no spaces at the end of a line (so a line of only spaces is a blank
// line), at most one blank line in a row, and no leading or trailing spaces
// or line breaks. Spaces inside and at the start of a line are kept. It may
// be empty, and is otherwise at most BioMaxLength characters of printable
// text and line breaks.
func normalizeBio(s string) (string, *FieldError) {
	if !utf8.ValidString(s) {
		return "", ErrBioInvalid
	}
	s = norm.NFC.String(s)
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", " ").Replace(s)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRightFunc(line, isSpace)
	}
	s = strings.Join(lines, "\n")
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	s = strings.TrimFunc(s, isSpaceOrLineBreak)
	if utf8.RuneCountInString(s) > BioMaxLength {
		return "", ErrBioTooLong
	}
	if !printable(s, true) {
		return "", ErrBioInvalid
	}
	return s, nil
}

// printable reports whether s holds only characters that draw something
// (letters, marks, numbers, punctuation, symbols, spaces), the two joiners,
// and line feeds if lineBreaks is set, with no long run of combining marks.
//
// That excludes what could disguise or break the text where it is shown:
// control characters, bidirectional overrides and isolates, other invisible
// format characters, the characters in invisible, private-use and unassigned
// code points, and U+FFFD, which is what a JSON decoder leaves in place of
// invalid UTF-8.
//
// A joiner counts as a combining mark: it draws nothing either, so it must
// neither restart the count nor pad the text without limit.
func printable(s string, lineBreaks bool) bool {
	marks := 0
	for _, r := range s {
		joiner := r == zeroWidthJoiner || r == zeroWidthNonJoiner
		switch {
		case joiner:
		case r == '\n' && lineBreaks:
		case r == unicode.ReplacementChar, invisible[r], !unicode.IsGraphic(r):
			return false
		}
		if joiner || unicode.IsMark(r) {
			if marks++; marks > maxCombiningMarks {
				return false
			}
		} else {
			marks = 0
		}
	}
	return true
}

func isLetterOrDigit(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}
