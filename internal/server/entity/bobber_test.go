package entity

import (
	"math"
	"testing"
)

// The bobber simulation as ordinary unit tests, driving the step over a
// world that is nothing but the blocks each case sets, the way the thrown
// projectiles' own are. The black-box tests in internal/server/server_fishing_test.go
// remain the proof of the whole life of a fishing rod — the cast, the
// bite's payout, the pull — and these are the cheapest possible checks of
// the hook itself.

// TestACastLeavesOnTheLookLine checks the constructor's own arithmetic: a
// bobber cast level and south is born at the eye a third of a block along
// the look, leaves on the look's line alone — the ray's nought east share
// a nought stays, whatever its factor scales — and moves at the line's
// own share of the six tenths and the half, the triangle's swing either
// way held to its deviation.
func TestACastLeavesOnTheLookLine(t *testing.T) {
	b := NewBobber(1, 10, 64+1.62, 20, 0, 0, 7, 3, 0, 0)
	if !closeEnough(b.X, 10) || !closeEnough(b.Z, 20+0.3) {
		t.Errorf("the bobber was born at (%v %v), want the eye's own x, a third south", b.X, b.Z)
	}
	if b.VX != 0 {
		t.Errorf("the level cast's vx = %v, want the look line's own zero", b.VX)
	}
	if b.VY != 0 {
		t.Errorf("the level cast's vy = %v, want the look line's own zero", b.VY)
	}
	if b.VZ < 1.1-0.0103365 || b.VZ > 1.1+0.0103365 {
		t.Errorf("the level cast's vz = %v, want the 1.1 the line's share gives", b.VZ)
	}
	if b.Yaw != 0 || b.Pitch != 0 {
		t.Errorf("the level cast points at yaw %v pitch %v, want the level south of its line", b.Yaw, b.Pitch)
	}
}

// TestAFlightIntoWaterTurnsToBobbing casts a bobber at a pool a couple of
// blocks below and checks the water's take of it: the flight counts the
// gravity and the drag until the tick that stands it in the water, whose
// state is the bobbing the cast's whole water life runs in, the entry
// damped the water's own shares leave.
func TestAFlightIntoWaterTurnsToBobbing(t *testing.T) {
	w := newTestWorld()
	water := defaultState(t, "minecraft:water")
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			w.set(x, 63, z, water)
		}
	}
	b := NewBobber(1, 0, 66, 0, 0, 50, 7, 3, 0, 0)
	// Two ticks of flight stand the hook in the pool's cell; the tick
	// after them takes the state.
	b.Step(w, &BobberOwner{Fishing: true}, nil, nil)
	b.Step(w, &BobberOwner{Fishing: true}, nil, nil)
	if b.State != BobberFlying {
		t.Fatalf("after two ticks of flight the hook is %v, want the still-flying", b.State)
	}
	vx0, vy0, vz0 := b.VX, b.VY, b.VZ
	b.Step(w, &BobberOwner{Fishing: true}, nil, nil)
	if b.State != BobberBobbing {
		t.Fatalf("the hook that entered the water is %v, want the bobbing", b.State)
	}
	// The entry damp: the flight's whole velocity under the water's
	// own shares.
	if !closeEnough(b.VX, vx0*0.3) || !closeEnough(b.VY, vy0*0.2) || !closeEnough(b.VZ, vz0*0.3) {
		t.Errorf("the entry left the hook at (%v %v %v), want the water's shares of (%v %v %v)",
			b.VX, b.VY, b.VZ, vx0, vy0, vz0)
	}
}

// TestABittenHookOpensTheNibbleWindow winds a bobbing hook to the bite's
// door and checks the bite: one event, the window of twenty to forty
// ticks opened, the flag the wire pops on set, and the window's end
// clearing it again.
func TestABittenHookOpensTheNibbleWindow(t *testing.T) {
	w := newTestWorld()
	water := defaultState(t, "minecraft:water")
	// Three cells deep, so the bite's sink does not drop the hook out of
	// the water and stop the countdown.
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			w.set(x, 61, z, water)
			w.set(x, 62, z, water)
			w.set(x, 63, z, water)
		}
	}
	b := NewBobber(1, 0, 66, 0, 0, 50, 7, 3, 0, 0)
	for range 4 {
		b.Step(w, &BobberOwner{Fishing: true}, nil, nil)
	}
	if b.State != BobberBobbing {
		t.Fatalf("the hook never reached its water, state %v", b.State)
	}
	// A hook at rest in the middle of the pool, its drift stilled: the
	// bite and its window are the water's own, not the cast's leftovers.
	b.X, b.Z, b.VX, b.VY, b.VZ = 0, 0, 0, 0, 0
	// The bite's door: the approach spent, one tick of it left. The seed
	// is fixed, so the window's roll is the same want every run.
	b.TimeUntilLured = 0
	b.TimeUntilHooked = 1
	events := b.Step(w, &BobberOwner{Fishing: true}, nil, nil)
	var bite *BobberEvent
	for i := range events {
		if events[i].Kind == BobberBite {
			bite = &events[i]
		}
	}
	if bite == nil {
		t.Fatalf("the bite gave %#v, want a bite event among them", events)
	}
	if math.Abs(bite.X-b.X) > 0.5 || math.Abs(bite.Y-b.Y) > 0.5 || math.Abs(bite.Z-b.Z) > 0.5 {
		t.Errorf("the bite event stands at (%v %v %v), far from the hook at (%v %v %v)",
			bite.X, bite.Y, bite.Z, b.X, b.Y, b.Z)
	}
	if b.Nibble < 20 || b.Nibble > 40 {
		t.Errorf("the bite opened a window of %d ticks, want the twenty to forty", b.Nibble)
	}
	if !b.Biting {
		t.Error("the bite left the wire's catchable flag unset")
	}
	// The window spends itself in the water and clears the flag at its
	// end; the next water's countdowns begin over, the lured wait rolled
	// fresh for the fish that comes after.
	for range 100 {
		b.Step(w, &BobberOwner{Fishing: true}, nil, nil)
		if b.Nibble == 0 {
			break
		}
	}
	if b.Biting {
		t.Error("the flag stayed past the window's end")
	}
	b.Step(w, &BobberOwner{Fishing: true}, nil, nil)
	if b.TimeUntilLured <= 0 {
		t.Error("the water rolled no fresh lured wait after the bite")
	}
}

// TestABiteDipsTheBobber winds a hook to the bite and checks the sink the
// wire's biting flag carries: vanilla sets the vertical velocity to a
// 0.6-to-1 dip of negative four tenths, which the tick's drag then scales.
func TestABiteDipsTheBobber(t *testing.T) {
	w := newTestWorld()
	water := defaultState(t, "minecraft:water")
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			w.set(x, 63, z, water)
		}
	}
	b := NewBobber(1, 0, 66, 0, 0, 50, 7, 3, 0, 0)
	for range 4 {
		b.Step(w, &BobberOwner{Fishing: true}, nil, nil)
	}
	if b.State != BobberBobbing {
		t.Fatalf("the hook never reached its water, state %v", b.State)
	}
	b.X, b.Z, b.VX, b.VY, b.VZ = 0, 0, 0, 0, 0
	b.TimeUntilLured = 0
	b.TimeUntilHooked = 1
	b.Step(w, &BobberOwner{Fishing: true}, nil, nil)
	if !b.Biting {
		t.Fatal("the wound hook never bit")
	}
	// The dip lands in [-0.4, -0.24], scaled by the tick's 0.92 drag.
	if b.VY > -0.24*0.92 || b.VY < -0.4*0.92 {
		t.Errorf("the bite left the hook at vy=%v, want the dip's four tenths scaled by the drag", b.VY)
	}
}

// TestAFlightHooksTheBodyItMeets runs a flying hook across a body and
// checks the take: the flight names the body without stopping, the next
// tick zeroes the flight and rides the body at its own 0.8 height, and a
// body gone from the world puts the hook back to flying.
func TestAFlightHooksTheBodyItMeets(t *testing.T) {
	w := newTestWorld()
	b := NewBobber(1, 0, 64, 0, 0, 0, 7, 3, 0, 0)
	target := ArrowTarget{EntityID: 9, MinX: -0.5, MinY: 63, MinZ: 0.5, MaxX: 0.5, MaxY: 64.9, MaxZ: 1.5}
	owner := &BobberOwner{Fishing: true}
	vz1 := b.VZ
	events := b.Step(w, owner, nil, []ArrowTarget{target})
	if len(events) != 1 || events[0].Kind != BobberHitTarget || events[0].Z != target.MinZ {
		t.Fatalf("hook events = %+v, want a body hit at z=%v", events, target.MinZ)
	}
	if b.HookedID != 9 {
		t.Fatalf("the flight across the body hooked %d, want the body's nine", b.HookedID)
	}
	// The strikes' own tick still moves the hook by its flight.
	if !closeEnough(b.Z, 0.3+vz1) {
		t.Errorf("the striking tick left the hook at z=%v, want the flight's own %v", b.Z, 0.3+vz1)
	}
	b.Step(w, owner, &target, nil)
	if b.State != BobberHookedInEntity {
		t.Fatalf("the hook that took a body is %v, want the riding state", b.State)
	}
	if b.VX != 0 || b.VY != 0 || b.VZ != 0 {
		t.Errorf("the riding hook keeps a flight of (%v %v %v), want the zeroed", b.VX, b.VY, b.VZ)
	}
	// The next tick rides the body: its own place, at the height's 0.8.
	b.Step(w, owner, &target, nil)
	if !closeEnough(b.X, 0) || !closeEnough(b.Y, 63+0.8*(64.9-63)) || !closeEnough(b.Z, 1) {
		t.Errorf("the riding hook stands at (%v %v %v), want the body's own place at its height's 0.8", b.X, b.Y, b.Z)
	}
	// The body gone: unhooked, flying again.
	b.Step(w, owner, nil, nil)
	if b.State != BobberFlying || b.HookedID != 0 {
		t.Fatalf("the hook whose body left is state %v hooking %d, want the flying free", b.State, b.HookedID)
	}
}

// TestTheHookLeavesWithItsAngler checks the hold: a hook whose angler
// gives nothing to hold — gone, or fishing no more — is out of the world
// with no event, and one whose angler holds stays for the water.
func TestTheHookLeavesWithItsAngler(t *testing.T) {
	w := newTestWorld()
	b := NewBobber(1, 0, 64, 0, 0, 0, 7, 3, 0, 0)
	if events := b.Step(w, nil, nil, nil); !b.Dead || events != nil {
		t.Fatalf("an anglerless hook stepped to dead=%v with %#v, want the discard untold", b.Dead, events)
	}

	b = NewBobber(1, 0, 64, 0, 0, 0, 7, 3, 0, 0)
	if events := b.Step(w, &BobberOwner{Fishing: false}, nil, nil); !b.Dead || events != nil {
		t.Fatalf("an unheld hook stepped to dead=%v with %#v, want the discard untold", b.Dead, events)
	}

	b = NewBobber(1, 0, 64, 0, 0, 0, 7, 3, 0, 0)
	if events := b.Step(w, &BobberOwner{Fishing: true}, nil, nil); b.Dead || events != nil {
		t.Fatalf("a held hook stepped to dead=%v with %#v, want the flight carried on", b.Dead, events)
	}
}
