package registry

import (
	"testing"
)

// TestRequiresCorrectTool pins the generated requiresCorrectToolForDrops
// table against vanilla: the pistons, an iron door, a rail, a lantern, a
// shulker box, ice and a button drop for a bare hand, while cobweb, the
// snows, the command block and a plain stone block ask for the tool.
func TestRequiresCorrectTool(t *testing.T) {
	for _, name := range []string{
		"minecraft:piston", "minecraft:sticky_piston",
		"minecraft:iron_door", "minecraft:rail", "minecraft:lantern",
		"minecraft:shulker_box", "minecraft:ice", "minecraft:stone_button",
	} {
		b, _ := BlockByName(name)
		if RequiresCorrectTool(b) {
			t.Errorf("%s: RequiresCorrectTool says its drops need a tool", name)
		}
	}
	for _, name := range []string{
		"minecraft:stone", "minecraft:cobweb", "minecraft:snow",
		"minecraft:snow_block", "minecraft:command_block",
	} {
		b, _ := BlockByName(name)
		if !RequiresCorrectTool(b) {
			t.Errorf("%s: RequiresCorrectTool says its drops need no tool", name)
		}
	}
}

// A tool rule names its blocks by tag, so a pack's change to the tag changes
// the mining speed and the drops of the item at once.
// vanilla: Tool.getMiningSpeed, Tool.isCorrectForDrops
func TestToolRulesFollowTheLoadedTags(t *testing.T) {
	t.Cleanup(func() { ApplyPackTags(nil) })
	axe, obsidian := int32(ItemDiamondAxe), BlockObsidian
	if CorrectForDrops(axe, obsidian) {
		t.Fatal("a diamond axe is the wrong tool for obsidian")
	}
	if got := MiningSpeed(axe, obsidian); got != 1 {
		t.Fatalf("speed on obsidian before the pack = %v, want the default 1", got)
	}
	// A pack adds obsidian to #mineable/axe, and in one case to the tag that
	// denies a diamond tool's drops.
	ApplyPackTags(PackTags{"minecraft:block": {
		"minecraft:mineable/axe":               {Entries: []PackTagEntry{{ID: "minecraft:obsidian"}}},
		"minecraft:incorrect_for_diamond_tool": {Entries: []PackTagEntry{{ID: "minecraft:obsidian"}}},
	}})
	if got := MiningSpeed(axe, obsidian); got != 8 {
		t.Errorf("diamond axe speed on obsidian in #mineable/axe = %v, want 8", got)
	}
	if CorrectForDrops(axe, obsidian) {
		t.Error("a diamond axe is right for a block #incorrect_for_diamond_tool denies")
	}
	ApplyPackTags(PackTags{"minecraft:block": {
		"minecraft:mineable/axe": {Entries: []PackTagEntry{{ID: "minecraft:obsidian"}}},
	}})
	if !CorrectForDrops(axe, obsidian) {
		t.Error("a diamond axe is the right tool for obsidian in #mineable/axe")
	}
	ApplyPackTags(nil)
	if got := MiningSpeed(axe, obsidian); got != 1 || CorrectForDrops(axe, obsidian) {
		t.Errorf("after the pack unloads: speed %v, drops %v", got, CorrectForDrops(axe, obsidian))
	}
}
