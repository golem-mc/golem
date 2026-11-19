package entity

import (
	"math/rand"
	"testing"
)

// The orb simulation as ordinary unit tests, driving the step over the same
// plain world the item tests use. The black-box tests in internal/server/ remain the
// proof of the orb's whole life; these are the cheap checks of the fluid and
// burn behaviour the vanilla tick gives it.

// TestAnOrbInDeepWaterRisesOnVanillasDrift sinks an orb to the bottom of a
// two-deep pool and steps it: the eye is in the water, so the drift carries
// it upward at the pace the vanilla recurrence gives, checked exactly for
// the first three ticks — the rise applied to the whole velocity, not the
// drag-shrunk one — and by the surface for the float.
func TestAnOrbInDeepWaterRisesOnVanillasDrift(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	w.set(0, 62, 0, stone)
	w.set(0, 63, 0, water)
	w.set(0, 64, 0, water)

	o := NewOrb(7, 0.5, 63, 0.5, 0, 0, 0, 3, 1)
	o.Rand = rand.New(rand.NewSource(1))
	o.Step(w, nil, nil)
	if !closeEnough(o.VY, fluidRise*orbAirDrag) {
		t.Errorf("vy after one tick in water = %v, want %v (the rise through the air's drag)", o.VY, fluidRise*orbAirDrag)
	}
	if !closeEnough(o.Y, 63+fluidRise) {
		t.Errorf("y after one tick in water = %v, want %v (carried by the rise)", o.Y, 63+fluidRise)
	}
	// The second and third ticks of the recurrence, written out: the rise
	// adds to the whole of the velocity the drag left, which is what the
	// vanilla drift is and what a drag-first reading is not.
	vyTwo := (fluidRise + fluidRise*orbAirDrag) * orbAirDrag
	o.Step(w, nil, nil)
	if !closeEnough(o.VY, vyTwo) {
		t.Errorf("vy after two ticks in water = %v, want %v", o.VY, vyTwo)
	}
	vyThree := (fluidRise + vyTwo) * orbAirDrag
	o.Step(w, nil, nil)
	if !closeEnough(o.VY, vyThree) {
		t.Errorf("vy after three ticks in water = %v, want %v", o.VY, vyThree)
	}

	high := o.Y
	for range 300 {
		o.Step(w, nil, nil)
		if o.Y > high {
			high = o.Y
		}
	}
	if high < 63.75 {
		t.Errorf("the highest of the orb's 300 ticks = %v, want the bob up at the surface the drift carries it to", high)
	}
}

// TestAnOrbWhoseEyesAreAboveTheWaterSinks drops an orb into the last sliver
// of a one-deep pool, where its feet are in the water and its eyes are not:
// the drift is the eye test's to give, so this one falls to the floor of the
// pool before its eyes are under and the float begins.
func TestAnOrbWhoseEyesAreAboveTheWaterSinks(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	water := defaultState(t, "minecraft:water")
	w := newTestWorld()
	w.set(0, 62, 0, stone)
	w.set(0, 63, 0, water)

	o := NewOrb(7, 0.5, 63.7, 0.5, 0, 0, 0, 3, 1)
	o.Rand = rand.New(rand.NewSource(1))
	o.Step(w, nil, nil)
	if o.VY >= 0 {
		t.Errorf("vy after one tick with dry eyes = %v, want the gravity of an orb whose eyes are above the water", o.VY)
	}
	if o.Y >= 63.7 {
		t.Errorf("y after one tick with dry eyes = %v, want it sinking", o.Y)
	}
	// The fall carries it to the floor of the pool, where its eyes go
	// under and the drift takes over: from there it rises again.
	low := o.Y
	for range 120 {
		o.Step(w, nil, nil)
		if o.Y < low {
			low = o.Y
		}
	}
	if low >= 63.2 {
		t.Errorf("the lowest of the orb's 120 ticks = %v, want the dip down to the pool's floor", low)
	}
	high := o.Y
	for range 200 {
		o.Step(w, nil, nil)
		if o.Y > high {
			high = o.Y
		}
	}
	if high < 63.4 {
		t.Errorf("the highest of the orb's 200 ticks = %v, want the float its underwater eyes give it", high)
	}
}

// TestAnOrbBesideAFencePostFallsToTheFloor drops an orb inside a fence
// post's own cell but clear of the four-pixel post: the gravity gate reads
// the post's shape, not the whole cell, so the orb falls past it to the
// floor below.
func TestAnOrbBesideAFencePostFallsToTheFloor(t *testing.T) {
	w := newTestWorld()
	w.set(0, 62, 0, defaultState(t, "minecraft:stone"))
	w.set(0, 63, 0, defaultState(t, "minecraft:oak_fence"))

	o := NewOrb(7, 0.05, 64, 0.5, 0, 0, 0, 3, 1)
	o.Rand = rand.New(rand.NewSource(1))
	for range 100 {
		o.Step(w, nil, nil)
	}
	if !closeEnough(o.Y, 63) || !o.OnGround {
		t.Errorf("the orb beside the post rests at %v (on ground %v), want 63 on the floor", o.Y, o.OnGround)
	}
}

// TestANoGravityOrbBouncesOnASmallFall lands a NoGravity orb on the floor
// with a downward speed smaller than the normal 0.03: the bounce compares
// the fall against Entity.getGravity, which is nought under the flag, so
// the small fall bounces here and would not for a body still acted on.
func TestANoGravityOrbBouncesOnASmallFall(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	w.set(0, 62, 0, stone)

	o := NewOrb(7, 0.5, 63, 0.5, 0, -0.02, 0, 3, 1)
	o.Rand = rand.New(rand.NewSource(1))
	o.NoGravity = true
	o.Step(w, nil, nil)
	if !closeEnough(o.VY, 0.02*orbBounceFactor) {
		t.Errorf("vy after the no-gravity orb landed = %v, want %v (the bounce of the small fall)", o.VY, 0.02*orbBounceFactor)
	}
}

// TestAnOrbInLavaBouncesUpAndBurnsUp drops an orb into a pool of lava: the
// fluid at its feet throws it upward on the vanilla bounce, the lava hurts
// it four points a tick, and the five it is given are spent by the second —
// each hurt playing the burn sound the vanilla orb asks for.
func TestAnOrbInLavaBouncesUpAndBurnsUp(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	lava := defaultState(t, "minecraft:lava")
	w := newTestWorld()
	w.set(0, 62, 0, stone)
	w.set(0, 63, 0, lava)

	o := NewOrb(7, 0.5, 63, 0.5, 0, 0, 0, 3, 1)
	o.Rand = rand.New(rand.NewSource(1))
	events := o.Step(w, nil, nil)
	if !closeEnough(o.VY, float64(float32(0.2))*orbAirDrag) {
		t.Errorf("vy after one tick in lava = %v, want the bounce's two tenths through the air's drag", o.VY)
	}
	if !closeEnough(o.Y, 63+float64(float32(0.2))) {
		t.Errorf("y after one tick in lava = %v, want it thrown up off the floor", o.Y)
	}
	if o.Health != 1 {
		t.Errorf("health after one tick in lava = %d, want 1 (four of the five spent)", o.Health)
	}
	var burned bool
	for _, ev := range events {
		if ev.Kind == OrbBurned {
			burned = true
		}
		if ev.Kind == OrbExpired {
			t.Error("the first tick of lava spent the orb, want it holding on with one point left")
		}
	}
	if !burned {
		t.Error("the first tick of lava played no burn, want the sound the vanilla orb asks for of every hurt")
	}

	events = o.Step(w, nil, nil)
	if !o.Dead {
		t.Fatal("the orb survived two ticks of lava, want it burned up")
	}
	var expired bool
	burned = false
	for _, ev := range events {
		switch ev.Kind {
		case OrbExpired:
			expired = true
		case OrbBurned:
			burned = true
		}
	}
	if !expired {
		t.Error("the orb's death by lava carried no expiry, want the body taken out of the world")
	}
	if !burned {
		t.Error("the orb's death by lava played no burn, want the fizz vanilla plays for it")
	}
}

// TestAnOrbStuckInABlockIsPushedOutTowardsTheNearestFreeSide buries an orb
// in a stone cell with no follower in sight: the vanilla tick answers a body
// whose next move would still cross a block with the moveTowardsClosestSpace
// push, so sixty ticks walk it out of the cell.
func TestAnOrbStuckInABlockIsPushedOutTowardsTheNearestFreeSide(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	w.set(0, 64, 0, stone)

	o := NewOrb(7, 0.5, 64.2, 0.5, 0, 0, 0, 3, 1)
	o.Rand = rand.New(rand.NewSource(1))
	for range 60 {
		o.Step(w, nil, nil)
	}
	if o.colliding(w) {
		t.Errorf("the orb is still inside the stone after sixty ticks, at (%v, %v, %v)", o.X, o.Y, o.Z)
	}
}

// TestAnOrbFollowsByFeetDistance puts a player 7.98 blocks aside with the feet
// 0.7 below the orb: the feet are out of the eight blocks though the middle
// height (0.81 up) is within, and the follow measures the feet.
func TestAnOrbFollowsByFeetDistance(t *testing.T) {
	players := []OrbPlayer{{X: 7.98, Y: 63.3, Z: 0, EntityID: 1}}
	o := NewOrb(7, 0, 64, 0, 0, 0, 0, 3, 1)
	o.followNearbyPlayer(players)
	if o.VX != 0 || o.followingKnown {
		t.Errorf("the orb follows a player whose feet stand out of range (vx %v)", o.VX)
	}

	o.followingKnown, o.followingID = true, 1
	o.followNearbyPlayer(players)
	if o.VX != 0 || o.followingKnown {
		t.Errorf("the orb keeps a follower whose feet stand out of range (vx %v)", o.VX)
	}

	players[0].Y = 64
	o.followNearbyPlayer(players)
	if o.VX <= 0 {
		t.Errorf("the orb ignores a player whose feet stand within range (vx %v)", o.VX)
	}
}

// TestAClosestDeadPlayerStopsTheFollow checks that the nearest player is
// picked with the dead included and then refused, so a live player further
// off is not followed, while a dead incumbent stays followed.
func TestAClosestDeadPlayerStopsTheFollow(t *testing.T) {
	players := []OrbPlayer{
		{X: 2, Y: 64, EntityID: 1, Dead: true},
		{X: -4, Y: 64, EntityID: 2},
	}
	o := NewOrb(7, 0, 64, 0, 0, 0, 0, 3, 1)
	o.followNearbyPlayer(players)
	if o.VX != 0 || o.followingKnown {
		t.Errorf("a live player behind a dead one is followed (vx %v)", o.VX)
	}

	o.followingKnown, o.followingID = true, 1
	o.followNearbyPlayer(players)
	if o.VX <= 0 {
		t.Errorf("a dead incumbent is dropped (vx %v)", o.VX)
	}
}

func TestOrbMergeScanGrowsOnlyTheScanningBox(t *testing.T) {
	a := &Orb{X: 0, Y: 64, Z: 0}
	for _, c := range []struct {
		dx   float64
		want bool
	}{{0.9, true}, {1.2, false}} {
		b := &Orb{X: c.dx, Y: 64, Z: 0}
		if got := orbBoxesTouch(a, b); got != c.want {
			t.Errorf("dx=%v: orbBoxesTouch = %v, want %v", c.dx, got, c.want)
		}
	}
}

// TestAnOrbAimsAtHalfTheViewsEyeHeight checks the follow aims at half the
// followed player's own eye height, not a standing 1.62.
// vanilla: ExperienceOrb.tick
func TestAnOrbAimsAtHalfTheViewsEyeHeight(t *testing.T) {
	vy := func(eye float64) float64 {
		o := NewOrb(7, 0, 64, 0, 0, 0, 0, 3, 1)
		o.followNearbyPlayer([]OrbPlayer{{X: 2, Y: 64, Z: 0, EntityID: 1, EyeHeight: eye}})
		return o.VY
	}
	if short, standing := vy(0.4), vy(1.62); short == standing {
		t.Errorf("VY = %v for eye height 0.4 and %v for 1.62, want different", short, standing)
	}
	if zero, standing := vy(0), vy(1.62); zero != standing {
		t.Errorf("VY = %v for a zero eye height, want the standing %v", zero, standing)
	}
}
