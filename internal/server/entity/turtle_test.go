package entity

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

func turtleBeach(t *testing.T) spawnTestWorld {
	t.Helper()
	w := newSpawnTestWorld()
	w.sky = 15
	floor(w.testWorld, -4, -4, 30, 4, 63, defaultState(t, "minecraft:sand"))
	return w
}

// TestATurtleSpawnsOnSandUnderFourAboveSeaLevel: Turtle.checkTurtleSpawnRules
// wants sand under the feet, a y below sea level plus four and light above 8.
func TestATurtleSpawnsOnSandUnderFourAboveSeaLevel(t *testing.T) {
	w := turtleBeach(t)
	rng := rand.New(rand.NewSource(1))
	if !validSpawnPositionAt(w, GoDice{rng}, TypeTurtle, 0.5, 64, 0.5, 0, CategoryCreature) {
		t.Fatal("a turtle was refused sand at y 64, want it spawned")
	}
	if !SpawnerAllows(w, rng, TypeTurtle, 0.5, 64, 0.5) {
		t.Error("a spawner refused a turtle on sand")
	}
	grass := turtleBeach(t)
	floor(grass.testWorld, -4, -4, 4, 4, 63, defaultState(t, "minecraft:grass_block"))
	if validSpawnPositionAt(grass, GoDice{rng}, TypeTurtle, 0.5, 64, 0.5, 0, CategoryCreature) {
		t.Error("a turtle spawned on grass, want sand only")
	}
	if validSpawnPositionAt(grass, GoDice{rng}, TypeCow, 0.5, 64, 0.5, 0, CategoryCreature) != true {
		t.Error("a cow was refused grass, want the animals' rule kept")
	}
	high := newSpawnTestWorld()
	high.sky = 15
	floor(high.testWorld, -4, -4, 4, 4, 69, defaultState(t, "minecraft:sand"))
	if validSpawnPositionAt(high, GoDice{rng}, TypeTurtle, 0.5, 70, 0.5, 0, CategoryCreature) {
		t.Error("a turtle spawned at y 70, want the limit of sea level plus four")
	}
	dark := turtleBeach(t)
	dark.sky = 8
	if validSpawnPositionAt(dark, GoDice{rng}, TypeTurtle, 0.5, 64, 0.5, 0, CategoryCreature) {
		t.Error("a turtle spawned in light 8, want light above 8")
	}
}

// TestATurtleHatchlingIsSmall: Turtle.BABY_DIMENSIONS scales the 1.2 by 0.4
// box by 0.3.
func TestATurtleHatchlingIsSmall(t *testing.T) {
	m := NewMob(TypeTurtle, 1, 0.5, 64, 0.5, 1)
	if m.width() != 1.2 || m.height() != 0.4 || m.MaxHealth != 30 {
		t.Errorf("turtle box %v by %v health %v, want 1.2 by 0.4 and 30", m.width(), m.height(), m.MaxHealth)
	}
	m.SetBaby(true)
	if m.width() != 0.36 || m.height() != 0.12 {
		t.Errorf("hatchling box %v by %v, want 0.36 by 0.12", m.width(), m.height())
	}
}

// TestABredTurtleCarriesAnEggInsteadOfBearingAChild: TurtleBreedGoal.breed
// sets the egg on the goal's turtle and starts both cooldowns.
func TestABredTurtleCarriesAnEggInsteadOfBearingAChild(t *testing.T) {
	a := lover(1, 0.5, TypeTurtle)
	b := lover(2, 1.5, TypeTurtle)
	tv := breedTickView(a, b)
	g := &breedGoal{m: a, partner: b, loveTime: breedWait, turtle: true}
	g.Tick(tv)
	if !a.HasEgg || b.HasEgg {
		t.Errorf("eggs a=%v b=%v after the courtship, want the courting turtle alone", a.HasEgg, b.HasEgg)
	}
	if a.BabyAge != 6000 || b.BabyAge != 6000 || a.InLove != 0 {
		t.Errorf("cooldowns %d %d love %d, want 6000 6000 and love ended", a.BabyAge, b.BabyAge, a.InLove)
	}
	if loved, _ := a.Feed(); loved {
		t.Error("a turtle with an egg fell in love, want canFallInLove refused")
	}
	if (&breedGoal{m: a, turtle: true}).CanStart(tv) {
		t.Error("a turtle with an egg started courting")
	}
}

// TestATurtleLaysItsEggOnSandNearHome: with an egg and home in reach the
// lay goal walks onto sand, digs for 200 goal ticks and sets the egg.
func TestATurtleLaysItsEggOnSandNearHome(t *testing.T) {
	w := turtleBeach(t)
	m := NewMob(TypeTurtle, 1, 0.5, 64, 0.5, 1)
	m.HasEgg = true
	g := newTurtleLayEggGoal(m)
	tv := &tickView{w: w}
	g.nextStart = 0
	if !g.CanStart(tv) {
		t.Fatal("a turtle with an egg on sand at home did not start laying")
	}
	g.Start(tv)
	var laid *MobEvent
	ticks := 0
	for range 400 {
		g.Tick(tv)
		ticks++
		for i := range tv.events {
			if tv.events[i].Kind == MobLaidTurtleEgg {
				laid = &tv.events[i]
			}
		}
		if laid != nil {
			break
		}
	}
	if laid == nil {
		t.Fatal("no egg was laid")
	}
	if ticks < 200 {
		t.Errorf("the egg came after %d goal ticks, want the 200 an every-tick goal waits", ticks)
	}
	if laid.Amount < 1 || laid.Amount > 4 || m.HasEgg || m.LayingEgg || m.InLove != 600 {
		t.Errorf("egg count %d hasEgg %v laying %v love %d, want 1-4, both cleared and love 600", laid.Amount, m.HasEgg, m.LayingEgg, m.InLove)
	}
	far := NewMob(TypeTurtle, 2, 20.5, 64, 0.5, 1)
	far.Home = Waypoint{0, 64, 0}
	far.HasEgg = true
	if newTurtleLayEggGoal(far).CanStart(tv) {
		t.Error("a turtle ten blocks from home started laying, want nine at most")
	}
}

// TestAHatchlingGrowingUpReportsIt: Turtle.ageBoundaryReached drops the
// turtle_grow gift, which the server learns of by MobGrewUp.
func TestAHatchlingGrowingUpReportsIt(t *testing.T) {
	w := turtleBeach(t)
	m := NewMob(TypeTurtle, 1, 0.5, 64, 0.5, 1)
	m.SetBaby(true)
	m.BabyAge = -1
	var grew int
	for _, ev := range m.Tick(w, nil) {
		if ev.Kind == MobGrewUp {
			grew++
		}
	}
	if grew != 1 || m.IsBaby() {
		t.Errorf("grew events %d baby %v, want one and a grown turtle", grew, m.IsBaby())
	}
}

// TestLightningKillsATurtleAndPaysABowl: Turtle.thunderHit hurts for the
// float maximum and the loot table's is_lightning pool pays a bowl.
func TestLightningKillsATurtleAndPaysABowl(t *testing.T) {
	m := NewMob(TypeTurtle, 1, 0.5, 64, 0.5, 1)
	m.ThunderHit(false)
	if m.Alive() {
		t.Fatal("a turtle survived the bolt")
	}
	bowl, _ := registry.ItemID("minecraft:bowl")
	var found bool
	for _, stack := range m.RollDrops(rules.LootKiller{}, nil) {
		if stack.ItemID == bowl {
			found = true
		}
	}
	if !found {
		t.Error("the turtle struck dead dropped no bowl")
	}
	plain := NewMob(TypeTurtle, 2, 0.5, 64, 0.5, 1)
	plain.HurtByWorld(100, rules.DamageTypeFall)
	for _, stack := range plain.RollDrops(rules.LootKiller{}, nil) {
		if stack.ItemID == bowl {
			t.Error("a turtle that fell dead dropped a bowl")
		}
	}
}

// TestATurtleInShallowWaterFindsAWayHome: the turtle's water malus is 0, so
// the walk towards home may pick water cells.
func TestATurtleInShallowWaterFindsAWayHome(t *testing.T) {
	w := turtleBeach(t)
	water := defaultState(t, "minecraft:water")
	// water over the whole beach: the only cells in reach are water cells
	for x := int32(-4); x <= 30; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 64, z, water)
		}
	}
	m := NewMob(TypeTurtle, 1, 0.5, 64, 0.5, 1)
	found := 0
	for range 50 {
		if _, ok := posTowards(w, m, 16, 3, [2]float64{20, 0}, turtleHomeAngle); ok {
			found++
		}
	}
	if found == 0 {
		t.Error("no cell towards home found from shallow water, want water allowed")
	}
}

// TestATurtleIsNotLeashedPushedOrStoppedByLedges: canBeLeashed is false,
// isPushedByFluid is false and the step height is 1.
func TestATurtleIsNotLeashedPushedOrStoppedByLedges(t *testing.T) {
	if Leashable(TypeTurtle) {
		t.Error("a turtle is leashable, want not")
	}
	if got := NewMob(TypeTurtle, 1, 0.5, 64, 0.5, 1).stepHeight(); got != 1.0 {
		t.Errorf("turtle step height %v, want 1", got)
	}
	if got := NewMob(TypeCow, 2, 0.5, 64, 0.5, 1).stepHeight(); got != 0.6 {
		t.Errorf("cow step height %v, want 0.6", got)
	}
}

// TestATurtleSettlesToItsLandSpeed: at pace 0.25 the land speed is 0.08375,
// panicking at 0.3 it is 0.09.
func TestATurtleSettlesToItsLandSpeed(t *testing.T) {
	for target, want := range map[float64]float64{0.25: 0.08375, 0.3: 0.09} {
		if got := turtleSettle(target, 2, turtleLandFloor); got < want-1e-9 || got > want+1e-9 {
			t.Errorf("turtleSettle(%v) = %v, want %v", target, got, want)
		}
	}
}

// TestATurtleWalkOnLandRunsAtItsMoveControlSpeed: the path walk's speed on
// land is the move control's settled speed, not a fixed fraction of the target.
func TestATurtleWalkOnLandRunsAtItsMoveControlSpeed(t *testing.T) {
	m := NewMob(TypeTurtle, 1, 0.5, 64, 0.5, 1)
	m.OnGround = true
	m.nav.speedFactor = 2
	target := m.pace() * 2
	want := turtleSettle(target, 2, turtleLandFloor)
	if want == target*0.335 {
		t.Fatalf("land speed %v is a fixed fraction of the target, test cannot tell", want)
	}
	if got := m.pathSpeed(); got != want {
		t.Errorf("pathSpeed %v, want %v", got, want)
	}
}

// TestATurtleInWaterSwimsTowardsAFarPoint: TurtleTravelGoal starts in water
// with no egg or errand, takes a swimming step and ends for an egg, a trip
// home, love or a point it cannot step towards.
func TestATurtleInWaterSwimsTowardsAFarPoint(t *testing.T) {
	w := turtleBeach(t)
	water := defaultState(t, "minecraft:water")
	_ = water
	fillWater(t, w, -4, -4, 30, 4)
	m, g, tv := travellingTurtle(w)
	if !g.CanStart(tv) {
		t.Fatal("a turtle in water did not start travelling")
	}
	g.Start(tv)
	if math.Abs(g.target[0]-m.X) > turtleTravelXZ+1 || math.Abs(g.target[1]-m.Z) > turtleTravelXZ+1 {
		t.Errorf("target %v is beyond %d blocks", g.target, turtleTravelXZ)
	}
	g.target = [2]float64{512.5, 0.5}
	g.Tick(tv)
	if g.stuck || m.navDone() {
		t.Errorf("no step was taken towards the far point (stuck %v)", g.stuck)
	}
	m.HasEgg = true
	if g.CanStart(tv) || g.ShouldContinue(tv) {
		t.Error("a turtle with an egg travels")
	}
	m.HasEgg, m.goingHome = false, true
	if g.CanStart(tv) || g.ShouldContinue(tv) {
		t.Error("a turtle going home travels")
	}
	m.goingHome, m.InLove = false, 100
	if g.ShouldContinue(tv) {
		t.Error("a turtle in love keeps travelling")
	}
}

// holedWorld is a world whose chunk at (hx, hz) is not loaded.
type holedWorld struct {
	spawnTestWorld
	hx, hz int32
}

func (w holedWorld) ChunkLoadedAt(x, z int32) bool { return x>>4 != w.hx || z>>4 != w.hz }

func travellingTurtle(w World) (*Mob, *turtleTravelGoal, *tickView) {
	m := NewMob(TypeTurtle, 1, 0.5, 64, 0.5, 1)
	m.Rand = rand.New(rand.NewSource(7))
	m.OnGround = false
	m.waterFluid.height = 1
	g := &turtleTravelGoal{m: m, target: [2]float64{512.5, 0.5}}
	return m, g, &tickView{w: w}
}

func fillWater(t *testing.T, w spawnTestWorld, x0, z0, x1, z1 int32) {
	water := defaultState(t, "minecraft:water")
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			w.set(x, 64, z, water)
		}
	}
}

// TestATurtleDoesNotTravelIntoAnUnloadedMiddleChunk: hasChunksAt wants every
// chunk of the 69 wide box loaded, not only its corners.
func TestATurtleDoesNotTravelIntoAnUnloadedMiddleChunk(t *testing.T) {
	base := turtleBeach(t)
	fillWater(t, base, -4, -4, 30, 4)
	for _, tc := range []struct {
		name   string
		hx, hz int32
		stuck  bool
	}{{"all loaded", 1 << 20, 0, false}, {"middle chunk", 1, 0, true}} {
		_, g, tv := travellingTurtle(holedWorld{base, tc.hx, tc.hz})
		m := g.m
		g.Tick(tv)
		if g.stuck != tc.stuck || m.navDone() != tc.stuck {
			t.Errorf("%s: stuck %v navDone %v, want stuck %v", tc.name, g.stuck, m.navDone(), tc.stuck)
		}
	}
}

// TestATurtleFallsBackToTheWideStep: when no cell in the narrow 16/3 cone is
// swimmable, the 8/7 step in the wide cone is taken.
func TestATurtleFallsBackToTheWideStep(t *testing.T) {
	w := turtleBeach(t)
	floor(w.testWorld, -30, -30, 40, 30, 63, defaultState(t, "minecraft:sand"))
	fillWater(t, w, 0, -30, 1, 30)
	stone := defaultState(t, "minecraft:stone")
	for x := int32(2); x <= 40; x++ {
		for z := int32(-30); z <= 30; z++ {
			for y := int32(64); y <= 70; y++ {
				w.set(x, y, z, stone)
			}
		}
	}
	m, g, tv := travellingTurtle(w)
	m.Rand = rand.New(rand.NewSource(8))
	if _, ok := turtleTravelPos(w, m, 16, 3, g.target, turtleHomeAngle); ok {
		t.Fatal("the narrow cone found a cell, the fixture does not exercise the fallback")
	}
	m.Rand = rand.New(rand.NewSource(8))
	g.Tick(tv)
	if g.stuck {
		t.Errorf("no wide step was found, the goal gave up")
	}
}

// TestATurtleTravelGoalIsWiredAtPriority7 checks registerGoals' slot.
func TestATurtleTravelGoalIsWiredAtPriority7(t *testing.T) {
	m := NewMob(TypeTurtle, 1, 0.5, 64, 0.5, 1)
	n := 0
	for _, g := range turtleGoals(m) {
		if _, ok := g.(*turtleTravelGoal); ok {
			n++
			if g.Priority() != 7 {
				t.Errorf("priority %d, want 7", g.Priority())
			}
		}
	}
	if n != 1 {
		t.Errorf("%d travel goals wired, want 1", n)
	}
}

// TestATurtleSinksSlowlyInWater: a turtle that neither targets nor heads home
// loses 0.005 a tick net of its move control's lift, dragged by 0.9.
func TestATurtleSinksSlowlyInWater(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeTurtle, 1, 0.5, 63, 0.5, 1)
	m.VY = 0.1
	m.turtleTravelInWater(w)
	if want := (0.1 + 0.005) * 0.9; m.VY < want-0.005-1e-9 || m.VY > want-0.005+1e-9 {
		t.Errorf("vy %v, want %v", m.VY, want-0.005)
	}
}

// TestATurtleWaterSounds: a turtle shambles at its own sound by age and
// swims louder than a generic body.
func TestATurtleWaterSounds(t *testing.T) {
	m := NewMob(TypeTurtle, 1, 0.5, 64, 0.5, 1)
	if got, _, _, _ := m.stepVoice(nil, 0, 0, 0, 0); got.Name() != "minecraft:entity.turtle.shamble" {
		t.Errorf("adult step %q", got)
	}
	m.BabyAge = -100
	if got, _, _, _ := m.stepVoice(nil, 0, 0, 0, 0); got.Name() != "minecraft:entity.turtle.shamble_baby" {
		t.Errorf("baby step %q", got)
	}
	m.VX = 50
	events := m.swimEvent(nil)
	if events[0].SoundID.Name() != "minecraft:entity.turtle.swim" || events[0].Volume != 1.5 {
		t.Errorf("swim %q volume %v", events[0].SoundID.Name(), events[0].Volume)
	}
}

// TestATurtlePlansDownThroughDeepWater: the amphibious planner swims a
// turtle down a column of water, which the ground planner refuses.
func TestATurtlePlansDownThroughDeepWater(t *testing.T) {
	w := turtleBeach(t)
	water := defaultState(t, "minecraft:water")
	for x := int32(-4); x <= 4; x++ {
		for z := int32(-4); z <= 4; z++ {
			for y := int32(60); y <= 64; y++ {
				w.set(x, y, z, water)
			}
		}
	}
	m := NewMob(TypeTurtle, 1, 0.5, 64, 0.5, 1)
	m.waterFluid.height = 1
	if !m.seekTo(w, 3.5, 61, 0.5, 1, 0) || len(m.nav.path) == 0 {
		t.Fatal("no path down through deep water")
	}
	if last := m.nav.path[len(m.nav.path)-1]; last.Y > 62 || last.X < 2 {
		t.Errorf("path ends at %v, want it near (3, 61)", last)
	}
}

// TestATurtleTravelsToWaterOnly: with a travel position the stable
// destinations are water blocks, not the sand around the pond.
func TestATurtleTravelsToWaterOnly(t *testing.T) {
	w := turtleBeach(t)
	fillWater(t, w, -1, -1, 1, 1)
	m, _, _ := travellingTurtle(w)
	for range 20 {
		if cell, ok := turtleTravelPos(w, m, 16, 3, [2]float64{512.5, 0.5}, turtleHomeAngle); ok &&
			!rules.IsWater(w.BlockAt(cell.X, cell.Y, cell.Z)) {
			t.Fatalf("travel target %v is not water", cell)
		}
	}
}

func TestAZombiePathsOntoATurtleEgg(t *testing.T) {
	w := newTestWorld()
	floor(w, -10, -10, 20, 10, 63, defaultState(t, "minecraft:sand"))
	w.set(1, 64, 0, defaultState(t, "minecraft:turtle_egg"))
	m := NewMob(TypeZombie, 1, 8.5, 64, 0.5, 1)
	if !m.seekTo(w, 1.5, 65, 0.5, 1, 0) {
		t.Fatal("no path to the egg")
	}
	path := m.nav.path
	if last := path[len(path)-1]; last != (Waypoint{1, 64, 0}) {
		t.Errorf("the path ends at %v, want the egg cell {1 64 0}", last)
	}
	if walkableAt(w, footprintOf(m.width(), m.height()), 1, 65, 0) {
		t.Error("the cell above the egg is walkable")
	}
}

// TestATurtleInWaterPassesANodeOnAClearLine: within 2 blocks of a node it has
// not reached, a turtle in water with a clear line to the node moves on to the
// next one; on land it keeps the node.
// vanilla: TurtlePathNavigation, AmphibiousPathNavigation.canMoveDirectly, PathNavigation.shouldTargetNextNodeInDirection
func TestATurtleInWaterPassesANodeOnAClearLine(t *testing.T) {
	w := turtleBeach(t)
	water := defaultState(t, "minecraft:water")
	for x := int32(-4); x <= 10; x++ {
		for z := int32(-4); z <= 4; z++ {
			for y := int32(61); y <= 63; y++ {
				w.set(x, y, z, water)
			}
		}
	}
	path := []Waypoint{{2, 61, 0}, {4, 61, 0}, {6, 61, 0}}
	for _, inWater := range []bool{true, false} {
		m := NewMob(TypeTurtle, 1, 1, 61, 0.5, 1)
		if inWater {
			m.waterFluid.height = 1
		}
		m.moveTo(append([]Waypoint(nil), path...), path[2], 1)
		m.navigate(w)
		want := path[1]
		if !inWater {
			want = path[0]
		}
		if len(m.nav.path) == 0 || m.nav.path[0] != want {
			t.Errorf("in water %v: steering to %v, want %v", inWater, m.nav.path, want)
		}
	}
}
