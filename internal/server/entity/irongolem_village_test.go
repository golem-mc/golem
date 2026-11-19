package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"testing"
)

func TestGolemApproachUsesALiveTargetWithin32Blocks(t *testing.T) {
	w := newTestWorld()
	floor(w, -32, -32, 48, 32, 63, defaultState(t, "minecraft:stone"))
	m := NewMob(TypeIronGolem, 1, 0.5, 64, 0.5, 2162)
	target := NewMob(TypeZombie, 2, 20.5, 64, 0.5, 1)
	m.TargetID = target.ID
	tick := &tickView{w: w, others: []*Mob{target}}
	g := &golemApproachGoal{m: m, priority: 2, pace: 0.9}
	found := false
	for range 100 {
		if g.CanStart(tick) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no approach destination for target within 32 blocks")
	}
	g.Start(tick)
	if !g.ShouldContinue(tick) {
		t.Fatal("approach did not start navigating")
	}
	target.X = 33
	if g.CanStart(tick) || g.ShouldContinue(tick) {
		t.Fatal("approaching a target beyond 32 blocks")
	}
	target.X = 20.5
	target.Dead = true
	if g.ShouldContinue(tick) {
		t.Fatal("approaching a dead target")
	}
}

func TestGolemFlowerConditionsAndInterruption(t *testing.T) {
	w := newTestWorld()
	floor(w, -8, -8, 8, 8, 63, defaultState(t, "minecraft:stone"))
	m := NewMob(TypeIronGolem, 1, 0.5, 64, 0.5, 2162)
	v := NewMob(TypeVillager, 2, 2.5, 64, 0.5, 1)
	farther := NewMob(TypeVillager, 3, 5.5, 64, 0.5, 1)
	tick := &tickView{w: w, others: []*Mob{farther, v}}
	g := &offerFlowerGoal{m: m}
	m.ForceGolemFlowerOffer()
	if g.CanStart(tick) {
		t.Fatal("offering a flower at night")
	}
	w.bright = true
	tick.w = w
	if !g.CanStart(tick) || g.target != v {
		t.Fatal("did not choose the nearest visible villager")
	}
	g.Start(tick)
	if m.OfferFlowerTicks != 400 || tick.events[0].Status != protocol.EntityStatusIronGolemOfferFlower {
		t.Fatal("missing flower start")
	}
	g.Tick(tick)
	if m.HeadYaw != -30 {
		t.Fatalf("flower head turn = %g, want -30 degrees", m.HeadYaw)
	}
	g.Stop(tick)
	if m.OfferFlowerTicks != 0 || tick.events[1].Status != protocol.EntityStatusIronGolemStopFlower {
		t.Fatal("interruption did not clear flower")
	}
	v.Dead, farther.Dead = true, true
	m.ForceGolemFlowerOffer()
	if g.CanStart(tick) {
		t.Fatal("offering to a dead villager")
	}
	v.Dead = false
	v.Y = 69
	m.ForceGolemFlowerOffer()
	if g.CanStart(tick) {
		t.Fatal("offering outside the vertical box")
	}
}

func TestGolemVillageStrollIgnoresIdleCutoff(t *testing.T) {
	w := newTestWorld()
	floor(w, -20, -20, 20, 20, 63, defaultState(t, "minecraft:stone"))
	m := NewMob(TypeIronGolem, 1, 0.5, 64, 0.5, 2162)
	m.NoActionTicks = 1000
	tick := &tickView{w: w}
	var stroll Goal
	for _, g := range ironGolemGoals(m) {
		if g.Priority() == 4 && g.Flags() == GoalMove {
			stroll = g
		}
	}
	if stroll == nil {
		t.Fatal("missing village stroll")
	}
	for range 1000 {
		if stroll.CanStart(tick) {
			return
		}
	}
	t.Fatal("idle golem never strolls")
}

func TestGolemReturnUsesOccupiedVillageSections(t *testing.T) {
	w := newTestWorld()
	floor(w, -16, -16, 64, 16, 63, defaultState(t, "minecraft:stone"))
	m := NewMob(TypeIronGolem, 1, 43.5, 64, 0.5, 2162)
	tick := &tickView{w: w}
	m.Village = fakeVillage{w: w}
	if _, ok := m.golemReturnPos(tick); ok {
		t.Fatal("returned toward a missing village")
	}
	m.Village = fakeVillage{w: w, pois: []Waypoint{{3, 64, 0}}}
	found := false
	for range 100 {
		if p, ok := m.golemReturnPos(tick); ok && p.X < 43 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no return destination toward the occupied village")
	}
	// VillageIndex treats the entire adjacent section as village.
	m.X = 20.5
	for _, g := range ironGolemGoals(m) {
		if g.Priority() == 2 && g.Flags() == GoalMove {
			if stroll, ok := g.(*randomStroll); ok && !stroll.blocked() {
				t.Fatal("return goal ran inside the village")
			}
		}
	}
}
