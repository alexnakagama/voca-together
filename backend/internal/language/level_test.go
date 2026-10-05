package language

import "testing"

// The numbers are the ones migration 00006 stores and constrains
// (user_languages_level_range, user_languages_native_is_spoken); the names
// are the API's.
var levelTable = []struct {
	level Level
	rank  int
	name  string
}{
	{LevelA1, 1, "a1"},
	{LevelA2, 2, "a2"},
	{LevelB1, 3, "b1"},
	{LevelB2, 4, "b2"},
	{LevelC1, 5, "c1"},
	{LevelC2, 6, "c2"},
	{LevelNative, 7, "native"},
}

func TestLevelsMapToRanksAndNamesBothWays(t *testing.T) {
	for _, tt := range levelTable {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.level) != tt.rank {
				t.Errorf("rank = %d, want %d", int(tt.level), tt.rank)
			}
			if got := tt.level.String(); got != tt.name {
				t.Errorf("String() = %q, want %q", got, tt.name)
			}
			if got, ok := parseLevel(tt.name); !ok || got != tt.level {
				t.Errorf("parseLevel(%q) = %v, %v; want %v", tt.name, got, ok, tt.level)
			}
			if got, ok := levelFromRank(tt.rank); !ok || got != tt.level {
				t.Errorf("levelFromRank(%d) = %v, %v; want %v", tt.rank, got, ok, tt.level)
			}
		})
	}
}

func TestLevelsAreOrdered(t *testing.T) {
	for i := 1; i < len(levelTable); i++ {
		if lower, higher := levelTable[i-1].level, levelTable[i].level; lower >= higher {
			t.Errorf("%v is not below %v", lower, higher)
		}
	}
}

func TestParseLevelRejectsAnythingNotCanonical(t *testing.T) {
	for _, in := range []string{
		"", "A1", "B2", "Native", "NATIVE", " a1", "a1 ", "a1\n", "a 1", "a0", "a3", "c3", "d1",
		"nativ", "natives", "1", "7", "a1,a2", "ａ1", "a１", "а1" /* Cyrillic а */, "a1\x00", "\xff",
	} {
		if got, ok := parseLevel(in); ok {
			t.Errorf("parseLevel(%q) = %v, want it refused", in, got)
		}
	}
}

func TestLevelFromRankRejectsRanksOffTheScale(t *testing.T) {
	for _, n := range []int{0, -1, 8, 127, 128, 256 + 1, -255} {
		if got, ok := levelFromRank(n); ok {
			t.Errorf("levelFromRank(%d) = %v, want it refused", n, got)
		}
	}
}

// A Level that isn't on the scale, the zero value included, must not print as
// a real level.
func TestInvalidLevelsDoNotPrintAsALevel(t *testing.T) {
	for _, l := range []Level{0, -1, 8, 100} {
		got := l.String()
		for _, tt := range levelTable {
			if got == tt.name {
				t.Errorf("Level(%d).String() = %q, a real level", int(l), got)
			}
		}
		if got == "" {
			t.Errorf("Level(%d).String() is empty", int(l))
		}
	}
}
