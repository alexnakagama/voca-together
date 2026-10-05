package language

import "testing"

func TestParseCode(t *testing.T) {
	for _, in := range []string{"es", "en", "zh", "yue", "fil", "aa", "zzz"} {
		if got, ok := parseCode(in); !ok || got != Code(in) {
			t.Errorf("parseCode(%q) = %q, %v; want it accepted unchanged", in, got, ok)
		}
	}
	for _, in := range []string{
		"", "e", "espa", "spanish", "ES", "Es", "eS", "YUE", "e1", "12", "e-", "pt-BR", "pt_br", "zh-Hant",
		" es", "es ", "es\n", "\tes", "e s", "es\x00", "é", "ñu", "еs" /* Cyrillic е */, "ｅｓ", "日本", "\xff\xfe", "e\xff",
	} {
		if got, ok := parseCode(in); ok {
			t.Errorf("parseCode(%q) = %q, want it refused", in, got)
		}
	}
}

// The kinds are the values migration 00006 stores (user_languages_kind_check)
// and the names validation errors give the two lists.
func TestKindsAreTheStoredValues(t *testing.T) {
	if KindSpoken != "spoken" || KindLearning != "learning" {
		t.Errorf("kinds = %q, %q", KindSpoken, KindLearning)
	}
}

// Positions 0 to 4 are all the schema has (user_languages_position_range).
func TestMaxPerKindIsWhatTheSchemaHolds(t *testing.T) {
	if MaxPerKind != 5 {
		t.Errorf("MaxPerKind = %d, want 5", MaxPerKind)
	}
}

func TestSelectionEqual(t *testing.T) {
	base := Selection{
		Spoken:   []Entry{{"es", LevelNative}, {"en", LevelC1}},
		Learning: []Entry{{"ja", LevelA2}},
	}
	tests := []struct {
		name  string
		other Selection
		want  bool
	}{
		{"the same", Selection{
			Spoken:   []Entry{{"es", LevelNative}, {"en", LevelC1}},
			Learning: []Entry{{"ja", LevelA2}},
		}, true},
		{"reordered", Selection{
			Spoken:   []Entry{{"en", LevelC1}, {"es", LevelNative}},
			Learning: []Entry{{"ja", LevelA2}},
		}, false},
		{"a level changed", Selection{
			Spoken:   []Entry{{"es", LevelNative}, {"en", LevelC2}},
			Learning: []Entry{{"ja", LevelA2}},
		}, false},
		{"a language changed", Selection{
			Spoken:   []Entry{{"es", LevelNative}, {"en", LevelC1}},
			Learning: []Entry{{"ko", LevelA2}},
		}, false},
		{"a language removed", Selection{
			Spoken:   []Entry{{"es", LevelNative}},
			Learning: []Entry{{"ja", LevelA2}},
		}, false},
		{"a language added", Selection{
			Spoken:   []Entry{{"es", LevelNative}, {"en", LevelC1}},
			Learning: []Entry{{"ja", LevelA2}, {"ko", LevelA1}},
		}, false},
		{"a language moved to the other kind", Selection{
			Spoken:   []Entry{{"es", LevelNative}, {"en", LevelC1}, {"ja", LevelA2}},
			Learning: nil,
		}, false},
		{"the kinds swapped", Selection{
			Spoken:   []Entry{{"ja", LevelA2}},
			Learning: []Entry{{"es", LevelNative}, {"en", LevelC1}},
		}, false},
		{"empty", Selection{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := base.Equal(tt.other); got != tt.want {
				t.Errorf("base.Equal(other) = %v, want %v", got, tt.want)
			}
			if got := tt.other.Equal(base); got != tt.want {
				t.Errorf("other.Equal(base) = %v, want %v", got, tt.want)
			}
		})
	}
}

// No languages is one value however it is held: a list read from no rows is
// nil, a parsed empty list may not be.
func TestSelectionEqualTreatsNilAndEmptyListsAlike(t *testing.T) {
	a := Selection{}
	b := Selection{Spoken: []Entry{}, Learning: []Entry{}}
	if !a.Equal(b) || !b.Equal(a) || !a.Equal(a) {
		t.Error("empty selections differ")
	}
	c := Selection{Spoken: []Entry{{"es", LevelNative}}, Learning: []Entry{}}
	d := Selection{Spoken: []Entry{{"es", LevelNative}}}
	if !c.Equal(d) || !d.Equal(c) {
		t.Error("selections differing only in nil and empty lists differ")
	}
}
