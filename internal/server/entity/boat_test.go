package entity

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The boat simulation as ordinary unit tests, driving the step over a
// world that is nothing but the blocks each case sets. No server is
// involved; the black-box tests in internal/server/boat_test.go remain the proof of
// the whole life of a boat — laying, carrying, breaking — and these are
// the cheapest possible checks of the simulation itself.

// boatWater is the block state a test world's pool is filled with, the
// source level the floats rest on.
var boatWater = func() uint16 {
	state, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		panic("no water state")
	}
	return state
}()

func boatStone(t *testing.T) uint16 {
	t.Helper()
	state, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("no stone state")
	}
	return state
}

// boatRest is where a hull floats at rest on water whose surface is at
// surfaceY: 0.65 of a hull below it, the depth the water's lift and the
// hull's gravity balance at.
func boatRest(surfaceY float64) float64 {
	return surfaceY - 0.65*BoatHeight
}

// TestAnUnriddenBoatFloatsUpToJustUnderTheSurfaceAndStaysThere: a boat set
// into the water near the surface settles just under it — the depth the
// water's lift and the hull's gravity balance at — bobbing out its dip and
// resting still.
// vanilla: AbstractBoat.floatBoat
func TestAnUnriddenBoatFloatsUpToJustUnderTheSurfaceAndStaysThere(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	for z := int32(-1); z <= 1; z++ {
		for x := int32(-1); x <= 1; x++ {
			for y := int32(0); y <= 3; y++ {
				w.set(x, y, z, boatWater)
			}
		}
	}
	const surface = 3.0 + 8.0/9.0
	// Set just under the surface, so the hull is part in the water rather
	// than whole under it — a boat whole under sinks, its flat lift less
	// than gravity.
	b := NewBoat(1, 0.5, surface-0.1, 0.5, 0)
	for range 200 {
		b.Step(w, false)
		if b.Dead {
			t.Fatalf("the boat broke floating at y=%v", b.Y)
		}
	}
	if math.Abs(b.Y-boatRest(surface)) > 0.02 {
		t.Errorf("after two hundred ticks the boat rests at y=%v, want the balance %v", b.Y, boatRest(surface))
	}
	if math.Abs(b.VY) > 0.01 {
		t.Errorf("the afloat boat still runs %v up a tick, want the stillness the damping gives", b.VY)
	}
}

// TestANoGravityBoatHangsInTheAir: a hull tagged with NoGravity drops by no
// gravity term, so forty ticks leave it at the height it was laid at.
// vanilla: AbstractBoat.tick, Entity.getGravity
func TestANoGravityBoatHangsInTheAir(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	b := NewBoat(1, 0.5, 70, 0.5, 0)
	b.NoGravity = true
	for range 40 {
		b.Step(w, false)
		if b.Dead {
			t.Fatalf("the boat broke hanging at y=%v", b.Y)
		}
	}
	if math.Abs(b.Y-70) > 0.001 {
		t.Errorf("the no-gravity boat fell from y=70 to y=%v over forty ticks, want it hanging", b.Y)
	}
}

// TestANoGravityBoatUsesNoGravityInTheWaterToo: the tag takes the gravity
// term out of both water floats as well as the air's, so a gravity-less hull
// whole under the water drifts up on the flat lift and one part in on the
// depth's, where it would otherwise sink by the 0.04 a tick.
// vanilla: AbstractBoat.floatBoat's UNDER_WATER and IN_WATER, Entity.getGravity
func TestANoGravityBoatUsesNoGravityInTheWaterToo(t *testing.T) {
	t.Parallel()
	// Whole under the water: the flat lift alone.
	under := newTestWorld()
	for z := int32(-1); z <= 1; z++ {
		for x := int32(-1); x <= 1; x++ {
			for y := int32(0); y <= 5; y++ {
				under.set(x, y, z, boatWater)
			}
		}
	}
	b := NewBoat(1, 0.5, 1.5, 0.5, 0)
	b.NoGravity = true
	b.Step(under, false)
	want := boatUnderWaterLift * boatGravity / boatBuoyancyDiv * boatWaterDamp
	if math.Abs(b.VY-want) > 1e-9 {
		t.Errorf("the no-gravity hull whole under the water runs %v a tick, want the lift %v", b.VY, want)
	}

	// Part in the water: the depth's lift alone.
	part := newTestWorld()
	const surface = 3.0 + 8.0/9.0
	for z := int32(-1); z <= 1; z++ {
		for x := int32(-1); x <= 1; x++ {
			for y := int32(0); y <= 3; y++ {
				part.set(x, y, z, boatWater)
			}
		}
	}
	b = NewBoat(1, 0.5, surface-0.1, 0.5, 0)
	b.NoGravity = true
	b.Step(part, false)
	want = b.water.height / BoatHeight * boatGravity / boatBuoyancyDiv * boatWaterDamp
	if math.Abs(b.VY-want) > 1e-9 {
		t.Errorf("the no-gravity hull part in the water runs %v a tick, want the depth's lift %v", b.VY, want)
	}
}

// TestABoatWholeUnderTheWaterSinks: a hull the water closes whole over
// loses the depth's lift for the flat hundredth vanilla's UNDER_WATER branch
// keeps, less than gravity, so it sinks rather than climbing to the surface.
// vanilla: AbstractBoat.floatBoat's UNDER_WATER
func TestABoatWholeUnderTheWaterSinks(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	for z := int32(-1); z <= 1; z++ {
		for x := int32(-1); x <= 1; x++ {
			for y := int32(0); y <= 5; y++ {
				w.set(x, y, z, boatWater)
			}
		}
	}
	b := NewBoat(1, 0.5, 1.5, 0.5, 0)
	start := b.Y
	for range 20 {
		b.Step(w, false)
	}
	if b.Y >= start {
		t.Errorf("the hull whole under the water went from %v to %v, want it sinking", start, b.Y)
	}
}

func TestABoatCreatedUnderWaterStaysOnBottom(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	floor(w, -1, -1, 1, 1, 0, boatStone(t))
	floor(w, -1, -1, 1, 1, 1, boatWater)
	b := NewBoat(1, 0.5, 1, 0.5, 0)
	for tick := range 40 {
		b.Step(w, false)
		if b.Y != 1 || !b.OnGround || b.VY != 0 {
			t.Fatalf("tick %d: y=%g vy=%g grounded=%v; want y=1, vy=0 on bottom", tick+1, b.Y, b.VY, b.OnGround)
		}
	}
}

func TestWaterCoveringABeachedBoatDoesNotSnap(t *testing.T) {
	t.Parallel()
	flowing, ok := registry.WithInt(boatWater, registry.PropLevel, 1)
	if !ok {
		t.Fatal("no flowing water state")
	}
	for _, tc := range []struct {
		name   string
		ridden bool
		water  uint16
	}{
		{"source", false, boatWater},
		{"flowing", false, flowing},
		{"ridden-source", true, boatWater},
		{"ridden-flowing", true, flowing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			floor(w, -1, -1, 1, 1, 0, boatStone(t))
			b := NewBoat(1, 0.5, 1, 0.5, 0)
			b.Step(w, tc.ridden)
			if b.lastStatus != boatOnLand {
				t.Fatalf("beached status=%v, want on land", b.lastStatus)
			}
			floor(w, -1, -1, 1, 1, 1, tc.water)
			for tick := range 40 {
				b.Step(w, false)
				if b.Y != 1 || !b.OnGround {
					t.Fatalf("tick %d: y=%g grounded=%v; want beached at y=1", tick+1, b.Y, b.OnGround)
				}
			}
		})
	}
}

// TestABoatWholeUnderARunningFlowSinksSlowly: a hull the running water
// closes over loses the depth's lift too, the flat 7e-4 gravity the
// UNDER_FLOWING_WATER branch keeps in its place, with a tenth off each
// side rather than the still water's half again.
// vanilla: AbstractBoat.floatBoat's UNDER_FLOWING_WATER
func TestABoatWholeUnderARunningFlowSinksSlowly(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	flowing, ok := registry.FindState("minecraft:water", map[string]string{"level": "1"})
	if !ok {
		t.Fatal("no flowing water state")
	}
	for z := int32(-1); z <= 1; z++ {
		for x := int32(-1); x <= 1; x++ {
			for y := int32(0); y <= 5; y++ {
				w.set(x, y, z, flowing)
			}
		}
	}
	b := NewBoat(1, 0.5, 1.5, 0.5, 0)
	b.VX, b.VZ = 1, 1
	b.Step(w, false)
	if math.Abs(b.VY+boatUnderFlowingSink) > 1e-9 {
		t.Errorf("the hull under the running flow runs %v down a tick, want the flat %v", b.VY, -boatUnderFlowingSink)
	}
	if math.Abs(b.VX-0.9) > 1e-9 || math.Abs(b.VZ-0.9) > 1e-9 {
		t.Errorf("the running flow left the hull's drift at (%v %v), want a tenth off each", b.VX, b.VZ)
	}
}

// TestABoatUnderSourceWaterOverARunningColumnStaysStillWater: the status
// reads only the slab over the hull's ceiling, so source water there settles
// a hull whose feet stand in a falling column — the still water's half-again
// drag, not the running flow's tenth. A whole-pool flow cannot tell the two
// apart; this mixes them.
// vanilla: AbstractBoat.isUnderwater
func TestABoatUnderSourceWaterOverARunningColumnStaysStillWater(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	falling, ok := registry.FindState("minecraft:water", map[string]string{"level": "8"})
	if !ok {
		t.Fatal("no falling water state")
	}
	for z := int32(-1); z <= 1; z++ {
		for x := int32(-1); x <= 1; x++ {
			// A running column at the hull's feet, source water over its
			// ceiling.
			w.set(x, 0, z, falling)
			w.set(x, 1, z, falling)
			for y := int32(2); y <= 5; y++ {
				w.set(x, y, z, boatWater)
			}
		}
	}
	b := NewBoat(1, 0.5, 1.5, 0.5, 0)
	b.VX = 1
	b.Step(w, false)
	if math.Abs(b.VX-boatUnderWaterFriction) > 1e-9 {
		t.Errorf("the hull under source water ran %v a tick, want the still water's %v", b.VX, boatUnderWaterFriction)
	}
	if b.VY >= 0 {
		t.Errorf("the hull under source water rises at %v a tick, want the still water's sink", b.VY)
	}
}

// TestABoatKeepsItsGlideOnIce: the ground branch's drag is the slipperiness
// of the blocks under the hull, so a hull sliding over ice keeps 0.98 of its
// drift a tick where a hull on stone keeps six tenths.
// vanilla: AbstractBoat.getGroundFriction
func TestABoatKeepsItsGlideOnIce(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	ice, ok := registry.FindState("minecraft:ice", nil)
	if !ok {
		t.Fatal("no ice state")
	}
	for z := int32(-2); z <= 2; z++ {
		for x := int32(-2); x <= 2; x++ {
			w.set(x, 62, z, ice)
		}
	}
	b := NewBoat(1, 0.5, 63, 0.5, 0)
	b.VX = 1
	b.Step(w, false)
	if math.Abs(b.VX-0.9799999594688416) > 1e-9 {
		t.Errorf("the hull on the ice still runs %v a tick, want the ice's 0.98 kept", b.VX)
	}

	// The same drift on stone loses the block's own six tenths.
	stone := newTestWorld()
	stoneBlock := boatStone(t)
	for z := int32(-2); z <= 2; z++ {
		for x := int32(-2); x <= 2; x++ {
			stone.set(x, 62, z, stoneBlock)
		}
	}
	b = NewBoat(1, 0.5, 63, 0.5, 0)
	b.VX = 1
	b.Step(stone, false)
	if math.Abs(b.VX-0.5999999642372131) > 1e-9 {
		t.Errorf("the hull on the stone still runs %v a tick, want the block's 0.6 kept", b.VX)
	}
}

// TestABoatOverALilyPadTakesTheAirDrag: a slab whose only shape is a lily
// pad leaves the hull in the air drag — no shape left to land on — not the
// land drag.
// vanilla: AbstractBoat.getGroundFriction, getNearbySlipperiness
func TestABoatOverALilyPadTakesTheAirDrag(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	pad, ok := registry.FindState("minecraft:lily_pad", nil)
	if !ok {
		t.Fatal("no lily pad state")
	}
	for z := int32(-2); z <= 2; z++ {
		for x := int32(-2); x <= 2; x++ {
			w.set(x, 62, z, pad)
		}
	}
	b := NewBoat(1, 0.5, 63, 0.5, 0)
	b.OnGround = true
	b.VX = 1
	b.Step(w, false)
	if math.Abs(b.VX-boatAirFriction) > 1e-9 {
		t.Errorf("the hull over the lily pad runs %v a tick, want the air drag %v", b.VX, boatAirFriction)
	}
}

// TestABoatComingOffTheAirPlungesJustUnderTheSurface: a boat that falls
// into the water is snapped to just under the surface the tick it first
// touches it — the whole hull less the hundredth-and-a-thousandth
// vanilla's splash-down leaves above it — and bobs back up to its rest
// from there.
// vanilla: AbstractBoat.tick's IN_AIR → IN_WATER
func TestABoatComingOffTheAirPlungesJustUnderTheSurface(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	for z := int32(-1); z <= 1; z++ {
		for x := int32(-1); x <= 1; x++ {
			for y := int32(0); y <= 3; y++ {
				w.set(x, y, z, boatWater)
			}
		}
	}
	const surface = 3.0 + 8.0/9.0
	b := NewBoat(1, 0.5, 10, 0.5, 0)
	// The fall runs until the hull is about to touch the water; the next
	// tick is the splash-down.
	for i := 0; i < 200 && b.Y > surface; i++ {
		b.Step(w, false)
	}
	if b.lastStatus != boatInAir {
		t.Fatal("the boat never came off the air into the water")
	}
	b.Step(w, false)
	// The hull's ceiling is already above the water cell, so the scan starts
	// at y=4 rather than at the fluid surface below it.
	const entryY = 4 - BoatHeight + 0.101
	if math.Abs(b.Y-entryY) > 1e-6 || b.VY != 0 || b.lastStatus != boatInWater {
		t.Errorf("the splash-down left the boat at y=%v, want the plunge at %v",
			b.Y, entryY)
	}
	// From the plunge the hull bobs back to its rest.
	for range 200 {
		b.Step(w, false)
	}
	if math.Abs(b.Y-boatRest(surface)) > 0.02 {
		t.Errorf("after two hundred ticks the boat rests at y=%v, want the balance %v", b.Y, boatRest(surface))
	}
}

func TestAFastFallingBoatFindsTheSurfaceAbove(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	for y := int32(0); y <= 3; y++ {
		floor(w, -1, -1, 1, 1, y, boatWater)
	}
	b := NewBoat(1, 0.5, 10, 0.5, 0)
	b.VY = -8
	b.Step(w, false)
	if b.Y >= 3 || b.lastStatus != boatInAir {
		t.Fatalf("fall y=%g status=%v, want below the top water cell from air", b.Y, b.lastStatus)
	}
	b.Step(w, false)
	want := 3 + fluidOwnHeight(fluidCellOf(boatWater)) - boatEntryDepth
	if math.Abs(b.Y-want) > 1e-6 || b.VY != 0 || b.lastStatus != boatInWater {
		t.Fatalf("entry y=%g vy=%g status=%v; want y=%g vy=0 in water", b.Y, b.VY, b.lastStatus, want)
	}
}

func TestABoatWaterEntrySnapChecksCollision(t *testing.T) {
	t.Parallel()
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprintf("blocked=%v", blocked), func(t *testing.T) {
			w := newTestWorld()
			b := NewBoat(1, 0.5, 2, 0.5, 0)
			b.NoGravity = true
			b.Step(w, false)
			b.VY = -0.25
			for y := int32(0); y <= 3; y++ {
				floor(w, -1, -1, 1, 1, y, boatWater)
			}
			want := 4 - boatEntryDepth
			wantVY := 0.0
			if blocked {
				w.set(1, 3, 0, boatStone(t))
				want, wantVY = 1.75, -0.25
			}
			b.Step(w, false)
			if math.Abs(b.Y-want) > 1e-6 || b.VY != wantVY || b.lastStatus != boatInWater {
				t.Fatalf("entry y=%g vy=%g status=%v; want y=%g vy=%g in water", b.Y, b.VY, b.lastStatus, want, wantVY)
			}
		})
	}
}

// TestABoatFallsAndLandsLikeAnythingElse: off the water a boat falls the
// gravity every body falls by and rests on the ground it reaches, the box
// sitting on the block's top.
func TestABoatFallsAndLandsLikeAnythingElse(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	stone := boatStone(t)
	for z := int32(-1); z <= 1; z++ {
		for x := int32(-1); x <= 1; x++ {
			w.set(x, 62, z, stone)
		}
	}
	b := NewBoat(1, 0.5, 64, 0.5, 0)
	for range 40 {
		b.Step(w, false)
	}
	if !b.OnGround {
		t.Error("the boat never came to rest on the ground")
	}
	if b.Y != 63 {
		t.Errorf("the boat rests at y=%v, want the block's top at 63", b.Y)
	}
	if b.VY != 0 {
		t.Errorf("the landed boat still runs %v a tick, want the stillness of a rest", b.VY)
	}
}

// TestABoatStepsTheLargerHorizontalAxisFirst: a diagonal move runs the
// horizontal axes in Direction.axisStepOrder's larger-first order, so a hull
// whose longer way passes the end of a wall clears it where stepping the
// smaller axis first would have stopped it short.
// vanilla: Entity.collideWithShapes, Direction.axisStepOrder
func TestABoatStepsTheLargerHorizontalAxisFirst(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	// One block east of the hull's start, its near face at x=1 and its
	// z span 0..1.
	w.set(1, 64, 0, boatStone(t))
	// The hull south of that span, its way north twice its way east, so
	// the z step runs first and carries it past the wall before x is read.
	b := NewBoat(1, 0.0, 64, -2.0, 0)
	b.VX, b.VZ = 0.5, 2.5
	b.move(w, false)
	if math.Abs(b.X-0.3125) > 1e-9 || math.Abs(b.Z-0.5) > 1e-9 {
		t.Errorf("the diagonal move left the hull at (%v %v), want (0.3125 0.5): the larger axis first clears the wall's end", b.X, b.Z)
	}
}

// TestABoatSinksInLava: the lift is the water's alone — vanilla's boat
// floats on FluidTags.WATER — so a hull over lava keeps falling to the
// floor the pool holds it over.
// vanilla: AbstractBoat.getStatus's FluidTags.WATER
func TestABoatSinksInLava(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	lava, ok := registry.FindState("minecraft:lava", nil)
	if !ok {
		t.Fatal("no lava state")
	}
	stone := boatStone(t)
	for z := int32(-1); z <= 1; z++ {
		for x := int32(-1); x <= 1; x++ {
			for y := int32(60); y <= 62; y++ {
				w.set(x, y, z, lava)
			}
			w.set(x, 59, z, stone)
		}
	}
	b := NewBoat(1, 0.5, 61.5, 0.5, 0)
	for range 120 {
		b.Step(w, false)
	}
	if b.Y != 60 {
		t.Errorf("the boat floats in lava at y=%v, want it sunk onto the lava's floor at 60", b.Y)
	}
}

// TestAFlowingPoolCarriesABoatAlong: the water's current pushes an empty
// hull the same push every body in a running flow takes, so a hull beside
// a running water drifts where the flow runs.
// vanilla: Entity.updateFluidHeightAndDoFluidPushing
func TestAFlowingPoolCarriesABoatAlong(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	flowing, ok := registry.FindState("minecraft:water", map[string]string{"level": "1"})
	if !ok {
		t.Fatal("no flowing water state")
	}
	// A source with a running east beside it, the running water's east
	// side a dry drop the flow runs to.
	w.set(0, 62, 0, boatWater)
	w.set(1, 62, 0, flowing)
	b := NewBoat(1, 0.5, 62.9, 0.5, 0)
	for range 200 {
		b.Step(w, false)
	}
	if b.X <= 0.7 {
		t.Errorf("the boat still rests at x=%v, want the current having carried it east", b.X)
	}
}

// TestABoatFarBelowTheWorldIsDiscardedLeavingNothing: the below-world
// check that ends the boat marks it out of the world with nothing behind
// it — the discard of vanilla's onBelowWorld, not a break.
func TestABoatFarBelowTheWorldIsDiscardedLeavingNothing(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	b := NewBoat(1, 0.5, -600, 0.5, 0)
	b.Step(w, false)
	if !b.Dead {
		t.Fatal("the boat beneath the world's bottom was not discarded")
	}
	if !b.NoLoot {
		t.Error("the discarded boat leaves a drop behind, want nothing")
	}
}

// TestARiddenBoatKeepsStill: a boat a driver holds is run by the driver's
// client, so the simulation moves it not at all — the tick the server runs
// reads the report, not the physics.
func TestARiddenBoatKeepsStill(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	b := NewBoat(1, 0.5, 64, 0.5, 0)
	for range 10 {
		b.Step(w, true)
	}
	if b.Y != 64 {
		t.Errorf("the ridden boat drifted to y=%v, want the stillness of a drive", b.Y)
	}
}

// TestABoatsBlowsRunOutAndItsDamageWearsOff: the rocking a blow wounds
// runs ten ticks, the damage the blows accumulated wears off by one every
// tick, the direction the rocking runs by turns about with every blow,
// and a survival total that has passed forty breaks the boat.
// vanilla: VehicleEntity.hurtServer, AbstractBoat.tick
func TestABoatsBlowsRunOutAndItsDamageWearsOff(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	b := NewBoat(1, 0.5, 64, 0.5, 0)
	// A blow is read as itself tenfold: a fist's one lays ten of damage,
	// and the rocking runs the ten ticks of it.
	if !b.TakeDamage(1, false) {
		t.Fatal("the first blow did not land")
	}
	if b.HurtTime != BoatHurtTicks {
		t.Errorf("the blow wound %d ticks of rocking, want %d", b.HurtTime, BoatHurtTicks)
	}
	if b.HurtDir != -1 {
		t.Errorf("the fresh blow's rocking runs by %d, want the default 1 turned about", b.HurtDir)
	}
	if b.HurtDamage != 10 {
		t.Errorf("the fist's blow laid %v damage, want its own tenfold 10", b.HurtDamage)
	}
	// The rocking runs out while the damage wears off tick by tick.
	for range BoatHurtTicks {
		b.Step(w, true)
	}
	if b.HurtTime != 0 {
		t.Errorf("the rocking still has %d ticks, want the ten run out", b.HurtTime)
	}
	if b.HurtDamage != 0 {
		t.Errorf("the damage still stands at %v, want the ten ticks having worn it off", b.HurtDamage)
	}
	// The next blow turns the direction about again.
	b.TakeDamage(1, false)
	if b.HurtDir != 1 {
		t.Errorf("the second blow's rocking runs by %d, want the first turned back", b.HurtDir)
	}

	// Blows spaced further than their damage runs up never add to it:
	// fourteen ticks between blows wears every blow's ten clean off.
	for range 20 {
		b.TakeDamage(1, false)
		for range BoatHurtTicks + 4 {
			b.Step(w, true)
		}
	}
	if b.Dead {
		t.Error("twenty blows a second apart broke the boat, want the damage never adding up")
	}
	// Blows a tick apart do add: the fifth carries the total past forty.
	for range 4 {
		b.TakeDamage(1, false)
		b.Step(w, true)
	}
	if b.Dead {
		t.Fatalf("four blows a tick apart broke the boat at %v damage, want it standing under the forty", b.HurtDamage)
	}
	if !b.TakeDamage(1, false) || !b.Dead {
		t.Error("the fifth blow a tick apart did not break the boat, want the total past fourty")
	}
	if b.TakeDamage(1, false) {
		t.Error("a blow on a broken boat landed")
	}
	// A survival break still leaves the drop the boat's break does.
	if b.NoLoot {
		t.Error("a broken boat leaves nothing behind, want the boat it was")
	}
}

// TestACreativeBlowDiscardsABoatWithNothingBehind: a creative instabuild's
// blow takes the boat out of the world at once, no rocking run out and no
// drop.
// vanilla: VehicleEntity.hurtServer's creative discard
func TestACreativeBlowDiscardsABoatWithNothingBehind(t *testing.T) {
	t.Parallel()
	b := NewBoat(1, 0.5, 64, 0.5, 0)
	if !b.TakeDamage(1, true) {
		t.Fatal("the creative blow did not land")
	}
	if !b.Dead || !b.NoLoot {
		t.Error("a creative blow rocked the boat instead of discarding it clean")
	}

	// A sunk hull takes no passenger, its eye under the water.
	pool := newTestWorld()
	for z := int32(0); z <= 0; z++ {
		for x := int32(0); x <= 0; x++ {
			pool.set(x, 0, z, boatWater)
			pool.set(x, 1, z, boatWater)
		}
	}
	sunk := NewBoat(1, 0.5, 0.5, 0.5, 0)
	sunk.updateFluids(pool, false)
	if !sunk.EyeInWater() {
		t.Error("a hull whole under a block of water reads dry-eyed")
	}
	if sunk.CanTakePassenger(0, 2) {
		t.Error("a sunk hull takes a passenger")
	}
}

// TestABlastBetweenTheDeckAndTheDefaultEyePushesTheBoatUp: a hull's eye sits
// at the top of the hull, the eyeHeight the boat entity types set, so a blast
// just below the deck's top but above the dimensions' default 0.85-of-the-
// hull eye pushes the boat up. Read off the default the vertical part would
// come out down.
// vanilla: Entity.getEyePosition, EntityTypes BOAT eyeHeight
func TestABlastBetweenTheDeckAndTheDefaultEyePushesTheBoatUp(t *testing.T) {
	t.Parallel()
	b := NewBoat(1, 0.5, 64, 0.5, 0)
	ex, ey, ez := b.EyePosition()
	if math.Abs(ey-(64+BoatHeight)) > 1e-9 {
		t.Errorf("the hull's eye stands at y=%v, want the top of the hull %v", ey, 64+BoatHeight)
	}
	// The blast halfway between the top eye and the default one: below the
	// top, above the default.
	blastY := 64 + (BoatHeight+BoatHeight*0.85)/2
	dist := ey - blastY
	_, ky, _ := rules.ExplosionKnockback(ex, ey, ez, b.X, blastY, b.Z, math.Abs(dist), 1, 5)
	if ky <= 0 {
		t.Errorf("the blast above the default eye pushed the hull by %v down, want it up", ky)
	}
}

// TestASunkenBoatEjectsItsRidersAtSixtyTicks: a ridden hull held wholly under
// water unseats its riders on the sixtieth tick, and takes no boarders after.
// vanilla: AbstractBoat.tick's outOfControlTicks
func TestASunkenBoatEjectsItsRidersAtSixtyTicks(t *testing.T) {
	t.Parallel()
	pool := newTestWorld()
	pool.set(0, 0, 0, boatWater)
	pool.set(0, 1, 0, boatWater)
	pool.set(0, 2, 0, boatWater)
	b := NewBoat(1, 0.5, 0.5, 0.5, 0)
	for i := 1; i < boatOutOfControlTicks; i++ {
		b.Step(pool, true)
		if b.EjectPassengers {
			t.Fatalf("riders ejected on tick %d, want the sixtieth", i)
		}
	}
	b.Step(pool, true)
	if !b.EjectPassengers {
		t.Fatal("riders still aboard after sixty ticks under water")
	}
	if b.CanTakePassenger(0, 2) {
		t.Error("a boat sunk for sixty ticks takes a passenger")
	}
	b.EjectPassengers = false
	b.Step(newTestWorld(), true)
	if b.EjectPassengers || b.outOfControl != 0 {
		t.Error("the count survived the hull coming up")
	}
}

// vanilla: AbstractBoat.getGroundFriction
func TestBoatGroundFrictionRequiresShapeContact(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, block string
		y           float64
		contact     bool
	}{
		{"above stone", "minecraft:stone", 64.01, false},
		{"above slab", "minecraft:stone_slab", 64, false},
		{"on slab", "minecraft:stone_slab", 63.5, true},
		{"on fence", "minecraft:oak_fence", 64.5, true},
		{"above fence", "minecraft:oak_fence", 64.51, false},
		{"lily pad", "minecraft:lily_pad", 63.09375, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			w.set(0, 63, 0, defaultState(t, tc.block))
			b := NewBoat(1, 0.5, tc.y, 0.5, 0)
			friction, contact := b.groundFriction(w)
			if contact != tc.contact || contact && friction != float64(float32(0.6)) {
				t.Fatalf("ground friction = %v, %v; want contact=%v", friction, contact, tc.contact)
			}
		})
	}
}
