package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestProtectionComponentsRoundTripThroughAStack checks that a stored
// stack's glider, death_protection and damage_resistant read into the
// stack, win over the item's defaults and come back out as tags that read
// the same. vanilla: Unit.CODEC, DeathProtection.CODEC, DamageResistant.CODEC
func TestProtectionComponentsRoundTripThroughAStack(t *testing.T) {
	stick, _ := ItemID("minecraft:stick")
	stack := protocol.ItemStack{ItemID: stick, Count: 1}
	if StackGlider(stack) || StackResistsDamage(stack, DamageTypeLava) {
		t.Fatal("a plain stick glides or resists lava")
	}
	if _, ok := StackDeathProtection(stack); ok {
		t.Fatal("a plain stick saves its holder")
	}
	effects := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"type": nbt.String("clear_all_effects")}}}
	for name, value := range map[string]nbt.Tag{
		"minecraft:glider":           nbt.Compound{},
		"minecraft:death_protection": nbt.Compound{"death_effects": effects},
		"minecraft:damage_resistant": nbt.Compound{"types": nbt.String("minecraft:lava")},
	} {
		if !readComponent(&stack, name, value) {
			t.Fatalf("%s was refused", name)
		}
	}
	if !StackGlider(stack) {
		t.Error("the stick's own glider is not seen")
	}
	if death, ok := StackDeathProtection(stack); !ok || len(death) != 1 || death[0].Type != ConsumeEffectType(protocol.ConsumeEffectClearAllEffects) {
		t.Errorf("StackDeathProtection = %+v, %v", death, ok)
	}
	if !StackResistsDamage(stack, DamageTypeLava) || StackResistsDamage(stack, DamageTypeInFire) {
		t.Error("damage_resistant [lava] does not name exactly lava")
	}

	again := protocol.ItemStack{ItemID: stick, Count: 1}
	for name, value := range withProtectionTags(nil, stack) {
		if !readComponent(&again, name, value) {
			t.Fatalf("the written %s was refused on reading back", name)
		}
	}
	if !again.HasGlider() || !StackResistsDamage(again, DamageTypeLava) {
		t.Errorf("the tag round trip lost a component: %+v", again.Raw)
	}
	if death, ok := again.DeathProtectionOf(); !ok || len(death) != 1 {
		t.Errorf("the round-tripped death_protection = %+v, %v", death, ok)
	}
	if readComponent(&again, "minecraft:damage_resistant", nbt.Compound{}) {
		t.Error("a damage_resistant without types was accepted")
	}

	// The defaults: the elytra glides, netherite resists fire, the nether
	// star explosions, the totem saves.
	for item, check := range map[string]func(protocol.ItemStack) bool{
		"minecraft:elytra":          StackGlider,
		"minecraft:netherite_ingot": func(s protocol.ItemStack) bool { return StackResistsDamage(s, DamageTypeLava) },
		"minecraft:nether_star":     func(s protocol.ItemStack) bool { return StackResistsDamage(s, DamageTypeExplosion) },
		"minecraft:totem_of_undying": func(s protocol.ItemStack) bool {
			_, ok := StackDeathProtection(s)
			return ok
		},
	} {
		id, _ := ItemID(item)
		if !check(protocol.ItemStack{ItemID: id, Count: 1}) {
			t.Errorf("%s lost its default", item)
		}
	}
}
