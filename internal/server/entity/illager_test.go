package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

// TestAVindicatorDropsEmeraldsOnlyToAPlayerKill: the vindicator's table rolls
// an emerald for a kill a player is credited with and nothing otherwise.
// vanilla: entities/vindicator loot table
func TestAVindicatorDropsEmeraldsOnlyToAPlayerKill(t *testing.T) {
	table := rules.EntityLootTable(TypeVindicator.Name())
	rng := rand.New(rand.NewSource(1))
	byPlayer, other := 0, 0
	for range 400 {
		byPlayer += len(rules.RollLoot(table, &rules.LootContext{KilledByPlayer: true}, rng))
		other += len(rules.RollLoot(table, &rules.LootContext{}, rng))
	}
	if byPlayer == 0 || other != 0 {
		t.Errorf("drops: %d by a player over 400 kills, %d otherwise; want some, then none", byPlayer, other)
	}
}

// TestAnIllagerIdlesTwoTicksFasterInTheDark: Raider.updateNoActionTime adds 2
// in any light, where a plain monster adds them only in bright light.
func TestAnIllagerIdlesTwoTicksFasterInTheDark(t *testing.T) {
	for _, tc := range []struct {
		kind MobType
		want int
	}{{TypeVindicator, 3}, {TypeZombie, 1}} {
		w := newTestWorld()
		w.sky = 0
		floor64(w, t)
		m := NewMob(tc.kind, 1, 0.5, 64, 0.5, 3)
		before := m.NoActionTicks
		m.idleStep(w)
		if got := m.NoActionTicks - before; got != tc.want {
			t.Errorf("%s: idleness grew by %d in the dark, want %d", tc.kind, got, tc.want)
		}
	}
}

// TestAVindicatorIgnoresAWitchButNotAZombie: HurtByTargetGoal(Raider.class)
// spares every raider, the witch among them.
// vanilla: Vindicator.registerGoals
func TestAVindicatorIgnoresAWitchButNotAZombie(t *testing.T) {
	for _, tc := range []struct {
		kind MobType
		want bool
	}{{TypeWitch, false}, {TypeZombie, true}} {
		w := newTestWorld()
		floor64(w, t)
		v := NewMob(TypeVindicator, 1, 0.5, 64, 0.5, 3)
		o := NewMob(tc.kind, 2, 1.5, 64, 0.5, 4)
		v.retaliate = o.ID
		view := &tickView{w: w, others: []*Mob{v, o}}
		var goal *hurtByGoal
		for _, g := range vindicatorGoals(v) {
			if h, ok := g.(*hurtByGoal); ok {
				goal = h
			}
		}
		if goal == nil {
			t.Fatal("vindicatorGoals has no hurt-by goal")
		}
		if got := goal.CanStart(view); got != tc.want {
			t.Errorf("%s: retaliation = %v, want %v", tc.kind, got, tc.want)
		}
	}
}

// TestAVindicatorGlancesAtANearbyMob: the priority-10 LookAtPlayerGoal(Mob, 8).
func TestAVindicatorGlancesAtANearbyMob(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	v := NewMob(TypeVindicator, 1, 0.5, 64, 0.5, 3)
	o := NewMob(TypeSheep, 2, 4.5, 64, 0.5, 4)
	view := &tickView{w: w, others: []*Mob{v, o}}
	var goal *lookAtMob
	for _, g := range vindicatorGoals(v) {
		if l, ok := g.(*lookAtMob); ok {
			goal = l
		}
	}
	if goal == nil {
		t.Fatal("vindicatorGoals has no look-at-mob goal")
	}
	started := false
	for i := 0; i < 2000 && !started; i++ {
		started = goal.CanStart(view)
	}
	if !started || goal.target != o.ID {
		t.Fatalf("no glance at the sheep: started=%v target=%d", started, goal.target)
	}
	goal.Start(view)
	goal.Tick(view)
	if v.HeadYaw == 0 {
		t.Error("the head did not turn toward the sheep")
	}
}
