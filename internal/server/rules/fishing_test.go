package rules

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The fishing loot as ordinary unit tests over the roll the whole catch
// hangs on: which of the water's three kinds a hook pays, and what each
// kind can hand back. The black-box tests in internal/server/server_fishing_test.go
// remain the proof of the catch as the angler takes it.

// fishingRolls runs the roll n times over a fixed source, giving every
// catch the chance to land: truly deterministic per run of the suite, and
// wide enough that a mis-weighted kind shows.
func fishingRolls(n int, luck float64, openWater bool) map[string]int {
	rng := rand.New(rand.NewSource(11))
	caught := map[string]int{}
	for range n {
		for _, stack := range FishingLoot(rng, luck, openWater, "", nil) {
			name, _ := registry.ItemName(stack.ItemID)
			caught[name]++
		}
	}
	return caught
}

// TestTheFishingRollKeepsItsKinds rolls an open-water catch a thousand
// times: the cod alone must outweigh the junk's every item put together,
// and the junk's every item put together outweigh the treasure.
func TestTheFishingRollKeepsItsKinds(t *testing.T) {
	caught := fishingRolls(1000, 0, true)
	cod := caught["minecraft:cod"]
	var junk, treasure int
	for _, name := range []string{"minecraft:lily_pad", "minecraft:leather_boots",
		"minecraft:leather", "minecraft:bone", "minecraft:string",
		"minecraft:fishing_rod", "minecraft:bowl", "minecraft:stick",
		"minecraft:ink_sac", "minecraft:tripwire_hook", "minecraft:rotten_flesh"} {
		junk += caught[name]
	}
	for _, name := range []string{"minecraft:name_tag", "minecraft:saddle",
		"minecraft:nautilus_shell", "minecraft:bow", "minecraft:book"} {
		treasure += caught[name]
	}
	if cod == 0 || junk == 0 || treasure == 0 {
		t.Fatalf("the roll gave cod=%d junk=%d treasure=%d, want all three kinds landing", cod, junk, treasure)
	}
	// The shares: fish 85, junk 10 and treasure 5 of a hundred — cod alone
	// 60% of the fish's 85 — with the potion's roll dropped and a catch's
	// thousand rolls still owing each kind its neighbourhood.
	if cod < 400 || cod > 640 {
		t.Errorf("cod caught %d times in a thousand, want its three-fifths-of-85 share's neighbourhood", cod)
	}
	if junk < 20 || junk > 210 {
		t.Errorf("the junk caught %d times in a thousand, want its share's neighbourhood", junk)
	}
	if treasure < 5 || treasure > 100 {
		t.Errorf("the treasure caught %d times in a thousand, want its share's neighbourhood", treasure)
	}
}

// TestLuckOfTheSeaWeighsTheRoll rolls a lucky catch and an unlucky plain
// one against the same source: the quality the table folds its luck in
// by moves the roll's kind toward the treasure and off the junk.
func TestLuckOfTheSeaWeighsTheRoll(t *testing.T) {
	plain := fishingRolls(1000, 0, true)
	lucky := fishingRolls(1000, 3, true)
	var plainTreasure, luckyTreasure, plainJunk, luckyJunk int
	treasure := map[string]bool{"minecraft:name_tag": true, "minecraft:saddle": true,
		"minecraft:nautilus_shell": true, "minecraft:bow": true, "minecraft:book": true}
	junk := map[string]bool{"minecraft:lily_pad": true, "minecraft:leather_boots": true,
		"minecraft:leather": true, "minecraft:bone": true, "minecraft:string": true,
		"minecraft:fishing_rod": true, "minecraft:bowl": true, "minecraft:stick": true,
		"minecraft:ink_sac": true, "minecraft:tripwire_hook": true, "minecraft:rotten_flesh": true}
	for name, n := range plain {
		if treasure[name] {
			plainTreasure += n
		}
		if junk[name] {
			plainJunk += n
		}
	}
	for name, n := range lucky {
		if treasure[name] {
			luckyTreasure += n
		}
		if junk[name] {
			luckyJunk += n
		}
	}
	if luckyTreasure <= plainTreasure {
		t.Errorf("the lucky roll's treasure = %d, want it past the plain one's %d", luckyTreasure, plainTreasure)
	}
	if luckyJunk >= plainJunk {
		t.Errorf("the lucky roll's junk = %d, want it under the plain one's %d", luckyJunk, plainJunk)
	}
}

// TestTheTreasureIsEnchanted rolls the treasure table until its bow, rod
// and book have each landed: the bow and the rod carry their enchantments,
// the book the enchanted book's stored ones.
func TestTheTreasureIsEnchanted(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	seen := map[string]int{}
	for range 2000 {
		for _, stack := range fishingTreasure(rng) {
			name, _ := registry.ItemName(stack.ItemID)
			switch name {
			case "minecraft:bow", "minecraft:fishing_rod":
				if len(stack.Enchantments) == 0 {
					t.Fatalf("the treasure %s came up unenchanted", name)
				}
				seen[name]++
			case "minecraft:enchanted_book":
				if len(stack.StoredEnchantments) == 0 {
					t.Fatal("the enchanted book came up with no stored enchantments")
				}
				seen[name]++
			}
		}
	}
	for _, name := range []string{"minecraft:bow", "minecraft:fishing_rod", "minecraft:enchanted_book"} {
		if seen[name] == 0 {
			t.Fatalf("no %s landed in two thousand treasure rolls", name)
		}
	}
}
