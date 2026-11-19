package command

import (
	"testing"
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
