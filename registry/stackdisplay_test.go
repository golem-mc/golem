package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestTooltipDisplayDropsRepeatedComponents pins that a hidden component
// named twice is kept once, on the stack and in the saved tag.
// vanilla: TooltipDisplay.CODEC (a ReferenceLinkedOpenHashSet)
func TestTooltipDisplayDropsRepeatedComponents(t *testing.T) {
	t.Parallel()
	stack := protocol.ItemStack{ItemID: int32(ItemStick), Count: 1}
	value := nbt.Compound{"hidden_components": nbt.List{Elem: nbt.TypeString,
		Items: []nbt.Tag{nbt.String("enchantments"), nbt.String("minecraft:enchantments")}}}
	if ok, _ := readDisplayComponent(&stack, "minecraft:tooltip_display", value); !ok {
		t.Fatal("the tooltip_display did not read")
	}
	if _, hidden := stack.HiddenTooltipOf(); len(hidden) != 1 {
		t.Errorf("hidden components = %v, want one", hidden)
	}
	want := nbt.Compound{"hidden_components": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("minecraft:enchantments")}}}
	if got := StackComponents(stack)["minecraft:tooltip_display"]; !nbt.Equal(got, nbt.Tag(want)) {
		t.Errorf("saved tooltip_display = %v, want %v", got, want)
	}
}
