package entity

import (
	"math/rand"
	"testing"
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
