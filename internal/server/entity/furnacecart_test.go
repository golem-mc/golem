package entity

import (
	"math"
	"testing"
)

func TestFurnaceFuelTakesEightItemsThenRefuses(t *testing.T) {
	var f FurnaceCart
	for i := range 8 {
		if !f.AddFuel(5, 5, 3, 5) {
			t.Fatalf("item %d refused at fuel %d", i+1, f.Fuel)
		}
	}
	if f.Fuel != 28800 {
		t.Fatalf("fuel = %d, want 28800", f.Fuel)
	}
	if f.AddFuel(5, 5, 3, 5) {
		t.Errorf("a ninth item was taken: 28800 + 3600 passes the 32000 cap")
	}
	if f.Fuel != 28800 {
		t.Errorf("refused item changed fuel to %d", f.Fuel)
	}
}

func TestFurnaceFuelPushesAwayFromThePlayer(t *testing.T) {
	var f FurnaceCart
	f.AddFuel(5, 5, 3, 6)
	if f.PushX != 2 || f.PushZ != -1 || f.PushY != 0 {
		t.Errorf("push = (%v, %v, %v), want (2, 0, -1)", f.PushX, f.PushY, f.PushZ)
	}
}

func TestFurnaceTickBurnsAndClearsPushWhenOut(t *testing.T) {
	f := FurnaceCart{Fuel: 2, PushX: 1}
	f.Tick()
	if f.Fuel != 1 || !f.Lit || f.PushX != 1 {
		t.Fatalf("after one tick: fuel %d lit %v push %v", f.Fuel, f.Lit, f.PushX)
	}
	f.Tick()
	if f.Fuel != 0 || f.Lit || f.PushX != 0 {
		t.Fatalf("after two ticks: fuel %d lit %v push %v, want empty, unlit, no push", f.Fuel, f.Lit, f.PushX)
	}
	f.Tick()
	if f.Fuel != 0 {
		t.Errorf("fuel went to %d below empty", f.Fuel)
	}
}

func TestFurnaceSlowdown(t *testing.T) {
	idle := FurnaceCart{}
	if vx, vz := idle.Slowdown(0.1, -0.04, 0.2, false); math.Abs(vx-0.098) > 1e-12 || math.Abs(vz-0.196) > 1e-12 {
		t.Errorf("idle = (%v, %v), want 0.98 of the speed", vx, vz)
	}
	// Moving along the push: 0.8 of the speed plus the push.
	pushed := FurnaceCart{PushX: 0.5}
	if vx, vz := pushed.Slowdown(0.1, 0, 0, false); math.Abs(vx-(0.08+0.5)) > 1e-12 || vz != 0 {
		t.Errorf("pushed = (%v, %v), want (0.58, 0)", vx, vz)
	}
	// Below the 0.001 motion epsilon the push is not turned.
	still := FurnaceCart{PushZ: 0.5}
	still.Slowdown(0.01, 0, 0, false)
	if still.PushZ != 0.5 {
		t.Errorf("a near-still cart turned the push to z=%v", still.PushZ)
	}
	// A push square to the motion projects to nothing and is dropped.
	square := FurnaceCart{PushZ: 0.5}
	square.Slowdown(0.2, 0, 0, false)
	if square.PushX != 0 || square.PushZ != 0 {
		t.Errorf("push = (%v, %v), want zero for a push square to the motion", square.PushX, square.PushZ)
	}
	along := FurnaceCart{PushX: 0.3, PushZ: 0.4}
	along.Slowdown(0.2, 0, 0, false)
	if math.Abs(along.PushX-0.5) > 1e-12 || math.Abs(along.PushZ) > 1e-12 {
		t.Errorf("push = (%v, %v), want (0.5, 0) turned along the motion", along.PushX, along.PushZ)
	}
	water := FurnaceCart{PushX: 0.5}
	if vx, _ := water.Slowdown(0.1, 0, 0, true); math.Abs(vx-0.058) > 1e-12 {
		t.Errorf("in water vx = %v, want a tenth of 0.58", vx)
	}
}

func TestFurnaceCartTopSpeedIsHalved(t *testing.T) {
	plain, furnace := NewMinecart(1, 0, 0, 0), NewMinecart(2, 0, 0, 0)
	furnace.Furnace = &FurnaceCart{}
	for _, c := range []struct {
		water       bool
		plain, furn float64
	}{{false, 0.4, 0.2}, {true, 0.2, 0.15}} {
		if got := plain.maxSpeed(c.water); math.Abs(got-c.plain) > 1e-12 {
			t.Errorf("plain cart (water %v) = %v, want %v", c.water, got, c.plain)
		}
		if got := furnace.maxSpeed(c.water); math.Abs(got-c.furn) > 1e-12 {
			t.Errorf("furnace cart (water %v) = %v, want %v", c.water, got, c.furn)
		}
	}
}
