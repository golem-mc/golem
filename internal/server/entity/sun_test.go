package entity

import (
	"testing"
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
