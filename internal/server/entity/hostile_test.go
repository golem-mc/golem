package entity

import (
	"testing"
)

// The hostile machinery as ordinary unit tests, the way the passive mob's
// walking and wandering is: each case steps a zombie against a world that is
// nothing but the blocks and the weather it sets, with the players as the
// plain list the tick is handed, and asserts on the decisions vanilla's
// goals and despawn run on. The black-box tests in internal/server/server_hostile_
// test.go remain the proof of the whole pipeline over the wire.

// hunter is a zombie stood on a stone floor at the middle of the map, with
// its goals left wired the way a spawned one comes.
func hunter() *Mob {
	return NewMob(TypeZombie, 1, 0.5, 64, 0.5, 3)
}

func TestMeleeReachUsesTargetBox(t *testing.T) {
	zombie := NewMob(TypeZombie, 1, 0, 0, 0, 3)
	golem := NewMob(TypeIronGolem, 2, 1.7, 0, 0, 4)
	wolf := NewMob(TypeWolf, 3, 0, 1, 0, 5)
	rabbit := NewMob(TypeRabbit, 4, 0, 0, 0, 6)
	for _, tc := range []struct {
		name   string
		mob    *Mob
		target Player
		want   bool
	}{
		{"wide target inside reach", zombie, golem.asTarget(), true},
		{"short target below attacker", wolf, rabbit.asTarget(), false},
		{"default player inside reach", zombie, Player{X: 1.4}, true},
		{"default player outside reach", zombie, Player{X: 1.5}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.mob.inMeleeReach(&tc.target); got != tc.want {
				t.Errorf("inMeleeReach = %v, want %v", got, tc.want)
			}
		})
	}
	golem.X = 1.9
	target := golem.asTarget()
	if zombie.inMeleeReach(&target) {
		t.Error("golem at x=1.9 is outside melee reach")
	}
}

// floor64 lays the stone floor the tests stand their mobs on.
func floor64(w testWorld, t testing.TB) {
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-4); x <= 4; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 63, z, stone)
		}
	}
}

// survivor is the player the tests put in the world: huntable, five blocks
// east of the zombie, alive.
func survivor(id int32, x float64) Player {
	return Player{X: x, Y: 64, Z: 0.5, EntityID: id, Huntable: true}
}

// TestAZombieTargetsTheNearestHuntablePlayer checks the hunt's first half: a
// zombie with a survival player inside its follow range finds them, holds
// their entity ID as its target, and walks to them.
func TestAZombieTargetsTheNearestHuntablePlayer(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	players := []Player{survivor(7, 5.5)}

	sawTarget := false
	for range 60 {
		z.Tick(w, players)
		if z.TargetID == 7 {
			sawTarget = true
			break
		}
	}
	if !sawTarget {
		t.Fatalf("the zombie never took the player as its target in sixty ticks")
	}
	if z.X >= 3 {
		t.Errorf("the zombie stands at x=%v after finding its target, want it walking east", z.X)
	}
}

// TestABurningZombieBurnsInTheDaylightAndGoesOutInTheWater checks the sun:
// a zombie under an open sky in the burning daylight catches fire, the fire
// bites a point at vanilla's rhythm, and the water puts the whole thing
// out.
func TestABurningZombieBurnsInTheDaylightAndGoesOutInTheWater(t *testing.T) {
	w := newTestWorld()
	w.burn = true
	w.sky = 15
	w.darken = 0
	floor64(w, t)
	z := hunter()

	ignited := false
	for range 200 {
		z.Tick(w, nil)
		if z.FireTicks > 0 {
			ignited = true
			break
		}
	}
	if !ignited {
		t.Fatal("a zombie in full daylight under an open sky never caught fire")
	}

	// The fire bites once every twentieth tick of it: over the eight
	// seconds of the burn, eight bites land, each the whole point of fire
	// damage — on_fire is on the bypasses-armor tag, so the zombie's two
	// armour points leave it alone. The daylight attribute is dropped for
	// the count: while it stands the sun rolls the fire afresh each tick.
	w.burn = false
	wounded := 0
	for range 160 {
		for _, ev := range z.Tick(w, nil) {
			if ev.Kind == MobBurned {
				wounded++
			}
		}
	}
	if wounded != 8 {
		t.Errorf("a full burn hurt the zombie %d times, want vanilla's one a second eight times", wounded)
	}
	if !closeEnough(z.Health, z.MaxHealth-8) {
		t.Errorf("the burn left the zombie at %v of %v, want eight whole bites off", z.Health, z.MaxHealth)
	}

	// And the water puts it out whole.
	water := defaultState(t, "minecraft:water")
	w.set(0, 64, 0, water)
	z.Tick(w, nil)
	if z.FireTicks != 0 {
		t.Errorf("the zombie kept %d fire ticks in water, want the water to put it out", z.FireTicks)
	}
}

// TestAZombieInTheShadeDoesNotBurn checks the other half of the sun: under a
// roof — no sky light at the eye — the same burning daylight lights nothing,
// and neither does a night under an open sky.
func TestAZombieInTheShadeDoesNotBurn(t *testing.T) {
	// Under a roof: the sky light the burn reads is nothing.
	w := newTestWorld()
	w.burn = true
	w.sky = 0
	floor64(w, t)
	z := hunter()
	for range 400 {
		z.Tick(w, nil)
		if z.FireTicks > 0 {
			t.Fatal("a zombie under a roof caught fire")
		}
	}

	// In the dark of night, under an open sky: the attribute is down.
	night := newTestWorld()
	floor64(night, t)
	z2 := hunter()
	for range 400 {
		z2.Tick(night, nil)
		if z2.FireTicks > 0 {
			t.Fatal("a zombie under a night sky caught fire")
		}
	}
}

// TestAZombieRetaliatesOnWhoeverStruckIt checks the grudge: a zombie struck
// by a huntable player turns on them the next tick, ahead of any nearer
// stranger, and a strike from a mode it may not hunt leaves it idle.
func TestAZombieRetaliatesOnWhoeverStruckIt(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	z := hunter()
	attacker := survivor(7, 5.5)
	z.TakeDamage(1, attacker)
	players := []Player{attacker, survivor(8, 1.2)}
	for range 5 {
		z.Tick(w, players)
	}
	if z.TargetID != 7 {
		t.Errorf("a struck zombie targeted entity %d, want the one that hit it", z.TargetID)
	}
	if panicking(z) {
		t.Error("a struck zombie panicked, want the hostile mind to hold its ground")
	}
	// A punch from a creative player earns nothing: the mode may not be
	// hunted, so the grudge is dropped at the first look.
	struck := hunter()
	creative := Player{X: 5.5, Y: 64, Z: 0.5, EntityID: 3, Huntable: false}
	struck.TakeDamage(1, creative)
	players = []Player{creative}
	for range 5 {
		struck.Tick(w, players)
	}
	if struck.TargetID != 0 {
		t.Errorf("a zombie struck by an unhuntable player targeted entity %d, want nobody", struck.TargetID)
	}
}

// pillarFor stands the pillar of the searches: four blocks of stone at the
// cell given, on the floor the caller laid under it.
func pillarFor(t *testing.T, w testWorld, x, z int32) {
	t.Helper()
	stone := defaultState(t, "minecraft:stone")
	for y := int32(64); y <= 67; y++ {
		w.set(x, y, z, stone)
	}
}

// countingWorld is a test world that counts the block reads made against
// it, so a test can hold a hunt's searches to their cadence by the work
// they do alone.
type countingWorld struct {
	testWorld
	reads int
}

// BlockAt is the plain test world's read, counted on the way past.
func (w *countingWorld) BlockAt(x, y, z int32) uint16 {
	w.reads++
	return w.testWorld.BlockAt(x, y, z)
}

// BlocksIn counts every cell of the box as a read.
func (w *countingWorld) BlocksIn(x0, y0, z0, x1, y1, z1 int32, buf []uint16) []uint16 {
	return blocksVia(w.BlockAt, x0, y0, z0, x1, y1, z1, buf)
}

// huntReadBudget is the most block reads a hunt of a few hundred ticks
// against a target it has no way to may spend: a search reads the ground
// over all around itself, and MeleeAttackGoal's clock lets a hunt that
// keeps finding no way run one only every few dozen ticks — never one a
// tick, which would spend a search's worth of reads on every tick of the
// wait. The figure sits an order below the one-a-tick hunt and a wide
// margin above the cadence's handful of searches.
const huntReadBudget = 400000

func burnsWithin(w testWorld, z *Mob, ticks int) bool {
	for range ticks {
		z.Tick(w, nil)
		if z.FireTicks > 0 {
			return true
		}
	}
	return false
}

// leapGoal finds the leap goal a spider was wired with.
func leapGoal(t *testing.T, m *Mob) *leapAtTargetGoal {
	t.Helper()
	for _, g := range m.ai.goals {
		if l, ok := g.(*leapAtTargetGoal); ok {
			return l
		}
	}
	t.Fatal("a spider was wired without its leap goal")
	return nil
}

// leapsWithin offers the goal the start a hundred times and reports whether
// it ever took it.
func leapsWithin(g *leapAtTargetGoal, view *tickView) bool {
	for range 100 {
		if g.CanStart(view) {
			return true
		}
	}
	return false
}

// TestASpiderLeapsAtATargetTwoToFourBlocksAway checks LeapAtTargetGoal: from
// the ground, a target at three blocks gets the spider an upward and a
// forward velocity; closer than two, further than four, or in the air it
// stays put.
func TestASpiderLeapsAtATargetTwoToFourBlocksAway(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	for _, kind := range []MobType{TypeSpider, TypeCaveSpider} {
		for _, tc := range []struct {
			name     string
			dist     float64
			onGround bool
			want     bool
		}{
			{"three blocks", 3, true, true},
			{"under two blocks", 1.9, true, false},
			{"over four blocks", 4.1, true, false},
			{"in the air", 3, false, false},
		} {
			m := NewMob(kind, 4, 0.5, 64, 0.5, 21)
			m.TargetID = 7
			m.OnGround = tc.onGround
			g := leapGoal(t, m)
			view := &tickView{w: w, players: []Player{survivor(7, 0.5+tc.dist)}}
			if got := leapsWithin(g, view); got != tc.want {
				t.Fatalf("%v, %s: leap started = %v, want %v", kind, tc.name, got, tc.want)
			}
			if !tc.want {
				continue
			}
			g.Start(view)
			if m.VY != 0.4 || m.VX <= 0 || m.VZ != 0 {
				t.Errorf("%v: leap velocity = (%v, %v, %v), want upward 0.4 and forward along +x", kind, m.VX, m.VY, m.VZ)
			}
			if !g.ShouldContinue(view) {
				t.Errorf("%v: leap ended on its first tick", kind)
			}
		}
	}
}
