package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestEquippableOfTable pins which stored equippable values convert and the
// slot each one reads back as.
// vanilla: Equippable.CODEC
func TestEquippableOfTable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value nbt.Tag
		slot  string
		ok    bool
	}{
		{"chest", nbt.Compound{"slot": nbt.String("chest")}, "chest", true},
		{"feet with sound", nbt.Compound{"slot": nbt.String("feet"), "equip_sound": nbt.String("item.armor.equip_iron")}, "feet", true},
		{"asset and flags", nbt.Compound{"slot": nbt.String("head"), "asset_id": nbt.String("iron"), "swappable": nbt.Byte(0)}, "head", true},
		{"not a compound", nbt.String("chest"), "", false},
		{"no slot", nbt.Compound{}, "", false},
		{"unknown slot", nbt.Compound{"slot": nbt.String("tail")}, "", false},
		{"bad sound name", nbt.Compound{"slot": nbt.String("chest"), "equip_sound": nbt.String("Bad Sound")}, "", false},
		{"unregistered sound name", nbt.Compound{"slot": nbt.String("chest"), "equip_sound": nbt.String("minecraft:custom.sound")}, "", false},
		{"inline sound with an int range", nbt.Compound{"slot": nbt.String("chest"), "equip_sound": nbt.Compound{"sound_id": nbt.String("custom.sound"), "range": nbt.Int(16)}}, "chest", true},
		{"inline sound with a malformed range", nbt.Compound{"slot": nbt.String("chest"), "equip_sound": nbt.Compound{"sound_id": nbt.String("custom.sound"), "range": nbt.String("x")}}, "chest", true},
		{"bad asset id", nbt.Compound{"slot": nbt.String("chest"), "asset_id": nbt.String("Bad Id")}, "", false},
		{"flag not a bool", nbt.Compound{"slot": nbt.String("chest"), "dispensable": nbt.String("x")}, "", false},
		{"unknown entity", nbt.Compound{"slot": nbt.String("chest"), "allowed_entities": nbt.String("minecraft:nope")}, "", false},
	} {
		raw, ok := equippableOf(tc.value)
		if ok != tc.ok {
			t.Errorf("%s: ok = %v, want %v", tc.name, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		var s protocol.ItemStack
		s.SetRaw(raw)
		if got, has := s.EquippableOf(); !has || got.Slot != tc.slot {
			t.Errorf("%s: slot = %q, %v; want %q", tc.name, got.Slot, has, tc.slot)
		}
	}
}

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

// TestStackEquippableTable pins that a stack's own equippable replaces its
// item's, and that removing it leaves none.
// vanilla: ItemStack.get(DataComponents.EQUIPPABLE)
func TestStackEquippableTable(t *testing.T) {
	t.Parallel()
	helmet := protocol.ItemStack{ItemID: int32(ItemLeatherHelmet), Count: 1}
	if got, ok := StackEquippable(helmet); !ok || got.Slot != "head" || !got.DamageOnHurt {
		t.Errorf("a plain leather helmet = %+v, %v; want its item's head equippable", got, ok)
	}
	raw, ok := equippableOf(nbt.Compound{"slot": nbt.String("chest"), "allowed_entities": nbt.List{Elem: nbt.TypeString,
		Items: []nbt.Tag{nbt.String("player"), nbt.String("zombie")}}, "damage_on_hurt": nbt.Byte(0), "shearing_sound": nbt.Compound{"sound_id": nbt.String("custom.snip")}})
	if !ok {
		t.Fatal("the chest equippable did not read")
	}
	own := helmet
	own.SetRaw(raw)
	want := EquippableItem{Slot: "chest", Swappable: true, Entities: "minecraft:player,minecraft:zombie",
		EquipSound: SoundItemArmorEquipGeneric.Holder(), ShearingSound: protocol.SoundHolder{Name: "minecraft:custom.snip"}, Dispensable: true}
	if got, ok := StackEquippable(own); !ok || got != want {
		t.Errorf("the helmet's own equippable = %+v, %v; want %+v", got, ok, want)
	}
	removed := helmet
	removed.Remove(protocol.DataComponentEquippable)
	if got, ok := StackEquippable(removed); ok {
		t.Errorf("a helmet without its equippable = %+v, want none", got)
	}
	if !StackSparedHurtWear(removed, DamageTypeGeneric) || StackSparedHurtWear(helmet, DamageTypeGeneric) {
		t.Error("only the helmet with an equippable wears from a blow")
	}
}
