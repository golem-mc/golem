package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestCustomModelDataAndContainerLootSurviveASave checks that the two
// components a loot function can lay on a stack are written to a file and
// read back, that an empty list is left out of the tag, and that a colour
// stored as three channel floats reads as RGB.
// vanilla: CustomModelData.CODEC, SeededContainerLoot.CODEC
func TestCustomModelDataAndContainerLootSurviveASave(t *testing.T) {
	chest, _ := ItemID("minecraft:chest")
	stack := protocol.ItemStack{ItemID: chest, Count: 1,
		ContainerLoot: protocol.ContainerLoot{Table: "minecraft:chests/igloo_chest", Seed: -7}}
	model := protocol.CustomModelData{Floats: []float32{0.5}, Flags: []bool{true, false}, Strings: []string{"a"}, Colors: []int32{0x123456}}
	stack.SetCustomModelData(model)

	got, ok := StoredStack(StackTag(stack))
	if !ok || !got.Equal(stack) {
		t.Fatalf("read back %+v (%v), want %+v", got, ok, stack)
	}

	sparse := protocol.ItemStack{ItemID: chest, Count: 1}
	sparse.SetCustomModelData(protocol.CustomModelData{Floats: []float32{1}})
	components, _ := nbt.CompoundView(StackTag(sparse).Entries()["components"])
	if tag, _ := nbt.CompoundView(components["minecraft:custom_model_data"]); len(tag) != 1 || tag["floats"] == nil {
		t.Errorf("custom_model_data with one float saved as %v, want only floats", tag)
	}

	var vec protocol.ItemStack
	if !ReadComponent(&vec, "minecraft:custom_model_data", nbt.Compound{"colors": nbt.List{Elem: nbt.TypeList, Items: []nbt.Tag{
		nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(1), nbt.Float(0), nbt.Float(0.5)}},
	}}}) {
		t.Fatal("a colour of three floats was refused")
	}
	if d, _ := vec.CustomModelDataOf(); len(d.Colors) != 1 || uint32(d.Colors[0]) != 0xFFFF007F {
		t.Errorf("colours = %X, want [FFFF007F]", d.Colors)
	}
}
