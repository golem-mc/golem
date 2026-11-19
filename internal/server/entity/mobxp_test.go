package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

// TestMonstersAndAnimalsWithoutATableEntryRollTheirExperience: Monster's
// xpReward of 5 and Animal.getBaseExperienceReward's 1 + nextInt(3).
func TestMonstersAndAnimalsWithoutATableEntryRollTheirExperience(t *testing.T) {
	for _, typ := range []MobType{TypeWitch, TypeEnderman} {
		for seed := range int64(200) {
			if got := NewMob(typ, 1, 0, 64, 0, seed).RollExperience(rules.LootKiller{}); got != 5 {
				t.Fatalf("%v seed %d: experience %d, want 5", typ, seed, got)
			}
		}
	}
	for _, typ := range []MobType{TypeWolf, TypeFox, TypeHorse, TypeLlama, TypeTurtle, TypeAxolotl, TypeFrog} {
		seen := map[int]bool{}
		for seed := range int64(200) {
			got := NewMob(typ, 1, 0, 64, 0, seed).RollExperience(rules.LootKiller{})
			if got < 1 || got > 3 {
				t.Fatalf("%v seed %d: experience %d, want 1 to 3", typ, seed, got)
			}
			seen[got] = true
		}
		if len(seen) != 3 {
			t.Errorf("%v rolled %v, want all of 1, 2 and 3", typ, seen)
		}
	}
}
