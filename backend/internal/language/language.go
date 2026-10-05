// Package language owns a member's languages: the ones they speak and the
// ones they are learning, each with a level (decision 029). It knows nothing
// about HTTP or sessions, and its types and rules know nothing about the
// database: what is stored is converted explicitly (levelFromRank).
package language

import "slices"

// Kind says what a language is to a member. Its value is the one stored and
// the name validation errors give the list.
type Kind string

const (
	// KindSpoken: the member knows the language and can offer it.
	KindSpoken Kind = "spoken"
	// KindLearning: the member wants to practise the language.
	KindLearning Kind = "learning"
)

// MaxPerKind is how many languages a member may have of each kind. The
// database holds positions 0 to MaxPerKind-1 (migration 00006).
const MaxPerKind = 5

// Code identifies a language: the BCP 47 primary language subtag in
// lowercase, two or three letters ("es", "yue"). A Code is well formed; only
// the catalog says whether it names a language (Selection.checkKnown).
type Code string

// parseCode returns s as a Code if it is exactly two or three ASCII lowercase
// letters. A code is an identifier, not text: it is never trimmed or
// lowercased, so anything else is refused.
func parseCode(s string) (Code, bool) {
	if len(s) < 2 || len(s) > 3 {
		return "", false
	}
	for i := range len(s) {
		if s[i] < 'a' || s[i] > 'z' {
			return "", false
		}
	}
	return Code(s), true
}

// Entry is one language of a member and how well they know it.
type Entry struct {
	Code  Code
	Level Level
}

// Selection is a member's languages. Each list is in the member's own order,
// the first being the main one: an entry's position is its index. Either
// list may be empty, and a language is in at most one of them.
type Selection struct {
	Spoken   []Entry
	Learning []Entry
}

// Equal reports whether s and o hold the same languages at the same levels
// in the same order. A nil list and an empty one are the same.
func (s Selection) Equal(o Selection) bool {
	return slices.Equal(s.Spoken, o.Spoken) && slices.Equal(s.Learning, o.Learning)
}

// InputEntry is one language as a member submits it, not yet validated.
type InputEntry struct {
	Language string
	Level    string
}

// Input is everything a member can set about their languages.
type Input struct {
	Spoken   []InputEntry
	Learning []InputEntry
}
