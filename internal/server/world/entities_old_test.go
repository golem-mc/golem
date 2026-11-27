package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestUpgradeEntitiesAppliesTheEntityFixesOf1_16To1_20 feeds each entity of
// the shape before a data fixer through upgradeEntities.
// vanilla: AbstractArrowPickupFix, EntityVariantFix, VariantRenameFix,
// EntityFieldsRenameFix, EntityGoatMissingStateFix, EffectDurationFix,
// MobEffectIdFix, FixProjectileStoredItem
func TestUpgradeEntitiesAppliesTheEntityFixesOf1_16To1_20(t *testing.T) {
	str := func(s string) nbt.Tag { return nbt.String(s) }
	list := func(items ...nbt.Tag) nbt.List { return nbt.List{Elem: nbt.TypeCompound, Items: items} }
	for _, tc := range []struct {
		name    string
		version int32
		in      nbt.Compound
		want    nbt.Compound
	}{
		{"arrow from a player", 2586, nbt.Compound{"id": str("minecraft:arrow")},
			nbt.Compound{"id": str("minecraft:arrow"), "pickup": nbt.Byte(1), "item": nbt.Compound{"id": str("minecraft:arrow"), "count": nbt.Int(1)}}},
		{"trident not from a player", 2586, nbt.Compound{"id": str("minecraft:trident"), "player": nbt.Byte(0)},
			nbt.Compound{"id": str("minecraft:trident"), "pickup": nbt.Byte(0)}},
		{"tipped arrow", 3600, nbt.Compound{"id": str("minecraft:arrow"), "pickup": nbt.Byte(1), "Potion": str("minecraft:poison")},
			nbt.Compound{"id": str("minecraft:arrow"), "pickup": nbt.Byte(1), "Potion": str("minecraft:poison"),
				"item": nbt.Compound{"id": str("minecraft:tipped_arrow"), "count": nbt.Int(1)}}},
		{"cat", 2586, nbt.Compound{"id": str("minecraft:cat"), "CatType": nbt.Int(4)},
			nbt.Compound{"id": str("minecraft:cat"), "variant": str("minecraft:british_shorthair")}},
		{"cat between shorthair and variants", 3090, nbt.Compound{"id": str("minecraft:cat"), "variant": str("minecraft:british")},
			nbt.Compound{"id": str("minecraft:cat"), "variant": str("minecraft:british_shorthair")}},
		{"cat unknown type", 3000, nbt.Compound{"id": str("minecraft:cat"), "CatType": nbt.Int(99)},
			nbt.Compound{"id": str("minecraft:cat"), "variant": str("minecraft:tabby")}},
		{"frog", 3086, nbt.Compound{"id": str("minecraft:frog"), "Variant": nbt.Int(2)},
			nbt.Compound{"id": str("minecraft:frog"), "variant": str("minecraft:cold")}},
		{"painting", 3000, nbt.Compound{"id": str("minecraft:painting"), "Motive": str("minecraft:kebab"), "Facing": nbt.Byte(2)},
			nbt.Compound{"id": str("minecraft:painting"), "variant": str("minecraft:kebab"), "facing": nbt.Byte(2)}},
		{"goat", 3092, nbt.Compound{"id": str("minecraft:goat")},
			nbt.Compound{"id": str("minecraft:goat"), "HasLeftHorn": nbt.Byte(1), "HasRightHorn": nbt.Byte(1)}},
		{"active effects", 3000, nbt.Compound{"id": str("minecraft:zombie"), "ActiveEffects": list(
			nbt.Compound{"Id": nbt.Byte(1), "Amplifier": nbt.Byte(2), "Duration": nbt.Int(100), "ShowParticles": nbt.Byte(1)})},
			nbt.Compound{"id": str("minecraft:zombie"), "active_effects": list(
				nbt.Compound{"id": str("minecraft:speed"), "amplifier": nbt.Byte(2), "duration": nbt.Int(100), "show_particles": nbt.Byte(1)})}},
		{"effect ticks", 3300, nbt.Compound{"id": str("minecraft:zombie"), "ActiveEffects": list(
			nbt.Compound{"Id": nbt.Byte(1), "Duration": nbt.Int(100), "FactorCalculationData": nbt.Compound{"effect_changed_timestamp": nbt.Int(130)}})},
			nbt.Compound{"id": str("minecraft:zombie"), "active_effects": list(
				nbt.Compound{"id": str("minecraft:speed"), "duration": nbt.Int(100), "FactorCalculationData": nbt.Compound{"ticks_active": nbt.Int(30)}})}},
		{"area effect cloud", 3000, nbt.Compound{"id": str("minecraft:area_effect_cloud"), "Effects": list(nbt.Compound{"Id": nbt.Byte(19)})},
			nbt.Compound{"id": str("minecraft:area_effect_cloud"), "potion_duration_scale": nbt.Float(0.25),
				"potion_contents": nbt.Compound{"custom_effects": list(nbt.Compound{"id": str("minecraft:poison")})}}},
		{"mooshroom", 3000, nbt.Compound{"id": str("minecraft:mooshroom"), "EffectId": nbt.Byte(16), "EffectDuration": nbt.Int(160)},
			nbt.Compound{"id": str("minecraft:mooshroom"), "stew_effects": list(nbt.Compound{"id": str("minecraft:night_vision"), "duration": nbt.Int(160)})}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{tc.in}}
			upgradeEntities(l, tc.version)
			got, _ := nbt.CompoundView(l.Items[0])
			if !tagEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
