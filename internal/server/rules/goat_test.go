package rules

import (
	"testing"
)

// UUID.hashCode of the nil UUID is 0, and new Random(0).nextInt(4) is 2
// (its first next(31) is 1569741360).
func TestGoatHornInstrumentMatchesJavaRandom(t *testing.T) {
	var id [16]byte
	if got := GoatHornInstrument(id, false); got != "minecraft:seek_goat_horn" {
		t.Errorf("regular horn of the nil uuid = %s, want the third of the tag", got)
	}
	if got := GoatHornInstrument(id, true); got != "minecraft:yearn_goat_horn" {
		t.Errorf("screaming horn of the nil uuid = %s, want the third of the tag", got)
	}
}
