package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The grindstone's maths, worked against the registry's own data: a
// diamond pickaxe's 1561 of life and its twentieth of 78, sharpness's
// minimum cost of one and eleven a level above the first, the curse tag
// holding the binding and vanishing curses. Every expected number below
// is the vanilla transcription's own arithmetic run by hand on those
// facts.

// The enchantment IDs the tests curse by, beside the sharpness and
// efficiency the anvil's tests enchant with.
const (
	bindingCurse   = int32(2)
	vanishingCurse = int32(41)
)

func grindstoneItem(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("the item registry has no %s", name)
	}
	return id
}

func anEnchantedSword(t *testing.T, ench map[int32]int32) protocol.ItemStack {
	t.Helper()
	return protocol.ItemStack{
		ItemID:       grindstoneItem(t, "minecraft:diamond_sword"),
		Count:        1,
		Enchantments: ench,
	}
}

// TestGrindstoneCombineStripsALoneItemToItsCurses checks the disenchant on
// its own: the lone enchanted sword shows without its sharpness and with
// its repair cost started over; a sword carrying a curse shows with the
// curse alone, the doubling raising its cost to one; and an enchanted
// book without its magic turns back into the plain book.
func TestGrindstoneCombineStripsALoneItemToItsCurses(t *testing.T) {
	work := anEnchantedSword(t, map[int32]int32{sharpness: 1})
	work.RepairCost = 3
	got := GrindstoneCombine(work, protocol.ItemStack{})
	if want := (protocol.ItemStack{ItemID: work.ItemID, Count: 1}); !got.Equal(want) {
		t.Errorf("the stripped sword = %+v, want the plain %v", got, want)
	}

	cursed := anEnchantedSword(t, map[int32]int32{sharpness: 1, vanishingCurse: 1})
	got = GrindstoneCombine(cursed, protocol.ItemStack{})
	want := anEnchantedSword(t, map[int32]int32{vanishingCurse: 1})
	want.RepairCost = 1
	if !got.Equal(want) {
		t.Errorf("the cursed sword's strip = %+v, want %+v", got, want)
	}

	book := protocol.ItemStack{
		ItemID:             grindstoneItem(t, "minecraft:enchanted_book"),
		Count:              1,
		StoredEnchantments: map[int32]int32{sharpness: 1},
	}
	got = GrindstoneCombine(book, protocol.ItemStack{})
	if want := (protocol.ItemStack{ItemID: grindstoneItem(t, "minecraft:book"), Count: 1}); !got.Equal(want) {
		t.Errorf("the stripped book = %+v, want the plain %v", got, want)
	}
}

// TestGrindstoneCombineMergesTheWornPairIntoTheRepairs runs the merge of
// two worn pickaxes: the pair's remaining lives together with the item's
// whole twentieth of 78, capped at the whole life of 1561.
func TestGrindstoneCombineMergesTheWornPairIntoTheRepairs(t *testing.T) {
	cases := []struct {
		damage, want int32
	}{
		{1300, 961},  // 261 + 261 + 78 of life left
		{1550, 1461}, // 11 + 11 + 78
		{0, 0},       // the repaired past the item's whole life, capped at fresh
	}
	for _, c := range cases {
		pick := protocol.ItemStack{ItemID: grindstoneItem(t, "minecraft:diamond_pickaxe"), Count: 1, Damage: c.damage}
		got := GrindstoneCombine(pick, pick)
		want := protocol.ItemStack{ItemID: pick.ItemID, Count: 1, Damage: c.want}
		if !got.Equal(want) {
			t.Errorf("a pair worn to %d = %+v, want %+v", c.damage, got, want)
		}
	}
}

// TestGrindstoneAwardRollsFromHalfTheWorth pins the roll: the worth's
// rounding-up half flat, and a roll in [0, 1) naming as many points again
// below it; a worth of nothing lays no orbs at all.
func TestGrindstoneAwardRollsFromHalfTheWorth(t *testing.T) {
	cases := []struct {
		worth int
		roll  float64
		want  int
	}{
		{0, 0.5, 0},
		{1, 0.5, 1},
		{5, 0, 3},
		{5, 0.33, 3},
		{5, 0.34, 4},
		{5, 0.999, 5},
		{10, 0.25, 6},
	}
	for _, c := range cases {
		if got := GrindstoneAward(c.worth, c.roll); got != c.want {
			t.Errorf("award(%d, %v) = %d, want %d", c.worth, c.roll, got, c.want)
		}
	}
}
