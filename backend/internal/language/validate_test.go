package language

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// fieldsOf returns the field errors of err, which must be nil or a
// *ValidationError with at least one field.
func fieldsOf(t *testing.T, err error) []FieldError {
	t.Helper()
	if err == nil {
		return nil
	}
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("err = %v (%T), want a *ValidationError", err, err)
	}
	if len(verr.Fields) == 0 {
		t.Fatal("a ValidationError without fields")
	}
	out := make([]FieldError, len(verr.Fields))
	for i, f := range verr.Fields {
		out[i] = *f
	}
	return out
}

func entries(pairs ...string) []InputEntry {
	out := make([]InputEntry, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, InputEntry{Language: pairs[i], Level: pairs[i+1]})
	}
	return out
}

func TestParseAcceptsValidSelections(t *testing.T) {
	tests := []struct {
		name string
		in   Input
		want Selection
	}{
		{
			name: "one of each",
			in:   Input{Spoken: entries("es", "native"), Learning: entries("ja", "a2")},
			want: Selection{Spoken: []Entry{{"es", LevelNative}}, Learning: []Entry{{"ja", LevelA2}}},
		},
		{
			name: "five of each, in the order given",
			in: Input{
				Spoken:   entries("es", "native", "en", "c2", "pt", "c1", "fr", "b2", "it", "b1"),
				Learning: entries("ja", "a1", "ko", "a2", "zh", "b1", "yue", "b2", "fil", "c2"),
			},
			want: Selection{
				Spoken:   []Entry{{"es", LevelNative}, {"en", LevelC2}, {"pt", LevelC1}, {"fr", LevelB2}, {"it", LevelB1}},
				Learning: []Entry{{"ja", LevelA1}, {"ko", LevelA2}, {"zh", LevelB1}, {"yue", LevelB2}, {"fil", LevelC2}},
			},
		},
		{
			name: "order is the member's, not alphabetical",
			in:   Input{Spoken: entries("zu", "b1", "af", "native")},
			want: Selection{Spoken: []Entry{{"zu", LevelB1}, {"af", LevelNative}}},
		},
		{
			name: "several native languages",
			in:   Input{Spoken: entries("es", "native", "ca", "native")},
			want: Selection{Spoken: []Entry{{"es", LevelNative}, {"ca", LevelNative}}},
		},
		{
			name: "spoken at a beginner's level",
			in:   Input{Spoken: entries("es", "a1")},
			want: Selection{Spoken: []Entry{{"es", LevelA1}}},
		},
		{
			name: "learning at the highest level short of native",
			in:   Input{Learning: entries("es", "c2")},
			want: Selection{Learning: []Entry{{"es", LevelC2}}},
		},
		{name: "nothing at all", in: Input{}, want: Selection{}},
		{
			name: "empty lists",
			in:   Input{Spoken: []InputEntry{}, Learning: []InputEntry{}},
			want: Selection{},
		},
		{
			name: "only spoken",
			in:   Input{Spoken: entries("es", "native")},
			want: Selection{Spoken: []Entry{{"es", LevelNative}}},
		},
		{
			name: "only learning",
			in:   Input{Learning: entries("ja", "a1")},
			want: Selection{Learning: []Entry{{"ja", LevelA1}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parse(tt.in)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseRejectsInvalidSelections(t *testing.T) {
	six := entries("es", "c1", "en", "c1", "pt", "c1", "fr", "c1", "it", "c1", "de", "c1")
	tests := []struct {
		name string
		in   Input
		want []FieldError
	}{
		{"six spoken", Input{Spoken: six}, []FieldError{{"spoken", "too_many"}}},
		{"six learning", Input{Learning: six}, []FieldError{{"learning", "too_many"}}},
		{"six of each", Input{
			Spoken:   six,
			Learning: entries("ja", "a1", "ko", "a1", "zh", "a1", "ru", "a1", "ar", "a1", "hi", "a1"),
		}, []FieldError{{"spoken", "too_many"}, {"learning", "too_many"}}},

		{"malformed code, spoken", Input{Spoken: entries("ES", "c1")}, []FieldError{{"spoken", "unknown_language"}}},
		{"malformed code, learning", Input{Learning: entries("spanish", "c1")}, []FieldError{{"learning", "unknown_language"}}},
		{"empty code", Input{Spoken: entries("", "c1")}, []FieldError{{"spoken", "unknown_language"}}},
		{"padded code", Input{Spoken: entries(" es", "c1")}, []FieldError{{"spoken", "unknown_language"}}},
		{"regional variant", Input{Learning: entries("pt-BR", "a1")}, []FieldError{{"learning", "unknown_language"}}},
		{"a malformed code twice is not a duplicate", Input{Spoken: entries("ES", "c1", "ES", "c1")},
			[]FieldError{{"spoken", "unknown_language"}}},
		{"the same malformed code in both lists", Input{Spoken: entries("ES", "c1"), Learning: entries("ES", "a1")},
			[]FieldError{{"spoken", "unknown_language"}, {"learning", "unknown_language"}}},

		{"unknown level, spoken", Input{Spoken: entries("es", "fluent")}, []FieldError{{"spoken", "invalid_level"}}},
		{"unknown level, learning", Input{Learning: entries("es", "c3")}, []FieldError{{"learning", "invalid_level"}}},
		{"upper-case level", Input{Spoken: entries("es", "A1")}, []FieldError{{"spoken", "invalid_level"}}},
		{"upper-case native", Input{Spoken: entries("es", "Native")}, []FieldError{{"spoken", "invalid_level"}}},
		{"empty level", Input{Learning: entries("es", "")}, []FieldError{{"learning", "invalid_level"}}},
		{"padded level", Input{Spoken: entries("es", "b1 ")}, []FieldError{{"spoken", "invalid_level"}}},
		{"a stored rank is not a level", Input{Spoken: entries("es", "7")}, []FieldError{{"spoken", "invalid_level"}}},
		{"native while learning", Input{Learning: entries("es", "native")}, []FieldError{{"learning", "invalid_level"}}},
		{"native while learning, beside a valid one", Input{Learning: entries("ja", "a1", "es", "native")},
			[]FieldError{{"learning", "invalid_level"}}},

		{"twice in spoken", Input{Spoken: entries("es", "native", "en", "c1", "es", "b1")},
			[]FieldError{{"spoken", "duplicate"}}},
		{"twice in learning", Input{Learning: entries("ja", "a1", "ja", "a1")},
			[]FieldError{{"learning", "duplicate"}}},
		{"three times in one list is one error", Input{Spoken: entries("es", "c1", "es", "c1", "es", "c1")},
			[]FieldError{{"spoken", "duplicate"}}},
		{"in both lists, reported on learning", Input{Spoken: entries("es", "native"), Learning: entries("es", "b2")},
			[]FieldError{{"learning", "duplicate"}}},
		{"in both lists, later in learning", Input{Spoken: entries("en", "c1", "es", "b1"), Learning: entries("ja", "a1", "es", "b2")},
			[]FieldError{{"learning", "duplicate"}}},
		{"twice in spoken and once in learning", Input{Spoken: entries("es", "c1", "es", "c1"), Learning: entries("es", "b2")},
			[]FieldError{{"spoken", "duplicate"}, {"learning", "duplicate"}}},
		{"a duplicate whose level is invalid", Input{Spoken: entries("es", "c1", "es", "nope")},
			[]FieldError{{"spoken", "invalid_level"}, {"spoken", "duplicate"}}},
		{"in both lists with an invalid level in spoken", Input{Spoken: entries("es", "nope"), Learning: entries("es", "b2")},
			[]FieldError{{"spoken", "invalid_level"}, {"learning", "duplicate"}}},

		{"items of a list that is too long are still checked", Input{
			Spoken: entries("es", "c1", "en", "c1", "pt", "c1", "fr", "c1", "it", "c1", "ES", "nope", "es", "c1"),
		}, []FieldError{{"spoken", "too_many"}, {"spoken", "unknown_language"}, {"spoken", "invalid_level"}, {"spoken", "duplicate"}}},

		{"everything at once, each error once and in order", Input{
			Spoken:   entries("es", "c1", "es", "c1", "E", "x", "EN", "y", "pt", "c1", "fr", "c1", "it", "c1"),
			Learning: entries("es", "native", "ja", "a1", "ja", "A1", "??", "a1", "ko", "a1", "zh", "a1", "ru", "a1"),
		}, []FieldError{
			{"spoken", "too_many"}, {"spoken", "unknown_language"}, {"spoken", "invalid_level"}, {"spoken", "duplicate"},
			{"learning", "too_many"}, {"learning", "unknown_language"}, {"learning", "invalid_level"}, {"learning", "duplicate"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parse(tt.in)
			if fields := fieldsOf(t, err); !slices.Equal(fields, tt.want) {
				t.Errorf("fields = %v, want %v", fields, tt.want)
			}
			if !got.Equal(Selection{}) {
				t.Errorf("an invalid input returned the selection %+v", got)
			}
		})
	}
}

// Nothing is ever required: a member may have no spoken languages, no
// learning ones, or none at all.
func TestParseNeverReportsAMissingList(t *testing.T) {
	for _, in := range []Input{
		{},
		{Spoken: []InputEntry{}},
		{Learning: []InputEntry{}},
		{Spoken: entries("es", "native")},
		{Learning: entries("ja", "a1")},
	} {
		if _, err := parse(in); err != nil {
			t.Errorf("parse(%+v): %v", in, err)
		}
	}
}

func TestParseAcceptsExactlyTheMaximum(t *testing.T) {
	codes := []string{"es", "en", "pt", "fr", "it", "de"}
	var in Input
	for _, c := range codes[:MaxPerKind] {
		in.Spoken = append(in.Spoken, InputEntry{Language: c, Level: "b1"})
	}
	if _, err := parse(in); err != nil {
		t.Fatalf("%d languages: %v", MaxPerKind, err)
	}
	in.Spoken = append(in.Spoken, InputEntry{Language: codes[MaxPerKind], Level: "b1"})
	if fields := fieldsOf(t, mustFail(t, in)); !slices.Equal(fields, []FieldError{{"spoken", "too_many"}}) {
		t.Errorf("%d languages: fields = %v", MaxPerKind+1, fields)
	}
}

func mustFail(t *testing.T, in Input) error {
	t.Helper()
	_, err := parse(in)
	if err == nil {
		t.Fatal("err = nil")
	}
	return err
}

// Sending back what was parsed parses to the same selection.
func TestParseIsIdempotent(t *testing.T) {
	in := Input{
		Spoken:   entries("es", "native", "en", "c1", "yue", "a1"),
		Learning: entries("ja", "a2", "fil", "c2"),
	}
	first, err := parse(in)
	if err != nil {
		t.Fatal(err)
	}
	var again Input
	for _, e := range first.Spoken {
		again.Spoken = append(again.Spoken, InputEntry{Language: string(e.Code), Level: e.Level.String()})
	}
	for _, e := range first.Learning {
		again.Learning = append(again.Learning, InputEntry{Language: string(e.Code), Level: e.Level.String()})
	}
	second, err := parse(again)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Equal(first) {
		t.Errorf("second = %+v, first = %+v", second, first)
	}
}

func TestParseDoesNotKeepOrChangeTheInput(t *testing.T) {
	in := Input{Spoken: entries("es", "native", "en", "c1"), Learning: entries("ja", "a2")}
	before := Input{Spoken: slices.Clone(in.Spoken), Learning: slices.Clone(in.Learning)}
	got, err := parse(in)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(in.Spoken, before.Spoken) || !slices.Equal(in.Learning, before.Learning) {
		t.Errorf("the input was changed: %+v", in)
	}
	in.Spoken[0] = InputEntry{Language: "de", Level: "a1"}
	in.Learning[0] = InputEntry{Language: "ko", Level: "b1"}
	want := Selection{Spoken: []Entry{{"es", LevelNative}, {"en", LevelC1}}, Learning: []Entry{{"ja", LevelA2}}}
	if !got.Equal(want) {
		t.Errorf("the selection followed the input: %+v", got)
	}
}

// What a member sends is theirs: an error names the list and the rule, never
// the codes or levels submitted.
func TestValidationErrorsNeverContainTheInput(t *testing.T) {
	const code, level = "SECRETCODE", "SECRETLEVEL"
	six := entries("es", "c1", "en", "c1", "pt", "c1", "fr", "c1", "it", "c1", "xq", "c1")
	for _, in := range []Input{
		{Spoken: entries(code, level), Learning: entries(code, level)},
		{Spoken: entries("xq", "c1", "xq", "c1"), Learning: entries("xq", "native")},
		{Spoken: six},
	} {
		err := mustFail(t, in)
		for _, secret := range []string{code, level, "xq", "native", "c1"} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error %q contains the input %q", err, secret)
			}
		}
	}
	err := Selection{Spoken: []Entry{{"xq", LevelNative}}}.checkKnown(func(Code) bool { return false })
	if err == nil || strings.Contains(err.Error(), "xq") || strings.Contains(err.Error(), "native") {
		t.Errorf("checkKnown error = %v", err)
	}
}

func TestValidationErrorMessage(t *testing.T) {
	err := &ValidationError{Fields: []*FieldError{{Field: "spoken", Code: "too_many"}, {Field: "learning", Code: "duplicate"}}}
	const want = "language: invalid input: spoken: too_many, learning: duplicate"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestCheckKnown(t *testing.T) {
	catalog := func(codes ...Code) func(Code) bool {
		return func(c Code) bool { return slices.Contains(codes, c) }
	}
	sel := Selection{
		Spoken:   []Entry{{"es", LevelNative}, {"en", LevelC1}},
		Learning: []Entry{{"ja", LevelA2}, {"ko", LevelA1}},
	}
	tests := []struct {
		name  string
		sel   Selection
		known func(Code) bool
		want  []FieldError
	}{
		{"all known", sel, catalog("es", "en", "ja", "ko", "fr"), nil},
		{"one unknown in spoken", sel, catalog("es", "ja", "ko"), []FieldError{{"spoken", "unknown_language"}}},
		{"one unknown in learning", sel, catalog("es", "en", "ja"), []FieldError{{"learning", "unknown_language"}}},
		{"unknown in both", sel, catalog("es", "ja"),
			[]FieldError{{"spoken", "unknown_language"}, {"learning", "unknown_language"}}},
		{"every code unknown is one error per list", sel, catalog(),
			[]FieldError{{"spoken", "unknown_language"}, {"learning", "unknown_language"}}},
		{"an empty selection asks nothing", Selection{}, catalog(), nil},
		{"only learning, unknown", Selection{Learning: []Entry{{"ja", LevelA2}}}, catalog("es"),
			[]FieldError{{"learning", "unknown_language"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.sel.checkKnown(tt.known)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("err = %v (%T), want nil", err, err)
				}
				return
			}
			if fields := fieldsOf(t, err); !slices.Equal(fields, tt.want) {
				t.Errorf("fields = %v, want %v", fields, tt.want)
			}
		})
	}
}

// The catalog is asked about every code exactly once and nothing else.
func TestCheckKnownAsksAboutEachCode(t *testing.T) {
	var asked []Code
	err := Selection{
		Spoken:   []Entry{{"es", LevelNative}, {"en", LevelC1}},
		Learning: []Entry{{"ja", LevelA2}},
	}.checkKnown(func(c Code) bool {
		asked = append(asked, c)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []Code{"es", "en", "ja"}; !slices.Equal(asked, want) {
		t.Errorf("asked about %v, want %v", asked, want)
	}
}
