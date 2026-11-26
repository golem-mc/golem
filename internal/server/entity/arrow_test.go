package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The arrow simulation as ordinary unit tests, driving the step over a
// world that is nothing but the blocks each case sets, the way the item's
// and the mob's own are. No server is involved; the black-box tests in
// internal/server/server_skeleton_test.go remain the proof of the whole life of an
// arrow — the bow that fires it, the bodies it hits, the player who picks
// it up — and these are the cheapest possible checks of the flight
// itself.

// TestAnArrowFliesWithGravityAndDrag fires an arrow flat along the x axis
// at the power a full draw gives and checks the flight against the
// arithmetic vanilla's tick runs: the velocity decayed by the air's drag
// and pulled down by the gravity, the position carried by the velocity
// the tick moved with.
func TestAnArrowFliesWithGravityAndDrag(t *testing.T) {
	w := newTestWorld()
	a := NewArrow(1, 0, 64, 0, 3, 0, 0, false, ArrowBaseDamage, PickupAllowed, 0, 3)

	a.Step(w, nil, nil)
	if !closeEnough(a.X, 3) {
		t.Errorf("after one tick the arrow stands at x=%v, want the three the velocity carried", a.X)
	}
	// The drag and the gravity of the tick that just moved: the velocity
	// the next tick moves with is the old one scaled by 0.99 with the
	// 0.05 of the fall taken off its y.
	if !closeEnough(a.VX, 3*0.99) {
		t.Errorf("the velocity after one tick = %v, want the drag's share of three", a.VX)
	}
	if !closeEnough(a.VY, -0.05) {
		t.Errorf("the fall after one tick = %v, want the gravity's own -0.05", a.VY)
	}
	if a.InGround {
		t.Error("an arrow in open air is in the ground")
	}
}

// TestAnArrowSticksInTheWallItHits fires an arrow at a wall of stone two
// blocks off and checks the stick: the flight stopped at the wall's face
// and drawn back by the offset onHitBlock leaves the shaft standing out
// with, the velocity dead, the in-ground flag up, and the shake wound for
// the animation the clients run on it.
func TestAnArrowSticksInTheWallItHits(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	w.set(3, 64, 0, stone)
	w.set(3, 65, 0, stone)

	a := NewArrow(1, 0, 64, 0, 3, 0, 0, true, ArrowBaseDamage, PickupAllowed, 0, 3)
	a.Step(w, nil, nil)

	if !a.InGround {
		t.Fatal("an arrow shot into a wall is not in the ground")
	}
	if a.VX != 0 || a.VY != 0 || a.VZ != 0 {
		t.Errorf("a stuck arrow moves at (%v %v %v), want the velocity dead", a.VX, a.VY, a.VZ)
	}
	if a.Crit {
		t.Error("a stuck arrow is critical, want the flag onHitBlock takes down")
	}
	// The position: the wall's face at x=3, less the offset the shaft
	// stands out of it by.
	if !closeEnough(a.X, 3-arrowStuckOffset) {
		t.Errorf("the stuck arrow stands at x=%v, want the face less the shaft's offset", a.X)
	}
	if a.ShakeTicks != arrowShakeTicks {
		t.Errorf("the stuck arrow shakes for %d ticks, want the vanilla seven", a.ShakeTicks)
	}
	// The life: a stuck arrow's ticks count toward the despawn.
	for range arrowLifeLimit {
		a.Step(w, nil, nil)
	}
	if !a.Dead {
		t.Fatal("a stuck arrow's life ran out without the arrow dying")
	}
}

// TestAnArrowCannotHitItsShooterAtFirst fires an arrow out of the body of
// its own shooter — every arrow's birth, the spawn sitting inside the
// shooter's box — and checks the exclusion: the flight's first ticks
// cannot touch them, the mark vanilla's leftOwner keeps turning on only
// once the box grown along the flight has carried clear of theirs, and
// an arrow turned about after that — the way a falling one's flight
// turns — may land on them.
func TestAnArrowCannotHitItsShooterAtFirst(t *testing.T) {
	w := newTestWorld()
	// The shooter's box around the arrow's birth: the spawn point at the
	// eye height is inside it.
	shooter := PlayerTarget(7, 0, 63, 0, false)
	a := NewArrow(1, 0, 64, 0, 3, 0, 0, false, ArrowBaseDamage, PickupAllowed, 7, 3)

	for range 3 {
		for _, ev := range a.Step(w, &shooter, []ArrowTarget{shooter}) {
			if ev.Kind == ArrowHitTarget && ev.Target.EntityID == 7 {
				t.Error("the arrow hit its own shooter on the flight out of them")
			}
		}
	}
	if !a.leftOwner {
		t.Fatal("an arrow three blocks clear of its shooter is still held unable to hit them")
	}

	// The flight turned about, the way a falling arrow's is, and the way
	// back crosses the shooter's body: with the mark set, the hit lands.
	a.VX = -3
	hit := false
	for range 5 {
		for _, ev := range a.Step(w, &shooter, []ArrowTarget{shooter}) {
			if ev.Kind == ArrowHitTarget && ev.Target.EntityID == 7 {
				hit = true
			}
		}
	}
	if !hit {
		t.Error("an arrow flown clear of its shooter and turned back never hit them")
	}
}

// TestAnArrowDamageScalesWithVelocity checks the damage the vanilla
// onHitEntity computes: the base damage over the velocity's length,
// ceil'd, with the critical roll on top of a critical arrow's.
func TestAnArrowDamageScalesWithVelocity(t *testing.T) {
	a := NewArrow(1, 0, 64, 0, 3, 0, 0, false, ArrowBaseDamage, PickupAllowed, 0, 3)
	if got := a.hitDamage(&ArrowTarget{}); got != 6 {
		t.Errorf("a full-draw arrow's damage = %v, want the ceil of two over three", got)
	}

	crit := NewArrow(2, 0, 64, 0, 3, 0, 0, true, ArrowBaseDamage, PickupAllowed, 0, 3)
	got := crit.hitDamage(&ArrowTarget{})
	if got < 6 || got > 10 {
		t.Errorf("a critical arrow's damage = %v, want the six of the base and the roll of up to four on top", got)
	}
	// The roll is the arrow's own random source's: the same seed replays
	// the same bonus.
	crit2 := NewArrow(3, 0, 64, 0, 3, 0, 0, true, ArrowBaseDamage, PickupAllowed, 0, 3)
	if again := crit2.hitDamage(&ArrowTarget{}); again != got {
		t.Errorf("the same seed's critical roll came to %v, want the %v the first came to", again, got)
	}
}

// TestAnArrowPicksUpOnlyWhereAllowed checks the pickup rule: an allowed
// arrow may be taken once it is in the ground and the shake has settled,
// a disallowed one never, and a creative-only one only by a creative
// player.
func TestAnArrowPicksUpOnlyWhereAllowed(t *testing.T) {
	a := NewArrow(1, 0, 64, 0, 0, 0, 0, false, ArrowBaseDamage, PickupAllowed, 0, 3)
	a.InGround = true
	if !a.CanPickUp(false) {
		t.Error("an allowed arrow in the ground may not be picked up")
	}
	// The shake still running: not yet.
	a.ShakeTicks = arrowShakeTicks
	if a.CanPickUp(false) {
		t.Error("an arrow mid-shake may be picked up")
	}
	a.ShakeTicks = 0

	a.Pickup = PickupDisallowed
	if a.CanPickUp(false) || a.CanPickUp(true) {
		t.Error("a disallowed arrow may be picked up")
	}

	a.Pickup = PickupCreativeOnly
	if a.CanPickUp(false) {
		t.Error("a creative-only arrow may be picked up by a survival player")
	}
	if !a.CanPickUp(true) {
		t.Error("a creative-only arrow may not be picked up by a creative player")
	}

	// Dead ends every rule: the despawn a stuck arrow's last tick runs
	// must not hand the shaft to a touch of the same tick.
	a.Dead = true
	if a.CanPickUp(true) {
		t.Error("a discarded arrow may be picked up")
	}
}

// TestAStuckArrowFallsWhenItsBlockIsGone fires an arrow into a wall and
// takes the wall away: the block that held the shaft gone, the next tick
// lets it go — out of the ground, its life reset, and its fall begun. The
// freeing tick itself is through, so the shaft does not move until the
// ticks after it.
func TestAStuckArrowFallsWhenItsBlockIsGone(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	w.set(3, 64, 0, stone)
	w.set(3, 65, 0, stone)

	a := NewArrow(1, 0, 64, 0, 3, 0, 0, false, ArrowBaseDamage, PickupAllowed, 0, 3)
	a.Step(w, nil, nil)
	if !a.InGround {
		t.Fatal("the arrow shot into the wall did not stick")
	}

	// The wall stands through these ticks: the shaft stays where it stuck.
	for range 5 {
		a.Step(w, nil, nil)
	}
	if !a.InGround {
		t.Fatal("a stuck arrow whose block still stands was let go")
	}

	// A push a stuck shaft carries in its dead velocity without moving:
	// once freed, the freeing tick must not carry it either.
	a.Push(1, 0, 0)

	w.set(3, 64, 0, 0)
	w.set(3, 65, 0, 0)
	x, y, z := a.X, a.Y, a.Z
	a.Step(w, nil, nil)
	if a.InGround {
		t.Fatal("the arrow whose block was removed is still in the ground")
	}
	if a.X != x || a.Y != y || a.Z != z {
		t.Errorf("the freed arrow moved on the tick it was freed: (%v, %v, %v) to (%v, %v, %v)",
			x, y, z, a.X, a.Y, a.Z)
	}
	if a.Life != 0 {
		t.Errorf("the freed arrow's life = %d, want the reset nought", a.Life)
	}
	// The fall begins on the ticks after: the first only lets the gravity
	// take the dead velocity, the next carries the shaft down.
	for range 2 {
		a.Step(w, nil, nil)
	}
	if a.Y >= y {
		t.Errorf("the freed arrow did not fall: y went %v then %v", y, a.Y)
	}
}

// TestStuckArrowIsPutOutByRainOrWater checks that an arrow lodged in a block
// loses its burn to rain or to water like a flying one.
func TestStuckArrowIsPutOutByRainOrWater(t *testing.T) {
	slab, ok := registry.FindState("minecraft:stone_slab", map[string]string{"type": "bottom", "waterlogged": "false"})
	wet, ok2 := registry.FindState("minecraft:stone_slab", map[string]string{"type": "bottom", "waterlogged": "true"})
	if !ok || !ok2 {
		t.Fatal("stone slab states not found")
	}
	rainy := newTestWorld()
	rainy.rain = true
	rainy.set(0, 64, 0, slab)
	dry := newTestWorld()
	dry.set(0, 64, 0, slab)
	water := newTestWorld()
	water.set(0, 64, 0, wet)
	cases := []struct {
		name string
		w    testWorld
		want int
	}{
		{"rain", rainy, 0},
		{"dry", dry, 100},
		{"water", water, 0},
	}
	for _, c := range cases {
		a := NewArrow(1, 0.5, 64.1, 0.5, 0, 0, 0, false, ArrowBaseDamage, PickupAllowed, 0, 3)
		a.InGround = true
		a.FireTicks = 100
		a.Step(c.w, nil, nil)
		if !a.InGround {
			t.Fatalf("%s: arrow left the block", c.name)
		}
		if a.FireTicks != c.want {
			t.Errorf("%s: FireTicks = %d, want %d", c.name, a.FireTicks, c.want)
		}
	}
}

// TestAPiercingArrowHitsTheNextBodyInTheSameTick lines two bodies up on the
// flight: both are hit in one Step, in order, each at its own point.
// vanilla: AbstractArrow.tick
func TestAPiercingArrowHitsTheNextBodyInTheSameTick(t *testing.T) {
	w := newTestWorld()
	a := NewArrow(1, 0, 64, 0, 6, 0, 0, false, ArrowBaseDamage, PickupAllowed, 0, 3)
	a.PierceLevel = 1
	near := ArrowTarget{EntityID: 7, MinX: 1, MinY: 63, MinZ: -1, MaxX: 2, MaxY: 66, MaxZ: 1}
	far := ArrowTarget{EntityID: 8, MinX: 4, MinY: 63, MinZ: -1, MaxX: 5, MaxY: 66, MaxZ: 1}
	events := a.Step(w, nil, []ArrowTarget{far, near})

	if len(events) != 2 || events[0].Target.EntityID != 7 || events[1].Target.EntityID != 8 {
		t.Fatalf("events = %+v, want hits on 7 then 8", events)
	}
	if events[0].X != 1 || events[1].X != 4 {
		t.Errorf("hit points x = %v, %v, want 1, 4", events[0].X, events[1].X)
	}
}

// TestAnArrowSinksSlowlyInDeepWater drops an arrow at rest in a column of
// still water: the arrow moves with the velocity from before the water's
// inertia, so the sinking settles at 0.05 / (1 - 0.6) = 0.125 blocks a tick.
// vanilla: AbstractArrow.tick
func TestAnArrowSinksSlowlyInDeepWater(t *testing.T) {
	w := newTestWorld()
	water := defaultState(t, "minecraft:water")
	for y := int32(40); y <= 70; y++ {
		w.set(0, y, 0, water)
	}
	a := NewArrow(1, 0.5, 66, 0.5, 0, 0, 0, false, ArrowBaseDamage, PickupAllowed, 0, 3)

	for range 29 {
		a.Step(w, nil, nil)
	}
	before := a.Y
	a.Step(w, nil, nil)
	if a.InGround || a.Dead {
		t.Fatal("the sinking arrow stopped in open water")
	}
	if fall := before - a.Y; math.Abs(fall-0.125) > 1e-3 {
		t.Errorf("between the 29th and 30th step the arrow sank %v, want 0.125", fall)
	}
}

// TestAnArrowFlyingThroughLavaOrFireIsSetAlight sends an unlit arrow through a
// lava cell and through a fire block; each leaves it burning, and the burn is
// what it hands a body it hits.
// vanilla: LavaFluid.entityInside, BaseFireBlock.entityInside
func TestAnArrowFlyingThroughLavaOrFireIsSetAlight(t *testing.T) {
	for _, tc := range []struct {
		block string
		ticks int
	}{{"minecraft:lava", 15 * 20}, {"minecraft:fire", 8 * 20}} {
		w := newTestWorld()
		w.set(0, 64, 0, defaultState(t, tc.block))
		a := NewArrow(1, 0.5, 63.5, 0.5, 0, 0.5, 0, false, ArrowBaseDamage, PickupAllowed, 0, 3)
		a.Step(w, nil, nil)
		if a.FireTicks <= 0 || a.FireTicks > tc.ticks || a.FireTicks < tc.ticks-2 {
			t.Errorf("%s: arrow burns %d ticks, want about %d", tc.block, a.FireTicks, tc.ticks)
		}
		if a.igniteForTicks() != 100 {
			t.Errorf("%s: a burning arrow ignites its target for %d ticks, want 100", tc.block, a.igniteForTicks())
		}
	}
}

// TestAnArrowBornInsideABlockDoesNotShakeOnItsFirstTick checks that an arrow
// summoned inside stone is in the ground after its first tick with no shake,
// and so can be picked up, while one stuck later does shake.
func TestAnArrowBornInsideABlockDoesNotShakeOnItsFirstTick(t *testing.T) {
	w := newTestWorld()
	w.set(0, 64, 0, defaultState(t, "minecraft:stone"))

	a := NewArrow(1, 0.5, 64.52, 0.5, 0, 0, 0, false, ArrowBaseDamage, PickupAllowed, 0, 3)
	a.Step(w, nil, nil)
	if !a.InGround {
		t.Fatal("an arrow inside stone is not in the ground")
	}
	if a.ShakeTicks != 0 {
		t.Errorf("first-tick shake = %d, want 0", a.ShakeTicks)
	}
	if !a.CanPickUp(false) {
		t.Error("an arrow that began inside a block waits out a shake before pickup")
	}
}
