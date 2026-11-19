package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// TestAZombifiedPiglinIsBornWithAGoldenWeaponAndNoReinforcements checks
// ZombifiedPiglin.populateDefaultEquipmentSlots and
// randomizeReinforcementsChance over many seeds: a golden sword or, one time
// in twenty, a golden spear; no armour; a reinforcement chance of nought.
func TestAZombifiedPiglinIsBornWithAGoldenWeaponAndNoReinforcements(t *testing.T) {
	spears := 0
	const n = 2000
	for seed := range int64(n) {
		m := NewMob(TypeZombifiedPiglin, 1, 0, 64, 0, seed)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:nether_wastes"), nil, rules.LocalDifficulty{})
		switch m.MainHand.ItemID {
		case int32(registry.ItemGoldenSword):
		case int32(registry.ItemGoldenSpear):
			spears++
		default:
			t.Fatalf("seed %d: main hand = %+v, want a golden sword or spear", seed, m.MainHand)
		}
		for slot, piece := range m.Worn {
			if !piece.Empty() {
				t.Fatalf("seed %d: armour slot %d = %+v, want none", seed, slot, piece)
			}
		}
		if got := m.ReinforcementChance(); got != 0 {
			t.Fatalf("seed %d: reinforcement chance = %v, want 0", seed, got)
		}
	}
	if spears < 50 || spears > 160 {
		t.Errorf("%d spears in %d piglins, want about one in twenty", spears, n)
	}
}
