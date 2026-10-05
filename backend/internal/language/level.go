package language

// Level is how well a member knows a language: the six CEFR levels, then
// native, as one ordered scale so that levels compare as numbers (a level
// >= LevelB2 is B2 or better, natives included). The numbers are the ones
// stored (migration 00006) and must never change. The zero value is not a
// level.
type Level int8

const (
	LevelA1     Level = 1
	LevelA2     Level = 2
	LevelB1     Level = 3
	LevelB2     Level = 4
	LevelC1     Level = 5
	LevelC2     Level = 6
	LevelNative Level = 7 // only a spoken language can be native
)

// levelNames holds the canonical name of each level, lowercase. How a level
// is shown ("A1") is up to whoever displays it.
var levelNames = [...]string{
	LevelA1:     "a1",
	LevelA2:     "a2",
	LevelB1:     "b1",
	LevelB2:     "b2",
	LevelC1:     "c1",
	LevelC2:     "c2",
	LevelNative: "native",
}

// String returns the level's canonical name, or "invalid" for a value that
// is not on the scale.
func (l Level) String() string {
	if l < LevelA1 || l > LevelNative {
		return "invalid"
	}
	return levelNames[l]
}

// parseLevel returns the level named exactly s. A name is an identifier, not
// text: it is never trimmed or lowercased.
func parseLevel(s string) (Level, bool) {
	for l := LevelA1; l <= LevelNative; l++ {
		if levelNames[l] == s {
			return l, true
		}
	}
	return 0, false
}

// levelFromRank returns the level stored as the number n, the only way a
// stored number becomes a Level.
func levelFromRank(n int) (Level, bool) {
	if n < int(LevelA1) || n > int(LevelNative) {
		return 0, false
	}
	return Level(n), true
}
