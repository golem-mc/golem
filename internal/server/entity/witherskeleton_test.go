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

// TestAWitherSkeletonGivesWitherFor200Ticks checks
// WitherSkeleton.doHurtTarget at every difficulty.
func TestAWitherSkeletonGivesWitherFor200Ticks(t *testing.T) {
	m := NewMob(TypeWitherSkeleton, 1, 0, 64, 0, 1)
	for difficulty := range uint8(4) {
		name, ticks, ok := m.BlowEffect(difficulty, 0)
		if !ok || name != "minecraft:wither" || ticks != 200 {
			t.Errorf("difficulty %d: %s for %d ticks (%v), want wither for 200", difficulty, name, ticks, ok)
		}
	}
}

// TestAWitherSkeletonIsTallAndFireImmune checks its 0.7 by 2.4 box and the
// fireImmune of its builder.
func TestAWitherSkeletonIsTallAndFireImmune(t *testing.T) {
	m := NewMob(TypeWitherSkeleton, 1, 0, 64, 0, 1)
	if w, h := KindDims(TypeWitherSkeleton); w != 0.7 || h != 2.4 {
		t.Errorf("box %v by %v, want 0.7 by 2.4", w, h)
	}
	m.IgniteForTicks(100)
	if m.OnFire() {
		t.Error("a wither skeleton burns")
	}
}

// TestAWitherSkeletonDropsCoalAndBones checks the loot table: bones and
// coal, over many deaths.
func TestAWitherSkeletonDropsCoalAndBones(t *testing.T) {
	seen := map[string]bool{}
	for seed := range int64(200) {
		m := NewMob(TypeWitherSkeleton, 1, 0, 64, 0, seed)
		for _, s := range m.RollDrops(rules.LootKiller{}, nil) {
			name, _ := registry.ItemName(s.ItemID)
			seen[name] = true
		}
	}
	for _, want := range []string{"minecraft:bone", "minecraft:coal"} {
		if !seen[want] {
			t.Errorf("200 deaths never dropped %s (saw %v)", want, seen)
		}
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

// TestAWitherSkeletonKeepsItsArmsUpForTheWholeHunt checks that the skeleton
// family raises its arms at the goal's start and not by the zombie's clock.
func TestAWitherSkeletonKeepsItsArmsUpForTheWholeHunt(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	players := []Player{survivor(7, 6.5)}
	for _, tc := range []struct {
		typ  MobType
		want bool
	}{{TypeWitherSkeleton, true}, {TypeZombie, false}} {
		m := NewMob(tc.typ, 1, 0, 64, 0, 1)
		m.TargetID = 7
		g := &meleeAttackGoal{m: m, priority: priorityMelee}
		g.Start(&tickView{w: w, players: players})
		g.Tick(&tickView{w: w, players: players})
		if m.Aggressive != tc.want {
			t.Errorf("%s: aggressive after one tick = %v, want %v", tc.typ, m.Aggressive, tc.want)
		}
	}
}

// TestAWitherSkeletonRefusesBowsAndCrossbows checks
// WitherSkeleton.canHoldItem, and that a plain skeleton still takes a bow.
func TestAWitherSkeletonRefusesBowsAndCrossbows(t *testing.T) {
	bow, _ := registry.ItemID("minecraft:bow")
	crossbow, _ := registry.ItemID("minecraft:crossbow")
	wither := NewMob(TypeWitherSkeleton, 1, 0, 64, 0, 1)
	plain := NewMob(TypeSkeleton, 2, 0, 64, 0, 1)
	for _, id := range []int32{bow, crossbow} {
		stack := protocol.ItemStack{ItemID: id, Count: 1}
		if wither.wantsToPickUp(stack) {
			t.Errorf("a wither skeleton wants item %d", id)
		}
	}
	if !plain.wantsToPickUp(protocol.ItemStack{ItemID: bow, Count: 1}) {
		t.Error("a skeleton refuses a bow")
	}
	if wither.preferredWeaponTag() != "" {
		t.Errorf("preferred weapon tag = %q, want none", wither.preferredWeaponTag())
	}
}
