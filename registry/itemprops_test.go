package registry

import (
	"reflect"
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

func TestItemPropsByID(t *testing.T) {
	for id, name := range Items {
		want := ItemProperties[name]
		if got := propsOf(int32(id)); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s (%d): properties differ from generated defaults", name, id)
		}
		stack := want.MaxStack
		if stack <= 0 {
			stack = 64
		}
		if got := MaxStackSize(int32(id)); got != stack {
			t.Fatalf("%s (%d): stack size = %d, want %d", name, id, got, stack)
		}
	}
	for _, id := range []int32{-1, -1 << 31, int32(len(Items)), 1<<31 - 1} {
		if got := propsOf(id); !reflect.DeepEqual(got, ItemProps{}) {
			t.Errorf("unknown item %d: properties = %+v", id, got)
		}
		if got := MaxStackSize(id); got != 64 {
			t.Errorf("unknown item %d: stack size = %d, want 64", id, got)
		}
	}
}
