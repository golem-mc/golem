package rules_test

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
	"math/rand"
	"testing"
)

// vanilla: EnchantmentHelper's item and entity value component queries.
func TestPackItemValueEffectsUseComponentsAndContexts(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackEnchantments(nil) })
	def, dropped, err := datapack.DecodeEnchantment("minecraft:sharpness", []byte(`{
      "anvil_cost":1,"description":{"text":"Values"},"max_level":3,"weight":1,
      "min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":50,"per_level_above_first":0},
      "slots":["mainhand"],"supported_items":"#minecraft:enchantable/durability",
      "effects":{
        "minecraft:fishing_time_reduction":[{"effect":{"type":"minecraft:add","value":1.375}}],
        "minecraft:fishing_luck_bonus":[{"effect":{"type":"minecraft:add","value":2.75}}],
        "minecraft:projectile_count":[{"effect":{"type":"minecraft:add","value":{"type":"minecraft:linear","base":2.5,"per_level_above_first":2}},"requirements":{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:player"}}}],
        "minecraft:projectile_spread":[{"effect":{"type":"minecraft:add","value":12.5},"requirements":{"condition":"minecraft:random_chance","chance":{"type":"minecraft:enchantment_level","amount":{"type":"minecraft:linear","base":0,"per_level_above_first":1}}}}],
        "minecraft:projectile_piercing":[{"effect":{"type":"minecraft:add","value":3.75},"requirements":{"condition":"minecraft:match_tool","predicate":{"items":"minecraft:arrow"}}}],
        "minecraft:trident_return_acceleration":[{"effect":{"type":"minecraft:add","value":4.75},"requirements":{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:trident"}}}],
        "minecraft:crossbow_charge_time":{"type":"minecraft:all_of","effects":[{"type":"minecraft:multiply","factor":0.5},{"type":"minecraft:add","value":0.125}]},
        "minecraft:trident_spin_attack_strength":{"type":"minecraft:add","value":3.25}
      }} `))
	if err != nil || len(dropped) != 0 {
		t.Fatalf("decode: %v, dropped %v", err, dropped)
	}
	registry.ApplyPackEnchantments(map[string]registry.EnchantmentDef{"minecraft:sharpness": def})
	id, _ := registry.EnchantmentID("minecraft:sharpness")
	stack := protocol.ItemStack{ItemID: int32(registry.ItemCrossbow), Count: 1, Enchantments: map[int32]int32{id: 2}}
	ammo := protocol.ItemStack{ItemID: int32(registry.ItemArrow), Count: 1}
	user := &rules.EnchContext{Victim: protocol.EntityTypePlayer, Attacker: -1, DirectAttacker: -1, Rand: rand.New(rand.NewSource(1))}
	for _, tc := range []struct {
		name      string
		got, want float64
	}{
		{"fishing seconds to ticks", float64(rules.FishingLureTicks(stack, user)), 27},
		{"luck truncation", float64(rules.FishingLuck(stack, user)), 2},
		{"projectile count base and level", float64(rules.CrossbowProjectileCount(stack, user)), 5},
		{"missing user rejects condition", float64(rules.CrossbowProjectileCount(stack)), 1},
		{"spread chance reads enchantment level", float64(rules.CrossbowMaxSpread(stack, user)), 12.5},
		{"piercing sees ammo", float64(rules.PiercingLevel(stack, ammo)), 3},
		{"piercing refuses weapon as tool", float64(rules.PiercingLevel(stack, stack)), 0},
		{"return sees trident", float64(rules.TridentReturnAcceleration(stack, &rules.EnchContext{Victim: protocol.EntityTypeTrident})), 4},
		{"return refuses player", float64(rules.TridentReturnAcceleration(stack, user)), 0},
		{"single charge all_of", float64(rules.CrossbowChargeTicks(stack)), 15},
		{"single spin", rules.SpinAttackStrength(stack), 3.25},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	negative := registry.EnchEffect{Kind: registry.EnchSet, Value: &registry.EnchLevelValue{Kind: registry.EnchValueConstant, Constant: -2}}
	list := []registry.EnchConditional{{Effect: negative}}
	def.Effects = registry.EnchantmentEffects{FishingTimeReduction: list, FishingLuckBonus: list, ProjectileCount: list, ProjectileSpread: list, ProjectilePiercing: list, TridentReturnAcceleration: list, CrossbowChargeTime: &negative, TridentSpinAttackStrength: &negative}
	registry.ApplyPackEnchantments(map[string]registry.EnchantmentDef{"minecraft:sharpness": def})
	if rules.FishingLureTicks(stack) != 0 || rules.FishingLuck(stack) != 0 || rules.CrossbowProjectileCount(stack) != 0 || rules.CrossbowMaxSpread(stack) != 0 || rules.PiercingLevel(stack, ammo) != 0 || rules.TridentReturnAcceleration(stack) != 0 || rules.CrossbowChargeTicks(stack) != 0 {
		t.Error("negative component values must clamp to zero")
	}
	if got := rules.SpinAttackStrength(stack); got != -2 {
		t.Errorf("spin strength = %v, want unclamped -2", got)
	}
}
