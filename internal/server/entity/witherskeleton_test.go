package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// TestAWitherSkeletonIsBornWithAStoneSwordAndHitsHard checks
// WitherSkeleton.finalizeSpawn: a stone sword and four points of attack
// damage, with no armour rolled and no bow.
func TestAWitherSkeletonIsBornWithAStoneSwordAndHitsHard(t *testing.T) {
	m := NewMob(TypeWitherSkeleton, 1, 0, 64, 0, 1)
	m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{Special: 1})
	if sword, _ := registry.ItemID("minecraft:stone_sword"); m.MainHand.ItemID != sword || m.MainHand.Count != 1 {
		t.Errorf("main hand = %+v, want a stone sword", m.MainHand)
	}
	for slot, piece := range m.Worn {
		if !piece.Empty() {
			t.Errorf("armour slot %d = %+v, want none", slot, piece)
		}
	}
	if got := m.kind().attackDamage; got != 4 {
		t.Errorf("attack damage = %v, want 4", got)
	}
}

// TestAWitherSkeletonFightsInMelee checks that the skeleton family's melee
// goal runs only without a bow, and its bow goal only with one.
func TestAWitherSkeletonFightsInMelee(t *testing.T) {
	m := NewMob(TypeWitherSkeleton, 1, 0, 64, 0, 1)
	m.TargetID = 7
	var melee *meleeAttackGoal
	var ranged *rangedAttackGoal
	for _, g := range hostileGoals(m) {
		switch g := g.(type) {
		case *meleeAttackGoal:
			melee = g
		case *rangedAttackGoal:
			ranged = g
		}
	}
	if melee == nil || ranged == nil {
		t.Fatalf("goals: melee %v, ranged %v, want both", melee != nil, ranged != nil)
	}
	for _, tc := range []struct {
		name    string
		main    registry.Item
		off     registry.Item
		wantBow bool
	}{
		{"stone sword", registry.ItemStoneSword, 0, false},
		{"empty hands", 0, 0, false},
		{"bow", registry.ItemBow, 0, true},
		{"bow in the off hand", registry.ItemStoneSword, registry.ItemBow, true},
	} {
		m.MainHand, m.OffHand = protocol.ItemStack{}, protocol.ItemStack{}
		if tc.main != 0 {
			m.MainHand = protocol.ItemStack{ItemID: int32(tc.main), Count: 1}
		}
		if tc.off != 0 {
			m.OffHand = protocol.ItemStack{ItemID: int32(tc.off), Count: 1}
		}
		if got := ranged.CanStart(nil); got != tc.wantBow {
			t.Errorf("%s: bow goal may start = %v, want %v", tc.name, got, tc.wantBow)
		}
		if got := melee.refuse(); got != tc.wantBow {
			t.Errorf("%s: melee goal refuses = %v, want %v", tc.name, got, tc.wantBow)
		}
	}
}
