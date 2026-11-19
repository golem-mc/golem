package rules

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func blastItem(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("no item %s", name)
	}
	return id
}

// TestBlastDropsMergeToSixteenAtTheFirstPosition checks that stacks of one
// item fill the first collector to 16, spill into the next, and keep the
// position of the first block that dropped into each.
func TestBlastDropsMergeToSixteenAtTheFirstPosition(t *testing.T) {
	cobble := blastItem(t, "minecraft:cobblestone")
	var d BlastDrops
	for i := range 40 {
		d.Add(protocol.ItemStack{ItemID: cobble, Count: 1}, [3]int32{int32(i), 0, 0})
	}
	got := d.Drops()
	if len(got) != 3 {
		t.Fatalf("%d stacks, want 3", len(got))
	}
	for i, want := range []struct {
		x     int32
		count int32
	}{{0, 16}, {16, 16}, {32, 8}} {
		if got[i].Stack.Count != want.count || got[i].Pos[0] != want.x {
			t.Errorf("stack %d = %d at x=%d, want %d at x=%d", i, got[i].Stack.Count, got[i].Pos[0], want.count, want.x)
		}
	}
}

// TestBlastDropsKeepDifferentItemsApart checks that only the same item with
// the same components merges.
func TestBlastDropsKeepDifferentItemsApart(t *testing.T) {
	cobble := blastItem(t, "minecraft:cobblestone")
	dirt := blastItem(t, "minecraft:dirt")
	var d BlastDrops
	d.Add(protocol.ItemStack{ItemID: cobble, Count: 1}, [3]int32{})
	d.Add(protocol.ItemStack{ItemID: dirt, Count: 1}, [3]int32{1, 0, 0})
	d.Add(protocol.ItemStack{ItemID: cobble, Count: 2}, [3]int32{2, 0, 0})
	d.Add(protocol.ItemStack{ItemID: cobble, Count: 1, Enchantments: map[int32]int32{1: 1}}, [3]int32{3, 0, 0})
	d.Add(protocol.ItemStack{ItemID: cobble, Count: 0}, [3]int32{4, 0, 0})
	got := d.Drops()
	if len(got) != 3 {
		t.Fatalf("%d stacks, want cobble 3, dirt 1, enchanted cobble 1", len(got))
	}
	if got[0].Stack.Count != 3 || got[1].Stack.Count != 1 || got[2].Stack.Count != 1 {
		t.Errorf("counts = %d, %d, %d, want 3, 1, 1", got[0].Stack.Count, got[1].Stack.Count, got[2].Stack.Count)
	}
}

// TestBlastDropsSplitOnTheCapNotTheStackSize checks that a stack of 64 items
// stops merging at 16, while an unstackable item never merges.
func TestBlastDropsSplitOnTheCapNotTheStackSize(t *testing.T) {
	cobble := blastItem(t, "minecraft:cobblestone")
	var d BlastDrops
	d.Add(protocol.ItemStack{ItemID: cobble, Count: 10}, [3]int32{})
	d.Add(protocol.ItemStack{ItemID: cobble, Count: 10}, [3]int32{1, 0, 0})
	got := d.Drops()
	if len(got) != 2 || got[0].Stack.Count != 16 || got[1].Stack.Count != 4 {
		t.Errorf("stacks = %+v, want 16 then 4", got)
	}
	sword := blastItem(t, "minecraft:diamond_sword")
	var s BlastDrops
	s.Add(protocol.ItemStack{ItemID: sword, Count: 1}, [3]int32{})
	s.Add(protocol.ItemStack{ItemID: sword, Count: 1}, [3]int32{1, 0, 0})
	if len(s.Drops()) != 2 {
		t.Errorf("%d swords stacks, want 2", len(s.Drops()))
	}
}

// TestDecayedBlastDropsMergeIntoFewStacks rolls 800 stones of a radius-four
// blast with a seeded RNG and feeds the survivors through the collector:
// about 200 cobblestone arrive in 13 stacks at most, not one entity each.
func TestDecayedBlastDropsMergeIntoFewStacks(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	var d BlastDrops
	rolled := 0
	for i := range 800 {
		ctx := LootContext{BlockState: registry.MustState("minecraft:stone", nil), HasBlock: true, Radius: 4}
		for _, s := range RollLoot("minecraft:blocks/stone", &ctx, rng) {
			rolled += int(s.Count)
			d.Add(s, [3]int32{int32(i), 0, 0})
		}
	}
	if rolled < 160 || rolled > 240 {
		t.Fatalf("%d cobblestone survived the decay, want about 200", rolled)
	}
	total := 0
	for _, drop := range d.Drops() {
		total += int(drop.Stack.Count)
	}
	if total != rolled || len(d.Drops()) != (rolled+15)/16 {
		t.Errorf("%d cobblestone in %d stacks, want %d in %d", total, len(d.Drops()), rolled, (rolled+15)/16)
	}
}
