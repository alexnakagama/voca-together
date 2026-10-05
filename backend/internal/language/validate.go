package language

import "slices"

// parse returns in as a Selection, or a *ValidationError naming every rule
// each list breaks, once per list, spoken first:
//
//   - too_many: more than MaxPerKind languages (the rest is still checked);
//   - unknown_language: a code that is not well formed (parseCode);
//   - invalid_level: a level that is not one of the seven names, or native
//     for a language being learned;
//   - duplicate: a language twice in the list. A language in both lists is
//     reported on learning.
//
// Nothing is required: either list, or both, may be empty. The rules can't be
// checked one entry at a time, since two of them depend on the other entries,
// so there is no way to validate an entry alone.
//
// parse doesn't know the catalog: whether a well-formed code names a language
// is checkKnown's question, asked only of a Selection that parsed.
func parse(in Input) (Selection, error) {
	seen := make(map[Code]bool)
	spoken, fields := parseKind(KindSpoken, in.Spoken, seen)
	learning, learningFields := parseKind(KindLearning, in.Learning, seen)
	if fields = append(fields, learningFields...); len(fields) > 0 {
		return Selection{}, &ValidationError{Fields: fields}
	}
	return Selection{Spoken: spoken, Learning: learning}, nil
}

// parseKind parses the list of one kind. seen holds the well-formed codes of
// the entries before it, in this list and in the lists parsed earlier, and
// gains this list's.
func parseKind(kind Kind, in []InputEntry, seen map[Code]bool) ([]Entry, []*FieldError) {
	var unknown, invalidLevel, duplicate bool
	entries := make([]Entry, 0, len(in))
	for _, e := range in {
		level, ok := parseLevel(e.Level)
		if !ok || (level == LevelNative && kind != KindSpoken) {
			invalidLevel = true
		}
		code, ok := parseCode(e.Language)
		if !ok {
			unknown = true
			continue
		}
		if seen[code] {
			duplicate = true
		}
		seen[code] = true
		entries = append(entries, Entry{Code: code, Level: level})
	}

	var fields []*FieldError
	for _, c := range []struct {
		broken bool
		code   string
	}{
		{len(in) > MaxPerKind, codeTooMany},
		{unknown, codeUnknownLanguage},
		{invalidLevel, codeInvalidLevel},
		{duplicate, codeDuplicate},
	} {
		if c.broken {
			fields = append(fields, &FieldError{Field: string(kind), Code: c.code})
		}
	}
	if len(fields) > 0 {
		return nil, fields
	}
	return entries, nil
}

// checkKnown returns a *ValidationError with unknown_language for each list
// that holds a language known doesn't report as being in the catalog, and nil
// if there is none. It is the second half of validation, and takes the
// catalog as a function so that this package's rules don't depend on where
// the catalog is kept.
func (s Selection) checkKnown(known func(Code) bool) error {
	var fields []*FieldError
	for _, list := range []struct {
		kind    Kind
		entries []Entry
	}{
		{KindSpoken, s.Spoken},
		{KindLearning, s.Learning},
	} {
		if slices.ContainsFunc(list.entries, func(e Entry) bool { return !known(e.Code) }) {
			fields = append(fields, &FieldError{Field: string(list.kind), Code: codeUnknownLanguage})
		}
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}
