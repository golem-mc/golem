package rules

import (
	"math/rand"
	"slices"
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

// A goat's horn is picked from its variant's tag, and the same UUID always
// picks the same instrument.
func TestGoatHornInstrumentFollowsTheVariantAndIsStable(t *testing.T) {
	seen := map[string]bool{}
	rng := rand.New(rand.NewSource(1))
	for range 64 {
		var id [16]byte
		rng.Read(id[:])
		regular := GoatHornInstrument(id, false)
		screaming := GoatHornInstrument(id, true)
		if regular != GoatHornInstrument(id, false) {
			t.Fatalf("uuid %v picked two horns", id)
		}
		inTag := func(name string, tag []string) bool {
			return slices.Contains(tag, name)
		}
		if !inTag(regular, regularGoatHorns) || !inTag(screaming, screamingGoatHorns) {
			t.Fatalf("uuid %v picked %s / %s outside the tags", id, regular, screaming)
		}
		seen[regular] = true
	}
	if len(seen) < 2 {
		t.Errorf("64 uuids all picked %v, want a spread over the tag", seen)
	}
}
