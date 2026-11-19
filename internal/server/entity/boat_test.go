package entity

import (
	"math"
	"testing"

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
