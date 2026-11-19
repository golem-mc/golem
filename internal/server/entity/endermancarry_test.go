package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The enderman's carried block, persistent anger and endermite hunt.

// carryWorld is the test world with the two reads the block goals make,
// air and mob_griefing, answered truthfully, and rules a test can turn off.
type carryWorld struct {
	testWorld
	noGriefing bool
	off        map[world.GameRuleIndex]bool
}

func (w carryWorld) IsAirAt(x, y, z int32) bool { return w.BlockAt(x, y, z) == 0 }
func (w carryWorld) MobGriefing() bool          { return !w.noGriefing }
func (w carryWorld) BoolRule(id world.GameRuleIndex) bool {
	if id == world.RuleUniversalAnger {
		return !w.off[id]
	}
	return !w.off[id] && w.testWorld.BoolRule(id)
}

func newCarryWorld(t testing.TB) carryWorld {
	w := newTestWorld()
	wideFloor(w, t)
	return carryWorld{testWorld: w, off: map[world.GameRuleIndex]bool{}}
}

// tickUntil ticks the mob until stop answers true, reporting whether it did
// within the ticks given.
func tickUntil(m *Mob, w World, players []Player, ticks int, stop func() bool) bool {
	for range ticks {
		m.Tick(w, players)
		if stop() {
			return true
		}
	}
	return false
}

func eventOf(events []MobEvent, kind MobEventKind) (MobEvent, bool) {
	for _, ev := range events {
		if ev.Kind == kind {
			return ev, true
		}
	}
	return MobEvent{}, false
}

// takeTries drives the take goal's tick directly, as often as the dice take.
func takeTries(m *Mob, w World, tries int) (MobEvent, bool) {
	g := &endermanTakeBlockGoal{m: m}
	for range tries {
		view := &tickView{w: w}
		g.Tick(view)
		if ev, ok := eventOf(view.events, MobTookBlock); ok {
			return ev, true
		}
	}
	return MobEvent{}, false
}

func leaveTries(m *Mob, w World, tries int) (MobEvent, bool) {
	g := &endermanLeaveBlockGoal{m: m}
	for range tries {
		view := &tickView{w: w}
		g.Tick(view)
		if ev, ok := eventOf(view.events, MobPlacedBlock); ok {
			return ev, true
		}
	}
	return MobEvent{}, false
}

func TestAnEndermanTakesAHoldableBlockBesideIt(t *testing.T) {
	w := newCarryWorld(t)
	dirt := defaultState(t, "minecraft:dirt")
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			if x != 0 || z != 0 {
				w.set(x, 64, z, dirt)
			}
		}
	}
	m := newEnderman()
	ev, ok := takeTries(m, w, 500)
	if !ok {
		t.Fatal("an enderman among dirt blocks never took one")
	}
	if got := m.CarriedBlock(); got != dirt {
		t.Errorf("CarriedBlock = %d, want the dirt state %d", got, dirt)
	}
	if w.BlockAt(int32(ev.X), int32(ev.Y), int32(ev.Z)) != dirt || ev.BlockState != dirt {
		t.Errorf("the take event names %v,%v,%v state %d, not a dirt block", ev.X, ev.Y, ev.Z, ev.BlockState)
	}
}

func TestAnEndermanGriefsOnlyWhileMobGriefingAllows(t *testing.T) {
	w := newCarryWorld(t)
	w.noGriefing = true
	m := newEnderman()
	take := &endermanTakeBlockGoal{m: m}
	for range 1000 {
		if take.CanStart(&tickView{w: w}) {
			t.Fatal("the take goal started with mob_griefing off")
		}
	}
	m.SetCarriedBlock(defaultState(t, "minecraft:dirt"))
	leave := &endermanLeaveBlockGoal{m: m}
	for range 20000 {
		if leave.CanStart(&tickView{w: w}) {
			t.Fatal("the leave goal started with mob_griefing off")
		}
	}
	w.noGriefing = false
	started := false
	for range 20000 {
		started = started || leave.CanStart(&tickView{w: w})
	}
	if !started {
		t.Fatal("the leave goal never started with mob_griefing on")
	}
}

func TestAnEndermanSetsItsBlockDownOnAFullBlock(t *testing.T) {
	w := newCarryWorld(t)
	dirt := defaultState(t, "minecraft:dirt")
	m := newEnderman()
	m.SetCarriedBlock(dirt)
	ev, ok := leaveTries(m, w, 500)
	if !ok {
		t.Fatal("an enderman over a stone floor never set its block down")
	}
	if ev.BlockState != dirt || ev.Y != 64 {
		t.Errorf("placed state %d at y=%v, want dirt at y=64", ev.BlockState, ev.Y)
	}
	if m.CarriedBlock() != 0 {
		t.Error("the enderman still carries the block it set down")
	}
}

func TestAnEndermanDropsTheBlockItCarriesOnDeath(t *testing.T) {
	m := newEnderman()
	m.SetCarriedBlock(defaultState(t, "minecraft:grass_block"))
	grass, _ := registry.ItemID("minecraft:grass_block")
	dirt, _ := registry.ItemID("minecraft:dirt")
	var got bool
	for _, s := range m.RollDrops(rules.LootKiller{}, nil) {
		if s.ItemID == dirt {
			t.Error("the carried grass block dropped dirt, a silk touch drop is the block itself")
		}
		got = got || s.ItemID == grass && s.Count == 1
	}
	if !got {
		t.Error("an enderman carrying a grass block dropped no grass block")
	}
	m = newEnderman()
	for _, s := range m.RollDrops(rules.LootKiller{}, nil) {
		if s.ItemID == grass {
			t.Error("an enderman carrying nothing dropped a grass block")
		}
	}
}

// angerPlayer is player 7, looking away from the enderman but in plain view.
func angerPlayer(m *Mob, dist float64) Player {
	p := Player{X: m.X + dist, Y: 64, Z: m.Z, EntityID: 7, Huntable: true, UUID: uuid.MustParse("00000000-0000-0000-0000-000000000007")}
	gazeAt(&p, m)
	p.Yaw += 180
	return p
}

func TestAProvokedEndermanStaysAngryForTwentyToThirtyNineSeconds(t *testing.T) {
	w := newCarryWorld(t)
	m := newEnderman()
	p := angerPlayer(m, 8)
	m.TargetID = p.EntityID
	m.Tick(w, []Player{p})
	ticks, target, ok := m.Anger()
	if !m.Angry() || ticks < 400 || ticks > 780 || !ok || target != p.UUID {
		t.Fatalf("after provocation Angry = %v with %d ticks left for %v/%v, want 400 to 780 for the player", m.Angry(), ticks, target, ok)
	}
	// The target lost, the anger stays and points the enderman at the
	// player again though the player looks away.
	m.TargetID = 0
	if !tickUntil(m, w, []Player{p}, 100, func() bool { return m.TargetID == p.EntityID }) {
		t.Fatal("an angry enderman never turned on the player it is angry at")
	}
}

func TestAnEndermanHuntsAnEndermiteItSees(t *testing.T) {
	w := newCarryWorld(t)
	m := newEnderman()
	mite := NewMob(TypeEndermite, 9, m.X+5, 64, m.Z, 3)
	others := []*Mob{m, mite}
	hunted := -1
	for i := range 200 {
		m.tick(w, nil, others)
		if m.TargetID == mite.ID {
			hunted = i
			break
		}
	}
	if hunted < 0 {
		t.Fatal("an enderman never targeted an endermite in plain view")
	}
}

func TestAnEndermanNeverPathsThroughWater(t *testing.T) {
	w := newCarryWorld(t)
	water := defaultState(t, "minecraft:water")
	// A pool two cells wide across the way to the east; the dry way round
	// is open. A footprint that prices water at -1 plans round it.
	for z := int32(-20); z <= 20; z++ {
		for x := int32(3); x <= 4; x++ {
			w.set(x, 63, z, water)
		}
	}
	m := newEnderman()
	foot := footprintOf(m.width(), m.height())
	m.setFireMaluses(&foot)
	if foot.waterMalus >= 0 {
		t.Fatalf("an enderman's water malus = %v, want impassable", foot.waterMalus)
	}
	if cellMalus(w, foot, 3, 63, 0) >= 0 {
		t.Error("a cell of water is passable for an enderman")
	}
	cow := NewMob(TypeCow, 2, 0.5, 64, 0.5, 1)
	cowFoot := footprintOf(cow.width(), cow.height())
	cow.setFireMaluses(&cowFoot)
	if cellMalus(w, cowFoot, 3, 63, 0) < 0 {
		t.Error("water became impassable for a cow")
	}
}
