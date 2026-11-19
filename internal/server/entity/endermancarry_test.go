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

func TestAnEndermanLeavesBlocksThatAreNotHoldable(t *testing.T) {
	w := newCarryWorld(t)
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			if x != 0 || z != 0 {
				w.set(x, 64, z, stone)
			}
		}
	}
	m := newEnderman()
	if _, ok := takeTries(m, w, 500); ok {
		t.Fatal("an enderman took a stone block")
	}
}

func TestAnEndermanTakesNothingBehindAWall(t *testing.T) {
	w := newCarryWorld(t)
	dirt, stone := defaultState(t, "minecraft:dirt"), defaultState(t, "minecraft:stone")
	// The dirt stands two cells east with a stone block between, so the one
	// holdable block in reach is hidden.
	w.set(2, 64, 0, dirt)
	w.set(1, 64, 0, stone)
	m := newEnderman()
	if _, ok := takeTries(m, w, 3000); ok {
		t.Fatal("an enderman took a block through a wall")
	}
	// The same dirt in the open is taken.
	w.set(1, 64, 0, 0)
	if _, ok := takeTries(m, w, 3000); !ok {
		t.Fatal("the control dirt in the open was never taken")
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

func TestAnEndermanSetsNoBlockDownWithoutAFullBlockUnder(t *testing.T) {
	for _, floor := range []string{"minecraft:bedrock", "minecraft:oak_slab", "minecraft:air"} {
		t.Run(floor, func(t *testing.T) {
			w := newCarryWorld(t)
			state := defaultState(t, floor)
			for x := int32(-40); x <= 40; x++ {
				for z := int32(-40); z <= 40; z++ {
					w.set(x, 63, z, state)
				}
			}
			m := newEnderman()
			m.SetCarriedBlock(defaultState(t, "minecraft:dirt"))
			if _, ok := leaveTries(m, w, 1000); ok {
				t.Fatalf("an enderman set its block down on %s", floor)
			}
		})
	}
}

func TestAnEndermanSetsNoBlockDownInsideAPlayer(t *testing.T) {
	w := newCarryWorld(t)
	m := newEnderman()
	m.SetCarriedBlock(defaultState(t, "minecraft:dirt"))
	// Players tile the whole reach: x in -1..1, z in -1..1 at feet level 64.
	var players []Player
	for x := -1.0; x <= 1; x++ {
		for z := -1.0; z <= 1; z++ {
			players = append(players, Player{X: x + 0.5, Y: 64, Z: z + 0.5, EntityID: int32(10 + len(players))})
		}
	}
	g := &endermanLeaveBlockGoal{m: m}
	for range 2000 {
		view := &tickView{w: w, players: players}
		g.Tick(view)
		if _, ok := eventOf(view.events, MobPlacedBlock); ok {
			t.Fatal("an enderman set its block down inside a player")
		}
	}
}

func TestACarryingEndermanIsNeverDespawned(t *testing.T) {
	w := newCarryWorld(t)
	far := []Player{{X: 300, Y: 64, Z: 0, EntityID: 7, Huntable: true}}
	m := newEnderman()
	if !m.despawnStep(w, far) {
		t.Fatal("the control enderman a long way from every player was not despawned")
	}
	m = newEnderman()
	m.SetCarriedBlock(defaultState(t, "minecraft:dirt"))
	if m.despawnStep(w, far) {
		t.Error("an enderman carrying a block was despawned")
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

func TestAnAngerEndsWhenItsTimeRunsOut(t *testing.T) {
	w := newCarryWorld(t)
	m := newEnderman()
	// 100 blocks away is beyond the follow range and inside the despawn
	// distance, so nothing renews the anger and the enderman stays.
	p := angerPlayer(m, 100)
	m.SetAnger(30, p.UUID, true)
	for range 29 {
		m.Tick(w, []Player{p})
	}
	if !m.Angry() {
		t.Fatal("the anger ended before its thirty ticks")
	}
	m.Tick(w, []Player{p})
	m.Tick(w, []Player{p})
	if m.Angry() {
		t.Error("the anger outlived its thirty ticks")
	}
}

func TestAnEndermanWithNoAngerIgnoresAPlayerLookingAway(t *testing.T) {
	w := newCarryWorld(t)
	m := newEnderman()
	p := angerPlayer(m, 8)
	if tickUntil(m, w, []Player{p}, 200, func() bool { return m.TargetID != 0 }) {
		t.Fatal("a calm enderman targeted a player who is not staring")
	}
}

func TestAnAngerEndsWhenTheDeadAreForgiven(t *testing.T) {
	for _, forgive := range []bool{true, false} {
		w := newCarryWorld(t)
		w.off[world.RuleForgiveDeadPlayers] = !forgive
		m := newEnderman()
		p := angerPlayer(m, 100)
		m.SetAnger(600, p.UUID, true)
		m.Tick(w, []Player{p})
		p.Dead = true
		m.Tick(w, []Player{p})
		if m.Angry() == forgive {
			t.Errorf("forgive_dead_players=%v left Angry = %v after the target died", forgive, m.Angry())
		}
	}
}

func TestAnAngerEndsOnPeaceful(t *testing.T) {
	w := newCarryWorld(t)
	m := newEnderman()
	p := angerPlayer(m, 100)
	m.SetAnger(600, p.UUID, true)
	m.Tick(w, []Player{p})
	if !m.Angry() {
		t.Fatal("the control anger ended on easy")
	}
	w.diffUp, w.diff = true, rules.DifficultyPeaceful
	m.Tick(w, []Player{p})
	if m.Angry() {
		t.Error("the anger survived peaceful")
	}
}

func TestAPlayersBlowAnglesAnEndermanAtEveryPlayerUnderUniversalAnger(t *testing.T) {
	for _, universal := range []bool{true, false} {
		w := newCarryWorld(t)
		w.off[world.RuleUniversalAnger] = !universal
		m := newEnderman()
		// The player who strikes stands farther than the bystander, so
		// the bystander is the one a universal anger turns the enderman on.
		hitter := angerPlayer(m, 12)
		other := angerPlayer(m, 6)
		other.EntityID = 8
		other.UUID = uuid.MustParse("00000000-0000-0000-0000-000000000008")
		m.TakeDamage(1, hitter)
		players := []Player{hitter, other}
		sawOther, sawHitter := false, false
		for range 200 {
			m.tick(w, players, nil)
			sawOther = sawOther || m.TargetID == other.EntityID
			sawHitter = sawHitter || m.TargetID == hitter.EntityID
		}
		if universal && (!sawOther || !m.Angry()) {
			t.Errorf("universal_anger on: turned on the bystander = %v, Angry = %v", sawOther, m.Angry())
		}
		if !universal && (sawOther || !sawHitter) {
			t.Errorf("universal_anger off: turned on the bystander = %v, on the hitter = %v", sawOther, sawHitter)
		}
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

func TestAnEndermanHuntsAnEndermiteFarAboveIt(t *testing.T) {
	w := newCarryWorld(t)
	m := newEnderman()
	mite := NewMob(TypeEndermite, 9, m.X, m.Y+20, m.Z, 3)
	others := []*Mob{m, mite}
	for range 400 {
		m.tick(w, nil, others)
		if m.TargetID == mite.ID {
			return
		}
	}
	t.Fatal("an enderman never targeted an endermite 20 blocks above it in plain view")
}

func TestAnEndermanDoesNotHuntAnEndermiteBehindAWall(t *testing.T) {
	w := newCarryWorld(t)
	stone := defaultState(t, "minecraft:stone")
	for y := int32(64); y <= 68; y++ {
		for z := int32(-3); z <= 3; z++ {
			w.set(3, y, z, stone)
		}
	}
	m := newEnderman()
	mite := NewMob(TypeEndermite, 9, m.X+5, 64, m.Z, 3)
	for range 200 {
		m.tick(w, nil, []*Mob{m, mite})
		if m.TargetID == mite.ID {
			t.Fatal("an enderman targeted an endermite through a wall")
		}
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

// TestABlazePricesWaterLavaAndFire: water is impassable to a blaze, lava
// costs 8 and fire costs nothing.
// vanilla: Blaze.<init> setPathfindingMalus
func TestABlazePricesWaterLavaAndFire(t *testing.T) {
	m := NewMob(TypeBlaze, 1, 0.5, 64, 0.5, 1)
	foot := footprintOf(m.width(), m.height())
	m.setFireMaluses(&foot)
	if foot.waterMalus >= 0 || foot.lavaMalus != 8 || foot.fireMalus != 0 || foot.nearFireMalus != 0 {
		t.Errorf("blaze maluses water %v lava %v fire %v near %v, want -1, 8, 0, 0",
			foot.waterMalus, foot.lavaMalus, foot.fireMalus, foot.nearFireMalus)
	}
}
