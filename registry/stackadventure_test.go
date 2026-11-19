package registry

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestAdventureAndTypedDataSurviveTheStoredForm: can_place_on, can_break,
// entity_data and block_entity_data read from their codec's NBT are written
// to a file and read back to the same wire bytes.
func TestAdventureAndTypedDataSurviveTheStoredForm(t *testing.T) {
	t.Parallel()
	cases := []struct {
		item, name string
		component  int32
		value      nbt.Tag
	}{
		{"minecraft:stone", "minecraft:can_place_on", protocol.DataComponentCanPlaceOn,
			nbt.Compound{"blocks": nbt.String("minecraft:dirt")}},
		{"minecraft:diamond_pickaxe", "minecraft:can_break", protocol.DataComponentCanBreak,
			nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"blocks": nbt.String("#minecraft:logs")},
				nbt.Compound{
					"blocks": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("stone"), nbt.String("minecraft:dirt")}},
					"state": nbt.Compound{"axis": nbt.String("y"), "age": nbt.Compound{"min": nbt.String("2")},
						"level": nbt.Compound{"min": nbt.String("1"), "max": nbt.String("3")}},
					"nbt": nbt.String(`{Items:[{id:"minecraft:stone"}]}`),
				},
				nbt.Compound{"state": nbt.Compound{}, "nbt": nbt.Compound{"x": nbt.Int(1)}},
			}}},
		{"minecraft:armor_stand", "minecraft:entity_data", protocol.DataComponentEntityData,
			nbt.Compound{"id": nbt.String("minecraft:armor_stand"), "Invisible": nbt.Byte(1)}},
		{"minecraft:chest", "minecraft:block_entity_data", protocol.DataComponentBlockEntityData,
			nbt.Compound{"id": nbt.String("chest"), "CustomName": nbt.String("x")}},
	}
	for _, tc := range cases {
		item, _ := ItemID(tc.item)
		stack := protocol.ItemStack{ItemID: item, Count: 1}
		if !ReadComponent(&stack, tc.name, tc.value) {
			t.Errorf("%s %v was refused", tc.name, tc.value)
			continue
		}
		want := rawOf(stack, tc.component)
		back, ok := StoredStack(StackTag(stack))
		if !ok {
			t.Errorf("%s: the stored stack %v was refused", tc.name, StackTag(stack))
			continue
		}
		if got := rawOf(back, tc.component); want == nil || !bytes.Equal(got, want) {
			t.Errorf("%s after the round trip = % X, want % X", tc.name, got, want)
		}
	}
}

func rawOf(stack protocol.ItemStack, component int32) []byte {
	for _, raw := range stack.Raw {
		if raw.Type == component {
			return raw.Data
		}
	}
	return nil
}
