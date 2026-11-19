package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestAToolComponentSurvivesTheStoredForm: a stack's own tool is written to
// a file and read back the same, tags and block lists included.
func TestAToolComponentSurvivesTheStoredForm(t *testing.T) {
	t.Parallel()
	item, _ := ItemID("minecraft:stick")
	stack := protocol.ItemStack{ItemID: item, Count: 1}
	want := protocol.ToolComponent{
		Rules: []protocol.ToolRule{
			{Blocks: protocol.BlockPredicateSet{Present: true, Tag: "minecraft:mineable/pickaxe"},
				Speed: 4, HasSpeed: true, Correct: true, HasCorrect: true},
			{Blocks: protocol.BlockPredicateSet{Present: true, Inline: []int32{BlockIDs["minecraft:dirt"]}}},
		},
		DefaultMiningSpeed: 2, DamagePerBlock: 3,
	}
	stack.SetTool(want)

	back, ok := StoredStack(StackTag(stack))
	if !ok {
		t.Fatal("the stored stack was refused")
	}
	got, has := back.ToolOf()
	if !has || got.DefaultMiningSpeed != 2 || got.DamagePerBlock != 3 || got.CanDestroyBlocksInCreative ||
		len(got.Rules) != 2 || got.Rules[0].Blocks.Tag != want.Rules[0].Blocks.Tag || got.Rules[0].Speed != 4 || !got.Rules[0].Correct || got.Rules[1].Blocks.Inline[0] != want.Rules[1].Blocks.Inline[0] {
		t.Errorf("tool after the round trip = %+v (%v), want %+v", got, has, want)
	}
}

// TestEveryKeptRemovalSurvivesAFileRoundTrip: equippable, weapon and
// blocks_attacks removals are stored as their removal keys and read back.
func TestEveryKeptRemovalSurvivesAFileRoundTrip(t *testing.T) {
	t.Parallel()
	id, _ := ItemID("minecraft:shield")
	stack := protocol.ItemStack{ItemID: id, Count: 1}
	for _, c := range []int32{protocol.DataComponentEquippable, protocol.DataComponentWeapon, protocol.DataComponentBlocksAttacks} {
		if !stack.Remove(c) {
			t.Fatalf("component %d is not a kept removal", c)
		}
	}
	back, ok := StoredStack(StackTag(stack))
	if !ok || !back.Same(stack) {
		t.Errorf("after the round trip ok=%v removed=%b, want %b", ok, back.Removed, stack.Removed)
	}
}
