package entity

import (
	"github.com/golem-mc/golem/registry"

	"testing"

	"github.com/golem-mc/golem/internal/server/effect"
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

// vanilla: NearestVisibleLivingEntitySensor, Sensor
func TestVillagerHostileSensorKeepsTheNearestVisibleLivingDanger(t *testing.T) {
	w := flatWorld(t)
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	near := NewMob(TypeZombie, 2, 4.5, 64, 0.5, 2)
	far := NewMob(TypePillager, 3, 0.5, 64, 8.5, 3)
	tick := &tickView{w: w, others: []*Mob{far, near}}
	m.villagerDangerStep(tick)
	if m.vb.danger.hostile != near {
		t.Fatal("did not pick the nearest danger")
	}
	for y := int32(64); y <= 67; y++ {
		w.set(2, y, 0, defaultState(t, "minecraft:stone"))
	}
	m.vb.danger.scan = 0
	m.villagerDangerStep(tick)
	if m.vb.danger.hostile != far {
		t.Fatal("saw a danger through a wall")
	}
	far.Health = 0
	m.villagerDangerStep(tick)
	if m.vb.danger.hostile != nil {
		t.Fatal("kept a dead danger")
	}
	near.AddEffect(effect.New(mobEffectInvisibility, 100, 0, false, false, false))
	for y := int32(64); y <= 67; y++ {
		w.set(2, y, 0, 0)
	}
	m.vb.danger.scan = 0
	m.villagerDangerStep(tick)
	if m.vb.danger.hostile != nil {
		t.Fatal("saw a naked invisible danger four blocks away")
	}
	near.X = 2.5
	m.vb.danger.scan = 0
	m.villagerDangerStep(tick)
	if m.vb.danger.hostile != near {
		t.Fatal("missed an invisible danger within the two-block floor")
	}
}

// vanilla: HurtBySensor, VillagerPanicTrigger, VillagerCalmDown
func TestVillagerPanicKeepsACloseAttackerAfterTheHurtExpires(t *testing.T) {
	w := flatWorld(t)
	for _, player := range []bool{false, true} {
		m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
		m.GameTime, m.DayTime = 200, 12500
		tick := &tickView{w: w}
		if player {
			tick.players = []Player{{EntityID: 2, X: 3.5, Y: 64, Z: 0.5}}
		} else {
			tick.others = []*Mob{NewMob(TypeCow, 2, 3.5, 64, 0.5, 2)}
		}
		m.vb.lastSlept = 100
		for i := range 2 {
			o := NewMob(TypeVillager, int32(i+3), float64(i)+0.5, 64, 0.5, int64(i+3))
			o.GameTime, o.vb.lastSlept = m.GameTime, 100
			tick.others = append(tick.others, o)
		}
		m.noteLastDamage(registry.DamageTypeMobAttack)
		m.blowAttacker = 2
		m.villagerDangerStep(tick)
		if m.vb.activity != actPanic {
			t.Fatal("hurt did not trigger panic")
		}
		m.updateActivity()
		if m.vb.activity != actPanic {
			t.Fatal("schedule overrode panic")
		}
		m.golemStep(tick)
		if len(tick.events) != 1 || tick.events[0].Kind != MobWantsGolem {
			t.Fatal("hurt villager did not request a golem")
		}
		tick.events = nil
		m.Age += lastDamageExpiry + 1
		m.vb.danger.scan = 0
		m.villagerDangerStep(tick)
		if m.vb.activity != actPanic {
			t.Fatal("forgot a close attacker when the hurt expired")
		}
		if m.panicking() {
			t.Fatal("panic activity counted as PathfinderMob.isPanicking")
		}
		m.golemStep(tick)
		if len(tick.events) != 0 {
			t.Fatal("requested a golem after hurt expired with only a close attacker")
		}
		if player {
			tick.players[0].X = 6.51
		} else {
			tick.others[0].X = 6.51
		}
		m.villagerDangerStep(tick)
		if m.vb.activity != actRest || m.vb.danger.attacker != 0 {
			t.Fatal("did not calm and return to the schedule beyond six blocks")
		}
	}
}

func TestVillagerPanicDropsDeadOrAbsentAttackers(t *testing.T) {
	w := flatWorld(t)
	for _, tc := range []struct {
		name string
		p    []Player
		o    []*Mob
	}{
		{name: "absent"},
		{name: "dead player", p: []Player{{EntityID: 2, Dead: true}}},
		{name: "dead mob", o: []*Mob{{ID: 2, Type: TypeCow}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
			m.GameTime, m.DayTime = 200, 1000
			m.vb.activity, m.vb.danger.attacker = actPanic, 2
			m.villagerDangerStep(&tickView{w: w, players: tc.p, others: tc.o})
			if m.vb.activity != actIdle || m.vb.danger.attacker != 0 {
				t.Fatal("kept panicking over a dead or absent attacker")
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

// vanilla: SetWalkTargetAwayFrom uses land positions, never water.
func TestVillagerLandFleeRejectsWater(t *testing.T) {
	w := newTestWorld()
	floor(w, -30, -30, 30, 30, 63, defaultState(t, "minecraft:stone"))
	water := defaultState(t, "minecraft:water")
	floor(w, -30, -30, 30, 30, 64, water)
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	for range 20 {
		if p, ok := landRandomPosAway(w, m, 3, 0, 16, 7); ok {
			t.Fatalf("land flee picked water at %v", p)
		}
	}
}

// vanilla: HurtBySensor, VillagerPanicTrigger.isHurt
func TestVillagerPanicFromEnvironmentalDamageExpires(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	m.GameTime, m.DayTime = 200, 1000
	m.noteLastDamage(registry.DamageTypeFall)
	tick := &tickView{w: flatWorld(t)}
	m.villagerDangerStep(tick)
	if m.vb.activity != actPanic || m.vb.danger.attacker != 0 {
		t.Fatal("environmental hurt did not trigger panic without an attacker")
	}
	m.Age = lastDamageExpiry + 1
	m.vb.danger.scan = 0
	m.villagerDangerStep(tick)
	if m.vb.activity != actIdle {
		t.Fatal("kept panicking after environmental hurt expired")
	}
}

func TestANoAIVillagerDoesNotSenseHostiles(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	m.NoAI = true
	m.tick(flatWorld(t), nil, []*Mob{NewMob(TypePillager, 2, 3.5, 64, 0.5, 2)})
	if m.vb.activity == actPanic || m.vb.danger.hostile != nil {
		t.Fatal("NoAI villager sensed the hostile")
	}
}
