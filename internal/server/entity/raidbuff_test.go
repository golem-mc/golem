package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestRaidBuffsEnchantTheWeapon(t *testing.T) {
	t.Parallel()
	quick, _ := registry.EnchantmentID("minecraft:quick_charge")
	sharp, _ := registry.EnchantmentID("minecraft:sharpness")
	tests := []struct {
		name  string
		kind  MobType
		wave  int
		odds  float32
		item  int32
		ench  int32
		level int32
	}{
		{"pillager early wave keeps its crossbow", TypePillager, 3, 2, 0, 0, 0},
		{"pillager past easy", TypePillager, 4, 2, int32(registry.ItemCrossbow), quick, 1},
		{"pillager past normal", TypePillager, 6, 2, int32(registry.ItemCrossbow), quick, 2},
		{"pillager not enchanted", TypePillager, 6, -1, 0, 0, 0},
		{"vindicator first wave", TypeVindicator, 1, 2, int32(registry.ItemIronAxe), sharp, 1},
		{"vindicator past normal", TypeVindicator, 6, 2, int32(registry.ItemIronAxe), sharp, 2},
		{"vindicator not enchanted", TypeVindicator, 6, -1, int32(registry.ItemIronAxe), 0, 0},
	}
	for _, tt := range tests {
		m := NewMob(tt.kind, 1, 0, 0, 0, 1)
		before := m.MainHand
		m.ApplyRaidBuffs(tt.wave, 3, 5, tt.odds)
		if tt.item == 0 {
			if m.MainHand.ItemID != before.ItemID || len(m.MainHand.Enchantments) != 0 {
				t.Errorf("%s: main hand changed to %+v", tt.name, m.MainHand)
			}
			continue
		}
		if m.MainHand.ItemID != tt.item || m.MainHand.Enchantments[tt.ench] != tt.level {
			t.Errorf("%s: main hand = %+v, want item %d level %d", tt.name, m.MainHand, tt.item, tt.level)
		}
	}
}
