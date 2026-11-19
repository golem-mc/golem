package rules

import (
	"math/rand"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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

// TestClosedWaterHoldsTheTreasureBack rolls a thousand closed catches:
// no treasure lands, the junk and the fish alone the water offers.
func TestClosedWaterHoldsTheTreasureBack(t *testing.T) {
	caught := fishingRolls(1000, 0, false)
	treasure := map[string]bool{"minecraft:name_tag": true, "minecraft:saddle": true,
		"minecraft:nautilus_shell": true, "minecraft:bow": true, "minecraft:book": true}
	for name, n := range caught {
		if treasure[name] && n > 0 {
			t.Fatalf("closed water caught %d of the %q, want the treasure held back", n, name)
		}
	}
}

// TestTheCatchPickIsReproducible rolls the same seed twice over: the same
// catches from each, so the pick walks its shares in their data-pack order
// and not a Go map's own iteration order.
func TestTheCatchPickIsReproducible(t *testing.T) {
	roll := func() []string {
		rng := rand.New(rand.NewSource(7))
		var got []string
		for range 500 {
			for _, stack := range FishingLoot(rng, 0, true, "", nil) {
				name, _ := registry.ItemName(stack.ItemID)
				got = append(got, name)
			}
		}
		return got
	}
	first, second := roll(), roll()
	if len(first) != len(second) {
		t.Fatalf("the same seed rolled %d catches then %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("the same seed rolled %q then %q at catch %d, so the pick is not reproducible", first[i], second[i], i)
		}
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

// TestTheWaterBottleWinsItsWater rolls the junk until the potion lands:
// the junk's set_potion gives it the water potion's contents.
func TestTheWaterBottleWinsItsWater(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	for range 2000 {
		for _, stack := range fishingJunk(rng, "") {
			name, _ := registry.ItemName(stack.ItemID)
			if name != "minecraft:potion" {
				continue
			}
			contents, ok := stack.PotionContentsOf()
			if !ok {
				t.Fatal("the caught water bottle carries no potion contents")
			}
			if contents.Potion != registry.MustPotion("minecraft:water") {
				t.Fatalf("the caught potion names potion %d, want the water's", contents.Potion)
			}
			return
		}
	}
	t.Fatal("no water bottle landed in two thousand rolls")
}

// TestTheTreasureDrawsFromOnRandomLoot checks the enchanted treasure's
// pool: the four enchantments the loot function's on_random_loot adds over
// the enchanting table's tag — mending, frost walker and the two curses —
// are reachable, while the table's own pool does not offer them.
func TestTheTreasureDrawsFromOnRandomLoot(t *testing.T) {
	book := protocol.ItemStack{ItemID: registry.MustItem("minecraft:book"), Count: 1}
	treasureOnly := map[string]bool{
		"minecraft:mending":         true,
		"minecraft:frost_walker":    true,
		"minecraft:binding_curse":   true,
		"minecraft:vanishing_curse": true,
	}
	reached := map[string]bool{}
	for _, o := range enchantmentsAvailable(30, book, registry.EnchantmentTagIDs("minecraft:on_random_loot")) {
		if name, ok := registry.EnchantmentName(o.offer.Enchantment); ok && treasureOnly[name] {
			reached[name] = true
		}
	}
	for name := range treasureOnly {
		if !reached[name] {
			t.Errorf("the on_random_loot pool cannot offer %s", name)
		}
	}
	for _, o := range enchantmentsAvailable(30, book, registry.EnchantingTableIDs()) {
		if name, ok := registry.EnchantmentName(o.offer.Enchantment); ok && treasureOnly[name] {
			t.Errorf("the enchanting table's pool offered the treasure-only %s", name)
		}
	}
}

// TestTheTreasureRollReachesOnRandomLoot rolls whole open-water catches a
// few thousand times: a treasure-only enchantment must land, which needs
// fishingEnchant to draw from the loot function's on_random_loot and not the
// enchanting table's tag. The pool checks above would not notice that swap.
func TestTheTreasureRollReachesOnRandomLoot(t *testing.T) {
	treasureOnly := map[int32]bool{}
	for _, name := range []string{"minecraft:mending", "minecraft:frost_walker",
		"minecraft:binding_curse", "minecraft:vanishing_curse"} {
		id, ok := registry.EnchantmentID(name)
		if !ok {
			t.Fatalf("the enchantment registry has no %s", name)
		}
		treasureOnly[id] = true
	}
	reached := func(stack protocol.ItemStack) bool {
		for _, enchants := range []map[int32]int32{stack.Enchantments, stack.StoredEnchantments} {
			for id := range enchants {
				if treasureOnly[id] {
					return true
				}
			}
		}
		return false
	}
	rng := rand.New(rand.NewSource(5))
	for range 5000 {
		if slices.ContainsFunc(FishingLoot(rng, 0, true, "", nil), reached) {
			return
		}
	}
	t.Fatal("five thousand open-water catches never reached a treasure-only enchantment")
}

// TestRodEnchantmentsFeedTheHook checks the two reads a cast carries: the
// Luck of the Sea into the roll and the Lure into the wait, both nought on
// a plain rod.
func TestRodEnchantmentsFeedTheHook(t *testing.T) {
	rod := protocol.ItemStack{ItemID: registry.MustItem("minecraft:fishing_rod"), Count: 1}
	if FishingLuck(rod) != 0 || FishingLureTicks(rod) != 0 {
		t.Fatalf("a plain rod carries luck %d lure %d, want the none", FishingLuck(rod), FishingLureTicks(rod))
	}
	luckID, ok := registry.EnchantmentID("minecraft:luck_of_the_sea")
	if !ok {
		t.Fatal("the enchantment registry has no luck_of_the_sea")
	}
	lureID, ok := registry.EnchantmentID("minecraft:lure")
	if !ok {
		t.Fatal("the enchantment registry has no lure")
	}
	rod.Enchantments = map[int32]int32{luckID: 3, lureID: 2}
	if got := FishingLuck(rod); got != 3 {
		t.Errorf("Luck of the Sea III gave the hook %d luck, want the three", got)
	}
	if got := FishingLureTicks(rod); got != 200 {
		t.Errorf("Lure II gave the hook %d ticks off the wait, want the two hundred", got)
	}
}

// TestTheInkSacComesTenTimes rolls until the ink lands: the junk's
// set-count of ten on it, the count a catch of it drops.
func TestTheInkSacComesTenTimes(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	for range 2000 {
		for _, stack := range FishingLoot(rng, 0, false, "", nil) {
			name, _ := registry.ItemName(stack.ItemID)
			if name == "minecraft:ink_sac" {
				if stack.Count != 10 {
					t.Fatalf("the ink sac caught is %d, want the ten its table rolls", stack.Count)
				}
				return
			}
		}
	}
	t.Fatal("no ink sac landed in two thousand rolls of the plain water")
}
