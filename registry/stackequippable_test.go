package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestEquippableTagRoundTrip pins that a stored equippable written back by
// StackComponents reads as itself, the codec's defaults left out.
// vanilla: Equippable.CODEC
func TestEquippableTagRoundTrip(t *testing.T) {
	t.Parallel()
	for _, value := range []nbt.Compound{
		{"slot": nbt.String("head")},
		{"slot": nbt.String("chest"), "equip_sound": nbt.String("minecraft:item.armor.equip_gold"), "asset_id": nbt.String("minecraft:iron"),
			"camera_overlay": nbt.String("minecraft:misc/pumpkinblur"), "allowed_entities": nbt.String("minecraft:zombie"),
			"dispensable": nbt.Byte(0), "swappable": nbt.Byte(0), "damage_on_hurt": nbt.Byte(0), "equip_on_interact": nbt.Byte(1),
			"can_be_sheared": nbt.Byte(1), "shearing_sound": nbt.String("minecraft:item.saddle.unequip")},
		{"slot": nbt.String("body"), "allowed_entities": nbt.String("#minecraft:can_equip_harness")},
		{"slot": nbt.String("feet"), "allowed_entities": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("minecraft:player"), nbt.String("minecraft:zombie")}}},
		{"slot": nbt.String("legs"), "equip_sound": nbt.Compound{"sound_id": nbt.String("minecraft:custom.sound")}},
		{"slot": nbt.String("legs"), "equip_sound": nbt.Compound{"sound_id": nbt.String("minecraft:item.armor.equip_generic")}},
		{"slot": nbt.String("legs"), "equip_sound": nbt.Compound{"sound_id": nbt.String("minecraft:custom.sound"), "range": nbt.Float(8)}},
	} {
		raw, ok := equippableOf(value)
		if !ok {
			t.Errorf("%v did not read", value)
			continue
		}
		var s protocol.ItemStack
		s.ItemID = int32(ItemStick)
		s.Count = 1
		s.SetRaw(raw)
		got := StackComponents(s)["minecraft:equippable"]
		if !nbt.Equal(got, nbt.Tag(value)) {
			t.Errorf("%v wrote back as %v", value, got)
		}
	}
}
