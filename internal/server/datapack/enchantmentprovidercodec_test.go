package datapack

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestDecodeEnchantmentProvider(t *testing.T) {
	p, err := DecodeEnchantmentProvider([]byte(`{"type":"minecraft:single","enchantment":"sharpness","level":{"type":"minecraft:uniform","min_inclusive":2,"max_inclusive":4}}`))
	if err != nil || p.Kind != registry.EnchProviderSingle || p.Names[0] != "minecraft:sharpness" || p.Level != (registry.IntRange{Min: 2, Max: 4}) {
		t.Errorf("single = %+v, %v", p, err)
	}
	p, err = DecodeEnchantmentProvider([]byte(`{"type":"minecraft:by_cost_with_difficulty","enchantments":"#minecraft:on_mob_spawn_equipment","min_cost":5,"max_cost_span":17}`))
	if err != nil || p.Tag != "minecraft:on_mob_spawn_equipment" || p.MinCost != 5 || p.MaxCostSpan != 17 {
		t.Errorf("by_cost_with_difficulty = %+v, %v", p, err)
	}
	p, err = DecodeEnchantmentProvider([]byte(`{"type":"minecraft:by_cost","enchantments":["minecraft:sharpness"],"cost":30}`))
	if err != nil || p.Names[0] != "minecraft:sharpness" || p.Cost != (registry.IntRange{Min: 30, Max: 30}) {
		t.Errorf("by_cost = %+v, %v", p, err)
	}
	for _, bad := range []string{`{"type":"minecraft:nope"}`, `{"type":"minecraft:single","level":1}`, `{"type":"minecraft:by_cost_with_difficulty","enchantments":"#x","min_cost":0,"max_cost_span":1}`} {
		if _, err := DecodeEnchantmentProvider([]byte(bad)); err == nil {
			t.Errorf("%s decoded", bad)
		}
	}
}
