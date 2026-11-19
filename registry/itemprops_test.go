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
