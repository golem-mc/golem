package entity

import (
	"testing"
)

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
