package entity

import (
	"github.com/golem-mc/golem/registry"

	"testing"
)

// vanilla: VillagerHostilesSensor
func TestVillagerHostileSensorUsesEachKindsRange(t *testing.T) {
	if got := villagerHostileRange(registry.EntityTypeIllusioner); got != 12 {
		t.Fatalf("illusioner range %g, want 12", got)
	}
	w := flatWorld(t)
	for _, tc := range []struct {
		typ MobType
		r   float64
	}{
		{TypeDrowned, 8}, {TypeHusk, 8}, {TypeVex, 8}, {TypeZombie, 8}, {TypeZombieVillager, 8},
		{TypeVindicator, 10}, {TypeZoglin, 10}, {TypeEvoker, 12}, {TypeRavager, 12}, {TypePillager, 15},
		{TypeCow, 0}, {TypeSkeleton, 0}, {TypeWitch, 0}, {TypeZombifiedPiglin, 0},
	} {
		t.Run(tc.typ.Name(), func(t *testing.T) {
			if got := villagerHostileRange(tc.typ); got != tc.r {
				t.Fatalf("range %g, want %g", got, tc.r)
			}
			m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
			o := NewMob(tc.typ, 2, 0.5+max(tc.r, 1), 64, 0.5, 2)
			tick := &tickView{w: w, others: []*Mob{o}}
			m.villagerDangerStep(tick)
			if got := m.vb.danger.hostile; (got == o) != (tc.r > 0) {
				t.Fatalf("hostile %v, range %g", got, tc.r)
			}
			o.X += 0.01
			m.vb.danger.scan = 0
			m.villagerDangerStep(tick)
			if m.vb.danger.hostile != nil || m.vb.activity == actPanic {
				t.Fatal("outside the kind's range still panicked")
			}
		})
	}
}

// vanilla: WakeUp, VillagerPanicTrigger, SetWalkTargetAwayFrom
func TestVillagerPanicWakesStopsInteractionsAndFlees(t *testing.T) {
	w := newTestWorld()
	floor(w, -30, -30, 30, 30, 63, defaultState(t, "minecraft:stone"))
	w.set(0, 64, 0, bedHead(t))
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	m.Village = fakeVillage{w: w}
	m.GameTime, m.DayTime = 5000, 12500
	m.startSleeping(Waypoint{0, 64, 0})
	m.TakeBedOps()
	m.vb.activity = actRest
	// An interaction left running on a tick between goal selections must stop.
	show := &showTradesGoal{m: m}
	m.ai.running = append(m.ai.running, show)
	h := NewMob(TypePillager, 2, m.X+3, 64, m.Z, 2)
	tick := &tickView{w: w, others: []*Mob{h}}
	m.villagerDangerStep(tick)
	if m.Sleeping || m.vb.activity != actPanic || m.ai.isRunning(show) {
		t.Fatal("danger did not wake the villager and interrupt its interaction")
	}
	ops := m.TakeBedOps()
	if len(ops) != 1 || ops[0].Occupied {
		t.Fatalf("bed ops %v, want one freeing the bed", ops)
	}
	g := &villagerPanicGoal{m: m}
	if !g.CanStart(tick) {
		t.Fatal("panic goal did not start")
	}
	g.Start(tick)
	g.Tick(tick)
	nav := &m.navOf().nav
	if len(nav.path) == 0 || nav.speedFactor != 0.75 {
		t.Fatalf("flee path %v at pace %g", nav.path, nav.speedFactor)
	}
	end := nav.path[len(nav.path)-1]
	if Dist2(float64(end.X)+0.5, float64(end.Y), float64(end.Z)+0.5, h.X, h.Y, h.Z) <= Dist2(m.X, m.Y, m.Z, h.X, h.Y, h.Z) {
		t.Fatal("flee destination was not farther from the danger")
	}
	g.Tick(tick)
	if got := nav.path[len(nav.path)-1]; got != end {
		t.Fatal("interrupted the current flee walk")
	}
	if (&showTradesGoal{m: m}).CanStart(&tickView{w: w, players: []Player{{EntityID: 3, X: m.X, Y: m.Y, Z: m.Z}}}) {
		t.Fatal("started showing trades while panicking")
	}
}

// vanilla: VillagerPanicTrigger.tick
func TestAHostileMakesThreeRestedVillagersRequestAGolem(t *testing.T) {
	w := flatWorld(t)
	var herd []*Mob
	for i := range 3 {
		m := NewMob(TypeVillager, int32(i+1), float64(i)+0.5, 64, 0.5, int64(i+1))
		m.Village = fakeVillage{w: w}
		m.GameTime, m.DayTime, m.vb.lastSlept = 5000, 2000, 4000
		herd = append(herd, m)
	}
	herd = append(herd, NewMob(TypePillager, 4, 3.5, 64, 3.5, 4))
	tick := &tickView{w: w, others: herd[1:]}
	herd[0].villagerBrainStep(tick)
	asked := false
	for _, ev := range tick.events {
		asked = asked || ev.Kind == MobWantsGolem
	}
	if !asked || herd[0].vb.activity != actPanic {
		t.Fatal("a hostile did not trigger the panic golem request before any goal ran")
	}
	herd[0].GolemDetected(5000)
	tick.events = nil
	herd[0].villagerBrainStep(tick)
	for _, ev := range tick.events {
		if ev.Kind == MobWantsGolem {
			t.Fatal("asked again with a golem-seen memory")
		}
	}
}
