package entity

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

// vanilla: ThrowableProjectile.applyInertia, AbstractHurtingProjectile.applyInertia
func TestThrownSpawnedInWaterUsesPreviousTickDrag(t *testing.T) {
	for _, kind := range []ThrownKind{ThrownSnowball, ThrownEgg, ThrownEnderPearl, ThrownSplashPotion, ThrownLingeringPotion, ThrownExperienceBottle, ThrownSmallFireball, ThrownLargeFireball, ThrownWitherSkull, ThrownDragonFireball} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			w := newTestWorld()
			water := defaultState(t, "minecraft:water")
			for x := int32(-1); x <= 1; x++ {
				for y := int32(64); y <= 67; y++ {
					for z := int32(-1); z <= 1; z++ {
						w.set(x, y, z, water)
					}
				}
			}
			p := NewThrown(1, kind, 0, 65, 0, .25, .3, .15, 0, 1)
			p.AccelerationPower = 0
			air := .9900000095367432
			if kind.IsFireball() {
				air = float64(float32(.95))
			}
			for tick := 1; tick <= 3; tick++ {
				p.Step(w, nil, nil)
				// The snowball's vanilla three-tick X motion is 0.1584;
				// applying water drag on its first tick gives 0.128.
				want := .25 * air * math.Pow(.8, float64(tick-1))
				if p.Dead || math.Abs(p.VX-want) > 1e-6 {
					t.Fatalf("tick %d: dead=%v, VX=%g, want %g", tick, p.Dead, p.VX, want)
				}
			}
		})
	}
}

// vanilla: Entity.updateFluidInteraction, ThrowableProjectile.applyInertia
func TestThrownWaterDragUsesBoxAndCachedState(t *testing.T) {
	w := newTestWorld()
	w.set(1, 64, 0, defaultState(t, "minecraft:water"))
	p := NewThrown(1, ThrownSnowball, .95, 64.25, .5, .01, 0, 0, 0, 1)
	p.NoGravity = true
	p.Step(w, nil, nil)
	// Only the side of the box touches water; the feet stay in air.
	w.set(1, 64, 0, 0)
	p.Step(w, nil, nil)
	want := .01 * .9900000095367432 * .8
	if math.Abs(p.VX-want) > 1e-9 {
		t.Fatalf("VX=%g, want previous tick's box water drag %g", p.VX, want)
	}
	p.Step(w, nil, nil)
	want *= .9900000095367432
	if math.Abs(p.VX-want) > 1e-9 {
		t.Fatalf("VX=%g, want air drag after leaving water %g", p.VX, want)
	}
}

// The thrown projectile simulation as ordinary unit tests, driving the step
// over a world that is nothing but the blocks each case sets, the way the
// arrow's own are. No server is involved; the black-box tests in
// internal/server/server_thrown_test.go remain the proof of the whole life of a
// thrown egg or snowball — the hand that throws it, the flight it makes,
// the chicks an egg hatches — and these are the cheapest possible checks of
// the flight itself.

// TestAThrownEggFliesWithGravityAndDrag throws an egg flat along the z axis
// and checks the flight against the arithmetic vanilla's tick runs, which
// is the arrow's read in the thrown order: the gravity taken off the
// velocity before the drag shrinks the whole of it, and the position
// carried by the velocity the tick moved with.
func TestAThrownEggFliesWithGravityAndDrag(t *testing.T) {
	w := newTestWorld()
	e := NewThrown(1, ThrownEgg, 0, 64, 0, 0, 0, 1.5, 0, 3)

	e.Step(w, nil, nil)
	// The position: the velocity the tick moved with, which is the
	// dragged one — the gravity and the drag of the tick run ahead of the
	// move — one and a half of a block's flight shrunk to its share.
	if !closeEnough(e.Z, 1.5*0.9900000095367432) {
		t.Errorf("after one tick the egg stands at z=%v, want the dragged velocity's share", e.Z)
	}
	// The velocity the next tick moves with: the fall's 0.03 taken off
	// the y first, then the drag's share of the whole — the (double)0.99f
	// the widened float is, not the 0.99 a plain literal reads as.
	if !closeEnough(e.VY, -0.03*0.9900000095367432) {
		t.Errorf("the fall after one tick = %v, want the gravity's own -0.03 under the drag", e.VY)
	}
	if !closeEnough(e.VZ, 1.5*0.9900000095367432) {
		t.Errorf("the flight after one tick = %v, want the drag's share of one and a half", e.VZ)
	}
	if e.Dead {
		t.Error("an egg thrown into open air broke")
	}
}

// TestANoGravityThrownEggKeepsItsLine throws an egg flat with the flag: the
// tick takes no gravity off the vertical, so the flight keeps its line
// where a plain throw would begin to drop.
func TestANoGravityThrownEggKeepsItsLine(t *testing.T) {
	w := newTestWorld()
	e := NewThrown(1, ThrownEgg, 0, 64, 0, 0, 0, 1.5, 0, 3)
	e.NoGravity = true
	e.Step(w, nil, nil)
	if e.VY != 0 {
		t.Errorf("the no-gravity egg's vy after one tick = %v, want nought, the gravity term dropped", e.VY)
	}
}

// TestAThrownEggBreaksOnTheBlockFaceItMeets throws an egg at a wall of
// stone two blocks off and checks the break: the flight stopped at the
// wall's face, the body dead, and the break event carrying the place the
// flight ended, which is where an egg's chicks hatch.
func TestAThrownEggBreaksOnTheBlockFaceItMeets(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	w.set(0, 63, 2, stone)
	w.set(0, 64, 2, stone)

	// Seed one's first roll of eight passes, so this egg's break hatches
	// nothing and the test's want below reads against it.
	e := NewThrown(1, ThrownEgg, 0, 64, 0, 0, 0, 1.5, 0, 1)
	// The first tick's flight ends short of the wall two blocks off; the
	// second's runs into its face.
	var events []ThrownEvent
	e.Step(w, nil, nil)
	events = e.Step(w, nil, nil)

	if !e.Dead {
		t.Fatal("an egg thrown into a wall did not break")
	}
	if len(events) != 1 || events[0].Kind != ThrownBreak {
		t.Fatalf("the wall gave %#v, want one break event", events)
	}
	if !closeEnough(events[0].Z, 2) {
		t.Errorf("the egg broke at z=%v, want the wall's face", events[0].Z)
	}
	if !closeEnough(e.Z, 2) {
		t.Errorf("the broken egg stands at z=%v, want the face", e.Z)
	}
	// The roll: one chance in eight of chicks, and the rolls a test
	// replays come back the same. This seed's first roll of eight is not
	// the nought, so the egg left no chicks.
	if events[0].Chicks != 0 {
		t.Errorf("the egg hatched %d chicks off a roll that passed nothing", events[0].Chicks)
	}
}

// TestAThrownEggGrantsItsHitBoxesTheAgeMargin throws an egg past a body
// whose edge stands a twentieth of a block clear of the flight line, the
// share of margin a body three ticks old has earned: the first two ticks
// of flight grant none and pass it by, the third's own margin reaches it
// and the hit lands.
func TestAThrownEggGrantsItsHitBoxesTheAgeMargin(t *testing.T) {
	w := newTestWorld()
	// The body: standing so its box's near face is a hundredth past the
	// line the egg flies along, the box starting ahead of the egg so the line
	// enters it — a gap the third tick's margin of a
	// twentieth reaches and the first two ticks' none does.
	target := ArrowTarget{EntityID: 9, MinX: 0.01, MinY: 63, MinZ: 3.5, MaxX: 0.5, MaxY: 65, MaxZ: 10}
	e := NewThrown(1, ThrownEgg, 0, 64, 0, 0, 0, 1.5, 0, 3)

	for tick := 1; tick <= 2; tick++ {
		for _, ev := range e.Step(w, nil, []ArrowTarget{target}) {
			if ev.Kind == ThrownHitTarget {
				t.Fatalf("the egg hit the body on tick %d, whose margin grants none", tick)
			}
		}
		if e.Dead {
			t.Fatalf("the egg broke on tick %d", tick)
		}
	}
	var hit bool
	for _, ev := range e.Step(w, nil, []ArrowTarget{target}) {
		if ev.Kind == ThrownHitTarget && ev.Target.EntityID == 9 {
			hit = true
		}
	}
	if !hit {
		t.Error("the egg flew past a body its third tick's margin reaches without hitting it")
	}
}

// TestAThrownEggCannotHitItsThrowerAtFirst throws an egg out of the body of
// its own thrower — every throw's birth, the spawn sitting inside their box
// — and checks the exclusion: the flight's first ticks cannot touch them,
// and a body flown clear and turned about may land on them.
func TestAThrownEggCannotHitItsThrowerAtFirst(t *testing.T) {
	w := newTestWorld()
	thrower := PlayerTarget(7, 0, 63, 0, false)
	e := NewThrown(1, ThrownEgg, 0, 64, 0, 0, 0, 1.5, 7, 3)

	for range 3 {
		for _, ev := range e.Step(w, &thrower, []ArrowTarget{thrower}) {
			if ev.Kind == ThrownHitTarget && ev.Target.EntityID == 7 {
				t.Error("the egg hit its own thrower on the flight out of them")
			}
		}
	}
	if !e.leftOwner {
		t.Fatal("an egg three blocks clear of its thrower is still held unable to hit them")
	}

	// The flight turned about, the way a falling one's is, and the way
	// back crosses the thrower's body: with the mark set, the hit lands.
	e.VZ = -1.5
	hit := false
	for range 5 {
		for _, ev := range e.Step(w, &thrower, []ArrowTarget{thrower}) {
			if ev.Kind == ThrownHitTarget && ev.Target.EntityID == 7 {
				hit = true
			}
		}
	}
	if !hit {
		t.Error("an egg flown clear of its thrower and turned back never hit them")
	}
}

// TestAThrownEggsAnglesEaseTowardItsFlight throws an egg with a rise to it
// and checks the angles: the spawn's own are the velocity's, and each tick
// eases them a fifth of the way toward the angle the velocity has shrunk
// to — the pitch falling as the fall's pull flattens the flight.
func TestAThrownEggsAnglesEaseTowardItsFlight(t *testing.T) {
	w := newTestWorld()
	e := NewThrown(1, ThrownEgg, 0, 64, 0, 0, 0.75, 1.5, 0, 3)
	// The spawn's angles are the velocity's own: a rise over a run, whose
	// pitch is the arc tangent of the one over the other.
	wantPitch := float32(math.Atan2(0.75, 1.5) * 180 / math.Pi)
	if !closeEnough(float64(e.Pitch), float64(wantPitch)) {
		t.Fatalf("the egg was born pointing %v up, want the velocity's %v", e.Pitch, wantPitch)
	}

	e.Step(w, nil, nil)
	// After one tick of flight the velocity has fallen and been dragged;
	// the pitch eased a fifth of the way toward its angle.
	vx, vy, vz := 0.0, e.VY, e.VZ
	target := float32(math.Atan2(vy, math.Hypot(vx, vz)) * 180 / math.Pi)
	want := wantPitch + (target-wantPitch)*0.2
	if !closeEnough(float64(e.Pitch), float64(want)) {
		t.Errorf("the pitch after one tick = %v, want the fifth of the way to %v", e.Pitch, want)
	}
}

// TestAnEggHatchesItsChicksWhereTheRollsGrantThem drives the egg's own
// rolls directly: a source whose first roll of eight comes up nought
// hatches one chick, and where its next roll of thirty-two comes up nought
// too the hatch is four, and a snowball's break rolls nothing whatever its
// source reads.
func TestAnEggHatchesItsChicksWhereTheRollsGrantThem(t *testing.T) {
	// A seed whose first two draws of a twenty-bit spread of rolls land
	// the egg's first roll of eight on the nought and its next roll of
	// thirty-two on it too: the four-chick hatch.
	var fourSeed int64 = -1
	for seed := int64(1); seed < 100000; seed++ {
		rng := rand.New(rand.NewSource(seed))
		if rng.Intn(8) == 0 && rng.Intn(32) == 0 {
			fourSeed = seed
			break
		}
	}
	if fourSeed < 0 {
		t.Fatal("no seed under a hundred thousand hatched four chicks")
	}
	e := NewThrown(1, ThrownEgg, 0, 64, 0, 0, 0, 1.5, 0, fourSeed)
	if n := e.chickRoll(); n != 4 {
		t.Errorf("the four-chick roll gave %d", n)
	}

	// A seed whose first roll of eight passes: no chicks, and the roll of
	// thirty-two never asked.
	var noneSeed int64 = -1
	for seed := int64(1); seed < 100000; seed++ {
		rng := rand.New(rand.NewSource(seed))
		if rng.Intn(8) != 0 {
			noneSeed = seed
			break
		}
	}
	if noneSeed < 0 {
		t.Fatal("no seed under a hundred thousand passed the roll of eight")
	}
	e = NewThrown(1, ThrownEgg, 0, 64, 0, 0, 0, 1.5, 0, noneSeed)
	if n := e.chickRoll(); n != 0 {
		t.Errorf("the no-chick roll gave %d", n)
	}

	// A seed whose first roll of eight lands and whose next roll of
	// thirty-two passes: one chick, the second roll spent.
	var oneSeed int64 = -1
	for seed := int64(1); seed < 100000; seed++ {
		rng := rand.New(rand.NewSource(seed))
		if rng.Intn(8) == 0 && rng.Intn(32) != 0 {
			oneSeed = seed
			break
		}
	}
	if oneSeed < 0 {
		t.Fatal("no seed under a hundred thousand hatched one chick")
	}
	e = NewThrown(1, ThrownEgg, 0, 64, 0, 0, 0, 1.5, 0, oneSeed)
	if n := e.chickRoll(); n != 1 {
		t.Errorf("the one-chick roll gave %d", n)
	}

	// The snowball's break: whatever the source reads, nothing hatches.
	s := NewThrown(1, ThrownSnowball, 0, 64, 0, 0, 0, 1.5, 0, fourSeed)
	if n := s.chickRoll(); n != 0 {
		t.Errorf("a snowball's break hatched %d chicks", n)
	}
}

// TestAThrownEggPassesThroughABodyItStartsInside flies an egg out of the
// middle of a body's box: with no entry crossing it hits nothing, while the
// same body standing ahead of the flight is hit.
func TestAThrownEggPassesThroughABodyItStartsInside(t *testing.T) {
	w := newTestWorld()
	around := ArrowTarget{EntityID: 9, MinX: -1, MinY: 63, MinZ: -1, MaxX: 1, MaxY: 65, MaxZ: 1}
	e := NewThrown(1, ThrownEgg, 0, 64, 0, 0, 0, 0.1, 0, 3)
	for _, ev := range e.Step(w, nil, []ArrowTarget{around}) {
		if ev.Kind == ThrownHitTarget {
			t.Fatal("the egg hit a body whose box held its start")
		}
	}
	if e.Dead {
		t.Fatal("the egg broke inside the body")
	}

	ahead := ArrowTarget{EntityID: 9, MinX: -1, MinY: 63, MinZ: 1, MaxX: 1, MaxY: 65, MaxZ: 3}
	e = NewThrown(1, ThrownEgg, 0, 64, 0, 0, 0, 1.5, 0, 3)
	var hit bool
	for _, ev := range e.Step(w, nil, []ArrowTarget{ahead}) {
		hit = hit || ev.Kind == ThrownHitTarget
	}
	if !hit {
		t.Fatal("the egg flew past a body ahead of it")
	}
}
