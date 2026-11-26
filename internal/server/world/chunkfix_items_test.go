package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func chestWith(components nbt.Compound) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:chest"), "x": nbt.Int(16), "y": nbt.Int(64), "z": nbt.Int(32),
		"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"Slot": nbt.Byte(0), "id": nbt.String("minecraft:diamond_sword"), "count": nbt.Int(1), "components": components},
		}},
	}
}

func TestOldChestEnchantedSwordLoads(t *testing.T) {
	t.Parallel()
	for _, version := range []int32{4189, 3837} {
		enchants := nbt.Compound{"levels": nbt.Compound{"minecraft:sharpness": nbt.Int(5)}, "show_in_tooltip": nbt.Byte(1)}
		w, pos := oldChunkWorld(t, version, chestWith(nbt.Compound{"minecraft:enchantments": enchants}))
		w.Acquire(pos)
		be := w.BlockEntity(16, 64, 32)
		if be == nil {
			t.Fatalf("%d: the chest was not loaded", version)
		}
		sharpness, _ := registry.EnchantmentID("minecraft:sharpness")
		stack := be.Slots[0]
		if stack.ItemID != int32(registry.ItemDiamondSword) || stack.Enchantments[sharpness] != 5 || be.Stored != nil {
			t.Errorf("%d: slot 0 = %+v, stored %v", version, stack, be.Stored)
		}
	}
}

func TestFixComponentShapes(t *testing.T) {
	t.Parallel()
	c := nbt.Compound{
		"minecraft:custom_model_data": nbt.Int(7),
		"minecraft:custom_name":       nbt.String(`{"text":"Hi","bold":true}`),
		"minecraft:food":              nbt.Compound{"nutrition": nbt.Int(4), "saturation": nbt.Float(1), "eat_seconds": nbt.Float(0.5)},
		"minecraft:unbreakable":       nbt.Compound{"show_in_tooltip": nbt.Byte(0)},
		"minecraft:attribute_modifiers": nbt.Compound{"show_in_tooltip": nbt.Byte(1), "modifiers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"name": nbt.String("Random spawn bonus"), "amount": nbt.Double(1)},
			nbt.Compound{"name": nbt.String("Random spawn bonus"), "amount": nbt.Double(2)},
		}}},
	}
	fixComponentShapes(c, 3837)
	cmd, _ := nbt.CompoundView(c["minecraft:custom_model_data"])
	if floats, _ := cmd["floats"].(nbt.List); len(floats.Items) != 1 || floats.Items[0] != nbt.Float(7) {
		t.Errorf("custom_model_data = %v", c["minecraft:custom_model_data"])
	}
	name, _ := nbt.CompoundView(c["minecraft:custom_name"])
	if name["text"] != nbt.String("Hi") || name["bold"] != nbt.Byte(1) {
		t.Errorf("custom_name = %v", c["minecraft:custom_name"])
	}
	consumable, _ := nbt.CompoundView(c["minecraft:consumable"])
	if consumable["consume_seconds"] != nbt.Float(0.5) {
		t.Errorf("consumable = %v", consumable)
	}
	display, _ := nbt.CompoundView(c["minecraft:tooltip_display"])
	hidden, _ := display["hidden_components"].(nbt.List)
	if len(hidden.Items) != 1 || hidden.Items[0] != nbt.String("minecraft:unbreakable") {
		t.Errorf("tooltip_display = %v", display)
	}
	mods, _ := c["minecraft:attribute_modifiers"].(nbt.List)
	if len(mods.Items) != 1 {
		t.Fatalf("attribute_modifiers = %v", c["minecraft:attribute_modifiers"])
	}
	m, _ := nbt.CompoundView(mods.Items[0])
	if m["id"] != nbt.String("minecraft:random_spawn_bonus") || m["amount"] != nbt.Double(3) || m["name"] != nil {
		t.Errorf("modifier = %v", m)
	}
}
