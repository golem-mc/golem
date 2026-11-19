package command

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestGiveReadsAToolComponent: the component the issue's example gives is
// kept on the stack, in the rules and flags it names.
// vanilla: Tool.CODEC
func TestGiveReadsAToolComponent(t *testing.T) {
	t.Parallel()
	stack, ok := parseItemStack(`diamond_sword[tool={rules:[],can_destroy_blocks_in_creative:true}]`)
	if !ok {
		t.Fatal("the stack was refused")
	}
	tool, has := stack.ToolOf()
	if !has || len(tool.Rules) != 0 || !tool.CanDestroyBlocksInCreative || tool.DamagePerBlock != 1 || tool.DefaultMiningSpeed != 1 {
		t.Errorf("tool = %+v (%v), want no rules, creative allowed and the defaults", tool, has)
	}

	stack, ok = parseItemStack(`iron_pickaxe[tool={rules:[{blocks:"#minecraft:mineable/pickaxe",speed:5,correct_for_drops:true},{blocks:["stone","dirt"]}],can_destroy_blocks_in_creative:false,damage_per_block:3,default_mining_speed:2}]`)
	if !ok {
		t.Fatal("the pickaxe was refused")
	}
	tool, has = stack.ToolOf()
	if !has || len(tool.Rules) != 2 || tool.CanDestroyBlocksInCreative || tool.DamagePerBlock != 3 ||
		tool.DefaultMiningSpeed != 2 || tool.Rules[0].Blocks.Tag != "minecraft:mineable/pickaxe" ||
		tool.Rules[0].Speed != 5 || !tool.Rules[0].Correct || len(tool.Rules[1].Blocks.Inline) != 2 {
		t.Errorf("tool = %+v (%v)", tool, has)
	}
}

// TestGiveRefusesABadToolComponent: a speed that is not positive, a block
// that does not exist and a missing rules list are refused.
// vanilla: Tool.CODEC, ExtraCodecs.POSITIVE_FLOAT
func TestGiveRefusesABadToolComponent(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		`{rules:[{blocks:"stone",speed:0}]}`,
		`{rules:[{blocks:"no_such_block"}]}`,
		`{rules:[{blocks:"#minecraft:no_such_tag"}]}`,
		`{}`,
		`{rules:[],damage_per_block:-1}`,
	} {
		if _, ok := parseItemStack(`stick[tool=` + value + `]`); ok {
			t.Errorf("tool=%s was accepted", value)
		}
	}
}

// TestGiveAndItemModifierRemoveTheTool: a stack given with [!minecraft:tool]
// or [!attack_range] and one run through a set_components removal both
// carry the removal.
// vanilla: DataComponentPatch removed entries
func TestGiveAndItemModifierRemoveTheTool(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		item, name string
		component  int32
	}{
		{"diamond_pickaxe", "minecraft:tool", protocol.DataComponentTool},
		{"wooden_spear", "minecraft:attack_range", protocol.DataComponentAttackRange},
	} {
		given, ok := parseItemStack(c.item + `[!` + c.name + `]`)
		if !ok || !given.Removes(c.component) {
			t.Errorf("give %s: ok=%v removed=%v, want it removed", c.name, ok, given.Removes(c.component))
		}
		patched, _ := parseItemStack(c.item)
		if !patchComponent(&patched, c.name, "", true) || !patched.Removes(c.component) {
			t.Errorf("item modifier: %s removed=%v, want it removed", c.name, patched.Removes(c.component))
		}
	}
}
