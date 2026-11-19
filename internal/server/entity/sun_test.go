package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The sun goals as ordinary unit tests, the way the hostile machinery's is:
// each case steps a skeleton against a world that is nothing but the blocks
// and the light it sets, with the players as the plain list the tick is
// handed, and asserts on the decisions vanilla's RestrictSunGoal and
// FleeSunGoal run on. The black-box test in internal/server/server_skeleton_test.go
// remains the proof of the flight over the wire.

// sunWorld is the world the sun tests run in: a stone floor under an open
// sky at noon, the daylight attribute of the burn on and the outside
// brightness with it, and the raw light's darkening at the clear day's
// nought.
func sunWorld() testWorld {
	w := newTestWorld()
	w.burn = true
	w.bright = true
	w.sky = 15
	w.darken = 0
	return w
}

// roofOver lays a stone roof two blocks over the floor of the world given,
// spanning the x and z ranges given, and shades the walkable cells under it
// the way the open sky's spread shades them: fourteen at the rim of the
// footprint, thirteen a step in, twelve beyond — and nothing brighter than
// the twelve anywhere deeper, the light the shade of it holds.
func roofOver(w testWorld, x0, x1, z0, z1 int32, t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			w.set(x, 66, z, stone)
		}
	}
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			switch {
			case x == x0 || x == x1 || z == z0 || z == z1:
				w.shade(x, 64, z, 14)
				w.shade(x, 65, z, 14)
			case x == x0+1 || x == x1-1 || z == z0+1 || z == z1-1:
				w.shade(x, 64, z, 13)
				w.shade(x, 65, z, 13)
			default:
				w.shade(x, 64, z, 12)
				w.shade(x, 65, z, 12)
			}
		}
	}
}

// hideSpot reports whether the feet cell given is one the flee would pick:
// shaded — no sky light of fifteen over it — and still bright, the
// walk-target value of a monster under zero only where the raw light is
// past the half the burn burns by.
func hideSpot(w testWorld, x, y, z int32) bool {
	return !canSeeSkyAt(w, x, y, z) && monsterWalkTargetValue(w, x, y, z) < 0
}

// TestABurningSkeletonFleesToTheShadeOfARoof checks the flight: a burning
// skeleton in the open daylight of a noon beside a roofed area finds itself
// one of the shaded bright cells under the roof's edge and walks there —
// inside the stroll's own hundred and twenty ticks of rest, so the walk is
// the flight's and nothing else's.
func TestABurningSkeletonFleesToTheShadeOfARoof(t *testing.T) {
	w := sunWorld()
	for x := int32(-14); x <= 20; x++ {
		for z := int32(-6); z <= 6; z++ {
			w.set(x, 63, z, defaultState(t, "minecraft:stone"))
		}
	}
	roofOver(w, 4, 8, -2, 2, t)

	s := NewMob(TypeSkeleton, 1, 0.5, 64, 0.5, 3)
	// The burn die is spared its roll: the skeleton is given the standing
	// fire a noon in the open leaves it with, the state FleeSunGoal's
	// canUse asks after.
	s.FireTicks = burnSeconds * 20

	inShade := false
	for range 120 {
		s.Tick(w, nil)
		c := s.cell()
		if c.X >= 4 && c.X <= 8 && c.Z >= -2 && c.Z <= 2 && hideSpot(w, c.X, c.Y, c.Z) {
			inShade = true
			break
		}
	}
	if !inShade {
		t.Fatalf("a burning skeleton beside the shade of a roof never took it, standing at (%v, %v, %v)",
			s.X, s.Y, s.Z)
	}
}

// TestASkeletonWithATargetOrWithoutFireNeverFlees checks the two gates the
// flight's canUse asks before its dice: a skeleton whose hunt holds a
// target rides the hunt and not the weather, and one the daylight has not
// lit stands where it is however bright the outside is.
func TestASkeletonWithATargetOrWithoutFireNeverFlees(t *testing.T) {
	w := sunWorld()
	for x := int32(-6); x <= 20; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 63, z, defaultState(t, "minecraft:stone"))
		}
	}
	roofOver(w, 12, 16, -2, 2, t)

	// The hunt first: no fire, so the flight's gate keeps it idle while
	// the target scan finds the player standing four blocks off.
	hunted := NewMob(TypeSkeleton, 1, 0.5, 64, 0.5, 3)
	players := []Player{{X: 4.5, Y: 64, Z: 0.5, EntityID: 7, Huntable: true}}
	for range 100 {
		hunted.Tick(w, players)
		if hunted.TargetID == 7 {
			break
		}
	}
	if hunted.TargetID != 7 {
		t.Fatalf("the skeleton never took the player as its target, want the hunt named before the sun's fire")
	}

	// Now the noon's fire, lit by hand the way the daylight would.
	hunted.FireTicks = burnSeconds * 20
	fled := false
	for range 200 {
		hunted.Tick(w, players)
		for _, g := range hunted.ai.running {
			if _, ok := g.(*fleeSunGoal); ok {
				fled = true
			}
		}
	}
	if fled {
		t.Error("a burning skeleton with a target fled the sun, want the hunt to hold it")
	}
	// The restriction runs whatever the hunt does: the bright outside has
	// the navigation's sun-avoiding up through the whole of the watch.
	if !hunted.nav.avoidSun {
		t.Error("a hunting skeleton's navigation was never told to avoid the sun, want the restriction's mode up")
	}

	// The unlit one without a target: the outside bright, the shade
	// beside it, and a world whose daylight burns nothing — the fire the
	// flight's gate reads never stands.
	calmWorld := sunWorld()
	calmWorld.burn = false
	for x := int32(-6); x <= 20; x++ {
		for z := int32(-4); z <= 4; z++ {
			calmWorld.set(x, 63, z, defaultState(t, "minecraft:stone"))
		}
	}
	roofOver(calmWorld, 8, 12, -2, 2, t)
	calm := NewMob(TypeSkeleton, 2, 0.5, 64, 0.5, 3)
	fled = false
	for range 200 {
		calm.Tick(calmWorld, nil)
		for _, g := range calm.ai.running {
			if _, ok := g.(*fleeSunGoal); ok {
				fled = true
			}
		}
	}
	if fled {
		t.Error("a skeleton the daylight never lit fled the sun, want it standing")
	}
	if !calm.nav.avoidSun {
		t.Error("an unlit skeleton's navigation was never told to avoid the sun, want the restriction's mode up")
	}
	if calm.FireTicks != 0 {
		t.Errorf("the unlit skeleton's fire ticks = %d, want the burn's gate to have kept it out", calm.FireTicks)
	}
}

// TestANightSkeletonNeverFleesTheSun checks the outside-brightness gate the
// flight's canUse asks before its fire: a skeleton on fire beside the lit
// shade of a roof stands where it is through the whole of a night, the dark
// outside keeping both the restriction's mode down and the flight's own
// start from firing. The shade is lit on purpose, so a hide spot the flight
// would take exists and the dark outside is the only thing holding it back.
func TestANightSkeletonNeverFleesTheSun(t *testing.T) {
	w := newTestWorld()
	w.sky = 15
	w.darken = 11 // the night's darkened sky
	w.block = 13  // the lamp that lights the shade under the roof
	w.bright = false
	for x := int32(-6); x <= 20; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 63, z, defaultState(t, "minecraft:stone"))
		}
	}
	roofOver(w, 4, 8, -2, 2, t)

	s := NewMob(TypeSkeleton, 1, 0.5, 64, 0.5, 3)
	// Lit by hand: the fire the flight's own gate reads stands however dark
	// the outside is, and the lamp-lit shade gives the dice a hide spot to
	// find, so the dark outside is the one thing left to stop the flight.
	s.FireTicks = burnSeconds * 20
	for range 200 {
		s.Tick(w, nil)
		for _, g := range s.ai.running {
			if _, ok := g.(*fleeSunGoal); ok {
				t.Fatalf("a skeleton on fire fled the sun in the night, standing at (%v, %v, %v)",
					s.X, s.Y, s.Z)
			}
		}
	}
	if s.nav.avoidSun {
		t.Error("the night told a skeleton's navigation to avoid the sun, want the dark outside to keep it off")
	}

	// The same walk in the same geometry under the noon: the twin flees, so
	// the night's standing still is the dark outside and not a square the
	// goal could not walk.
	day := sunWorld()
	for x := int32(-6); x <= 20; x++ {
		for z := int32(-4); z <= 4; z++ {
			day.set(x, 63, z, defaultState(t, "minecraft:stone"))
		}
	}
	roofOver(day, 4, 8, -2, 2, t)
	twin := NewMob(TypeSkeleton, 1, 0.5, 64, 0.5, 3)
	twin.FireTicks = burnSeconds * 20
	fled := false
	for range 120 {
		twin.Tick(day, nil)
		c := twin.cell()
		if c.X >= 4 && c.X <= 8 && c.Z >= -2 && c.Z <= 2 && hideSpot(day, c.X, c.Y, c.Z) {
			fled = true
			break
		}
	}
	if !fled {
		t.Fatal("the noon twin never took the shade, want the flight proved before the night's is denied")
	}
}

// TestAPathPlannedUnderTheSunIsCutWhereTheSkyStarts checks the
// restriction's trim: a skeleton standing in the shade of a roof, whose
// navigation the bright outside has told to avoid the sun, has every path
// it plans cut at the first node whose cell still sees the sky — while the
// same walk planned without the restriction runs on into the open.
func TestAPathPlannedUnderTheSunIsCutWhereTheSkyStarts(t *testing.T) {
	w := sunWorld()
	w.burn = false // the trim needs no fire, only the bright outside
	for x := int32(-14); x <= 14; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 63, z, defaultState(t, "minecraft:stone"))
		}
	}
	// The shaded west end the skeleton stands in, and the open east the
	// walk would run into.
	roofOver(w, -4, -1, -2, 2, t)

	// One tick of the bright outside: the restriction takes the
	// navigation's sun-avoiding up.
	s := NewMob(TypeSkeleton, 1, -2.5, 64, 0.5, 3)
	s.Tick(w, nil)
	if !s.nav.avoidSun {
		t.Fatal("a tick of the bright outside never told the navigation to avoid the sun")
	}

	// A stroll's worth of a walk due east, out of the shade into the open
	// sky.
	s.seekTo(w, 10.5, 64, 0.5, strollPace, 0)
	if len(s.nav.path) == 0 {
		t.Fatal("the shaded skeleton's walk was planned to nothing")
	}
	for _, node := range s.nav.path {
		if canSeeSkyAt(w, node.X, node.Y, node.Z) {
			t.Fatalf("the path was left running into the sun at (%d, %d, %d)", node.X, node.Y, node.Z)
		}
	}
	if last := s.nav.path[len(s.nav.path)-1]; last != (Waypoint{X: -1, Y: 64, Z: 0}) {
		t.Errorf("the trimmed walk ends at (%d, %d, %d), want the last shaded cell before the open",
			last.X, last.Y, last.Z)
	}

	// The same walk without the restriction: the fresh skeleton's path to
	// the same target crosses the shade line and reaches into the open,
	// which is the cutting the restriction's trim did.
	open := NewMob(TypeSkeleton, 2, -2.5, 64, 0.5, 3)
	open.seekTo(w, 10.5, 64, 0.5, strollPace, 0)
	crossed := false
	for _, node := range open.nav.path {
		if canSeeSkyAt(w, node.X, node.Y, node.Z) {
			crossed = true
		}
	}
	if !crossed || open.nav.path[len(open.nav.path)-1] != (Waypoint{X: 10, Y: 64, Z: 0}) {
		t.Error("the unrestrained walk never crossed into the sun, want the trim's work to show")
	}
}

// TestAFleeingSkeletonStaysUnderTheRoofItTook checks the flight's other
// edge: once the walk to the hide spot is spent, the skeleton whose feet
// stand shaded has no flight to make, and the restriction's trim holds
// every later stroll at the shade line — the shade the flee took is not
// given back up.
func TestAFleeingSkeletonStaysUnderTheRoofItTook(t *testing.T) {
	w := sunWorld()
	for x := int32(-6); x <= 20; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 63, z, defaultState(t, "minecraft:stone"))
		}
	}
	roofOver(w, 4, 8, -2, 2, t)

	s := NewMob(TypeSkeleton, 1, 0.5, 64, 0.5, 3)
	s.FireTicks = burnSeconds * 20
	arrived := false
	for range 120 {
		s.Tick(w, nil)
		c := s.cell()
		if c.X >= 4 && c.X <= 8 && c.Z >= -2 && c.Z <= 2 && hideSpot(w, c.X, c.Y, c.Z) {
			arrived = true
			break
		}
	}
	if !arrived {
		t.Fatal("the skeleton never took the shade, want the stay checked from its arrival")
	}

	// A burn that never ends — the fire of the rim of the shade the
	// skeleton stands in, whose light is past the half the burn burns by —
	// and a hundred ticks more of it: the skeleton stays under the roof.
	for range 100 {
		s.FireTicks = burnSeconds * 20
		s.Tick(w, nil)
		c := s.cell()
		if canSeeSkyAt(w, c.X, c.Y, c.Z) {
			t.Fatalf("the shaded skeleton walked back out into the sun at (%v, %v, %v)", s.X, s.Y, s.Z)
		}
	}
}

// TestAHelmetedSkeletonPaysTheSunNoMind checks the head slot the sun goals
// read: a skeleton wearing a helmet keeps its navigation sun-free and, on
// fire beside the shade a bare one runs to, stands where it is — the
// helmet standing the sun off it, the flight the naked head's alone.
func TestAHelmetedSkeletonPaysTheSunNoMind(t *testing.T) {
	w := sunWorld()
	for x := int32(-6); x <= 20; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 63, z, defaultState(t, "minecraft:stone"))
		}
	}
	roofOver(w, 4, 8, -2, 2, t)

	helmeted := NewMob(TypeSkeleton, 1, 0.5, 64, 0.5, 3)
	helmet, ok := registry.ItemID("minecraft:leather_helmet")
	if !ok {
		t.Fatal("the item registry has no leather helmet")
	}
	helmeted.Worn[wornHead] = protocol.ItemStack{ItemID: helmet, Count: 1}
	helmeted.FireTicks = burnSeconds * 20
	bare := NewMob(TypeSkeleton, 2, -3.5, 64, 0.5, 3)
	bare.FireTicks = burnSeconds * 20

	flees := func(s *Mob) bool {
		t.Helper()
		for range 120 {
			s.Tick(w, nil)
			for _, g := range s.ai.running {
				if _, ok := g.(*fleeSunGoal); ok {
					return true
				}
			}
		}
		return false
	}
	if !flees(bare) {
		t.Error("the burning bare skeleton never fled the sun, want the naked head's flight")
	}
	if helmeted.nav.avoidSun {
		t.Error("a helmeted skeleton was told to avoid the sun, want the helmet to keep the restriction off")
	}
	if flees(helmeted) {
		t.Error("a burning skeleton wearing a helmet fled the sun, want the helmet to keep the flight the bare head's")
	}
}
