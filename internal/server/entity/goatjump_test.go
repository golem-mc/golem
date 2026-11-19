package entity

import (
	"math/rand"
	"testing"
)

// gapWorld is two stone platforms with a gap of three blocks between them,
// the only place a goat on the first can land that it cannot walk to.
func gapWorld(t *testing.T) testWorld {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for z := int32(-3); z <= 3; z++ {
		for x := int32(-3); x <= 2; x++ {
			w.set(x, 63, z, stone)
		}
		for x := int32(6); x <= 10; x++ {
			w.set(x, 63, z, stone)
		}
	}
	return w
}

func settledGoat(w World, seed int64) *Mob {
	g := NewMob(TypeGoat, 1, 2.5, 64, 0.5, seed)
	g.tick(w, nil, []*Mob{g})
	g.SetGoatRamCooldown(1 << 20)
	g.SetGoatLongJumpCooldown(0)
	return g
}

// The leap comes forty ticks after the landing spot is picked, with the goat
// standing where it was, and lands the goat across the gap.
// vanilla: LongJumpToRandomPos.tick
func TestAGoatLeapsFortyTicksAfterPickingASpot(t *testing.T) {
	w := gapWorld(t)
	g := settledGoat(w, 1)
	picked, launched := -1, -1
	var events []MobEvent
	for tick := 1; tick <= 400 && launched < 0; tick++ {
		events = append(events, g.tick(w, nil, []*Mob{g})...)
		if g.Goat.jump.chosen && picked < 0 {
			picked = tick
		}
		if g.Goat.jump.mid {
			launched = tick
		}
	}
	if picked < 0 || launched < 0 {
		t.Fatalf("picked at %d, launched at %d", picked, launched)
	}
	if launched-picked != 40 {
		t.Errorf("the leap came %d ticks after the pick, want 40", launched-picked)
	}
	if !g.Goat.jump.noFriction {
		t.Error("the launch kept friction")
	}
	for tick := 0; tick < 200 && g.Goat.jump.mid; tick++ {
		events = append(events, g.tick(w, nil, []*Mob{g})...)
		if tick == 1 && !g.LongJumping() {
			t.Error("the goat is not in the long jumping pose in flight")
		}
	}
	if g.X < 5.5 || !g.OnGround {
		t.Errorf("the goat landed at x=%.2f onGround=%v, want the far platform", g.X, g.OnGround)
	}
	if g.LongJumping() || g.Goat.jump.noFriction {
		t.Error("the pose or the friction outlived the landing")
	}
	if c := g.LongJumpCooldown(); c < 590 || c > goatJumpGapMax {
		t.Errorf("the cooldown after the landing = %d, want 600 to 1200", c)
	}
	var jump, step bool
	for _, e := range events {
		if e.Kind == MobGoatSounded {
			jump = jump || e.SoundID.Name() == "minecraft:entity.goat.long_jump"
			if e.SoundID.Name() == "minecraft:entity.goat.step" && e.Volume != 2 {
				t.Errorf("the landing step has volume %v, want 2", e.Volume)
			}
			step = step || e.SoundID.Name() == "minecraft:entity.goat.step"
		}
	}
	if !jump || !step {
		t.Errorf("long_jump sound %v, landing step %v", jump, step)
	}
}

// A goat that is moved while it stands waiting gives the jump up for half a
// cooldown.
// vanilla: LongJumpToRandomPos.canStillUse
func TestAGoatShovedBeforeItLeapsGivesUp(t *testing.T) {
	w := gapWorld(t)
	g := settledGoat(w, 1)
	for tick := 0; tick < 400 && !g.Goat.jump.chosen; tick++ {
		g.tick(w, nil, []*Mob{g})
	}
	if !g.Goat.jump.chosen {
		t.Fatal("no spot picked")
	}
	g.X += 0.25
	g.tick(w, nil, []*Mob{g})
	g.tick(w, nil, []*Mob{g})
	if g.Goat.jump.mid || g.Goat.jump.running {
		t.Error("the goat kept to its jump after being moved")
	}
	if c := g.LongJumpCooldown(); c < 295 || c > 600 {
		t.Errorf("the cooldown = %d, want 300 to 600", c)
	}
}

// A goat in the air when its jump is due takes half a cooldown instead.
// vanilla: LongJumpToRandomPos.checkExtraStartConditions
func TestAGoatInTheAirTakesHalfACooldown(t *testing.T) {
	w := newTestWorld()
	g := NewMob(TypeGoat, 1, 0.5, 70, 0.5, 1)
	g.SetGoatRamCooldown(1 << 20)
	g.SetGoatLongJumpCooldown(0)
	g.tick(w, nil, []*Mob{g})
	if c := g.LongJumpCooldown(); c < 295 || c > 600 {
		t.Errorf("the cooldown = %d, want 300 to 600", c)
	}
}

// With nowhere out of walking reach to land, the goat tries and takes half a
// cooldown.
func TestAGoatWithNowhereToLeapTakesHalfACooldown(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-8); x <= 8; x++ {
		for z := int32(-8); z <= 8; z++ {
			w.set(x, 63, z, stone)
		}
	}
	g := settledGoat(w, 1)
	for range 4 {
		g.tick(w, nil, []*Mob{g})
	}
	if g.Goat.jump.mid || g.Goat.jump.chosen {
		t.Fatal("the goat leapt on level ground")
	}
	if c := g.LongJumpCooldown(); c < 295 || c > 600 {
		t.Errorf("the cooldown = %d, want 300 to 600", c)
	}
}

func hunted(x, y, z float64) []Player {
	return []Player{{X: x, Y: y, Z: z, EntityID: 50, Huntable: true, Pushable: true}}
}

func flatWorld(t *testing.T) testWorld {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-20); x <= 20; x++ {
		for z := int32(-20); z <= 20; z++ {
			w.set(x, 63, z, stone)
		}
	}
	return w
}

// Before its sensor has scanned, a goat has no memory of nearby entities and
// does not start a ram.
// vanilla: PrepareRamNearestTarget's NEAREST_VISIBLE_LIVING_ENTITIES requirement
func TestAGoatDoesNotRamBeforeItsSensorScans(t *testing.T) {
	w := flatWorld(t)
	g := NewMob(TypeGoat, 1, 0.5, 64, 0.5, 1)
	g.Goat.sense.delay = 10
	g.SetGoatLongJumpCooldown(1 << 20)
	g.SetGoatRamCooldown(0)
	players := hunted(8.5, 64, 0.5)
	for tick := 1; tick < 10; tick++ {
		g.tick(w, players, []*Mob{g})
		if g.Goat.ram.phase != goatIdle || g.RamCooldown() != 0 {
			t.Fatalf("tick %d: ram phase %v cooldown %d before the first scan", tick, g.Goat.ram.phase, g.RamCooldown())
		}
	}
	for range 2 {
		g.tick(w, players, []*Mob{g})
	}
	if g.Goat.ram.phase != goatPreparing {
		t.Errorf("the ram did not start on the scan tick, phase %v", g.Goat.ram.phase)
	}
}

// The scan's list is kept for twenty ticks: a player who steps into view after
// it is not a target until the next, and the ram finds nobody.
// vanilla: Sensor.tick, DEFAULT_SCAN_RATE
func TestAGoatDoesNotSeeAPlayerWhoArrivesBetweenScans(t *testing.T) {
	w := flatWorld(t)
	g := NewMob(TypeGoat, 1, 0.5, 64, 0.5, 1)
	g.Goat.sense.delay = 1
	g.SetGoatLongJumpCooldown(1 << 20)
	g.SetGoatRamCooldown(1 << 20)
	g.tick(w, nil, []*Mob{g})
	if !g.Goat.sense.scanned || g.Goat.sense.delay != goatSenseInterval {
		t.Fatalf("scanned %v delay %d", g.Goat.sense.scanned, g.Goat.sense.delay)
	}
	players := hunted(8.5, 64, 0.5)
	g.SetGoatRamCooldown(0)
	for range 2 {
		g.tick(w, players, []*Mob{g})
	}
	if g.Goat.ram.phase != goatIdle || g.RamCooldown() < goatRamGapMin-3 {
		t.Errorf("ram phase %v cooldown %d, want the fail cooldown with nobody remembered", g.Goat.ram.phase, g.RamCooldown())
	}
	g.SetGoatRamCooldown(0)
	for tick := 0; tick < goatSenseInterval && g.Goat.ram.phase == goatIdle; tick++ {
		g.SetGoatRamCooldown(0)
		g.tick(w, players, []*Mob{g})
	}
	if g.Goat.ram.phase != goatPreparing {
		t.Errorf("the next scan did not bring the player into the ram's view, phase %v", g.Goat.ram.phase)
	}
}

// A player within six blocks draws the goat's look after a 30 to 60 tick
// countdown, held for 45 to 90; one seven blocks off draws none.
// vanilla: SetEntityLookTargetSometimes, LookAtTargetSink
func TestAGoatLooksAtANearPlayerAfterAWhile(t *testing.T) {
	w := flatWorld(t)
	for _, tc := range []struct {
		name string
		dist float64
		look bool
	}{{"near", 5, true}, {"far", 7, false}} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewMob(TypeGoat, 1, 0.5, 64, 0.5, 1)
			g.Goat.sense.delay = 1
			g.SetGoatLongJumpCooldown(1 << 20)
			g.SetGoatRamCooldown(1 << 20)
			players := hunted(0.5+tc.dist, 64, 0.5)
			first := -1
			for tick := 1; tick <= 100; tick++ {
				g.tick(w, players, []*Mob{g})
				if g.Goat.look.target != 0 && first < 0 {
					first = tick
				}
			}
			if !tc.look {
				if first >= 0 {
					t.Errorf("the goat looked at a player %v blocks off at tick %d", tc.dist, first)
				}
				return
			}
			// The scan is on tick 1, the ticker rolls on tick 1 and counts
			// 29 to 59 more.
			if first < 30 || first > 62 {
				t.Errorf("the goat first looked at tick %d, want 30 to 62", first)
			}
		})
	}
}

// The jump candidates run x fastest and z outermost, the order the weighted
// draw walks.
// vanilla: LongJumpToRandomPos.start, BlockPos.betweenClosedStream
func TestAGoatListsJumpCandidatesWithXFastest(t *testing.T) {
	w := flatWorld(t)
	g := settledGoat(w, 1)
	(&goatJumpGoal{m: g}).Start(&tickView{w: w})
	c := g.Goat.jump.candidates
	if c[0].cell.X+1 != c[1].cell.X || c[0].cell.Z != c[1].cell.Z {
		t.Errorf("first candidates %v %v, want x to step first", c[0].cell, c[1].cell)
	}
	want := int(2*goatJumpReach + 1)
	if int(c[want-1].cell.X-c[0].cell.X) != want-1 || c[want].cell.Y != c[0].cell.Y+1 {
		t.Errorf("row %v..%v then %v, want y to step after a row of x", c[0].cell, c[want-1].cell, c[want].cell)
	}
}

// A goat holding a look drops it when the player dies, leaves the sensor's
// list or is too invisible to be seen.
// vanilla: LookAtTargetSink.canStillUse, EntityTracker.isVisibleBy
func TestAGoatDropsALookAtAPlayerWhoIsGone(t *testing.T) {
	w := flatWorld(t)
	for _, tc := range []struct {
		name string
		gone func(p *Player)
	}{
		{"dead", func(p *Player) { p.Dead = true }},
		{"invisible", func(p *Player) { p.Invisible = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewMob(TypeGoat, 1, 0.5, 64, 0.5, 1)
			g.Goat.sense.delay = 1
			g.SetGoatLongJumpCooldown(1 << 20)
			g.SetGoatRamCooldown(1 << 20)
			players := hunted(5.5, 64, 0.5)
			for tick := 0; tick < 100 && g.Goat.look.target == 0; tick++ {
				g.tick(w, players, []*Mob{g})
			}
			if g.Goat.look.target == 0 {
				t.Fatal("the goat never looked")
			}
			tc.gone(&players[0])
			// The sensor's answers stand until its next scan.
			for range goatSenseInterval {
				g.tick(w, players, []*Mob{g})
			}
			if g.Goat.look.target != 0 {
				t.Error("the goat kept its look at a player it cannot see")
			}
		})
	}
	t.Run("out of the sensor list", func(t *testing.T) {
		g := NewMob(TypeGoat, 1, 0.5, 64, 0.5, 1)
		g.Goat.sense.delay = 1
		g.SetGoatLongJumpCooldown(1 << 20)
		g.SetGoatRamCooldown(1 << 20)
		players := hunted(5.5, 64, 0.5)
		for tick := 0; tick < 100 && g.Goat.look.target == 0; tick++ {
			g.tick(w, players, []*Mob{g})
		}
		g.Goat.sense.nearby = nil
		g.tick(w, players, []*Mob{g})
		if g.Goat.look.target != 0 {
			t.Error("the goat kept its look at a player the sensor no longer lists")
		}
	})
}

// The mid-jump behaviour times out when the tick passes its start plus 100.
// vanilla: Behavior.tickOrStop, LongJumpMidJump
func TestAGoatMidJumpEndsAfterItsHundredTickTimeout(t *testing.T) {
	g := NewMob(TypeGoat, 1, 0.5, 64, 0.5, 1)
	g.OnGround = false
	g.Goat.jump.mid = true
	tv := &tickView{}
	for i := 1; i <= goatMidJumpTimeout+1; i++ {
		g.midJumpStep(tv)
		if !g.Goat.jump.mid {
			t.Fatalf("the mid-jump ended on tick %d, want it to run through tick %d", i, goatMidJumpTimeout+1)
		}
	}
	g.midJumpStep(tv)
	if g.Goat.jump.mid {
		t.Error("the mid-jump outlived its timeout")
	}
}

// The jump cooldown is drawn before the ram cooldown.
// vanilla: GoatAi.initMemories
func TestAGoatDrawsItsJumpCooldownBeforeTheRamCooldown(t *testing.T) {
	g := NewMob(TypeGoat, 1, 0.5, 64, 0.5, 7)
	r := rand.New(rand.NewSource(7))
	if want := goatJumpGapMin + r.Intn(goatJumpGapMax-goatJumpGapMin+1); g.LongJumpCooldown() != want {
		t.Errorf("jump cooldown %d, want the first draw %d", g.LongJumpCooldown(), want)
	}
	if want := rollGoatGap(GoDice{r}, false); g.RamCooldown() != want {
		t.Errorf("ram cooldown %d, want the second draw %d", g.RamCooldown(), want)
	}
}
