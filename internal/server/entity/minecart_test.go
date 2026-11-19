package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The cart simulation as ordinary unit tests, driving the step over a
// world that is nothing but the blocks each case sets. The black-box
// tests in internal/server/minecart_test.go remain the proof of the whole life of
// a cart — the laying, the boarding, the breaking — and these are the
// cheapest possible checks of the rail ride itself.

// cartState finds one block state by its name and properties, the state
// the test's rails are laid as.
func cartState(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("no %s state", name)
	}
	return state
}

// cartGround lays the stone the test's line stands on and the rails over
// it, east-west along z=0, from west to east inclusive.
func cartGround(t *testing.T, w testWorld, west, east, y int32) {
	t.Helper()
	for x := west; x <= east; x++ {
		w.set(x, y-1, 0, cartState(t, "minecraft:stone", nil))
		w.set(x, y, 0, cartState(t, "minecraft:rail",
			map[string]string{"shape": "east_west"}))
	}
}

// trackOf runs a cart n ticks and returns its x, y, z.
func trackOf(m *Minecart, w World, n int) (x, y, z float64) {
	for range n {
		m.Step(w, false)
	}
	return m.X, m.Y, m.Z
}

// TestALitPoweredRailShovesARestingCartAlongTheLine: a cart standing at
// rest on a lit powered rail with stone holding its west end is shoved
// off eastward a tick's start, the boost carries it up the line, and the
// siding's own friction brings it to a stand somewhere along the rails.
// vanilla: OldMinecartBehavior.moveAlongTrack's power half
func TestALitPoweredRailShovesARestingCartAlongTheLine(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	cartGround(t, w, 1, 20, 64)
	// The stone that ends the line behind the lit rail, the conductor the
	// rest-launch shoves the cart away from.
	w.set(0, 63, 0, cartState(t, "minecraft:stone", nil))
	w.set(0, 64, 0, cartState(t, "minecraft:stone", nil))
	w.set(1, 64, 0, cartState(t, "minecraft:powered_rail",
		map[string]string{"shape": "east_west", "powered": "true"}))

	m := NewMinecart(1, 1.5, 64.0625, 0.5)
	x, y, z := trackOf(m, w, 2)
	if x <= 1.5 {
		t.Fatalf("after two ticks the cart stands at x=%v, want shoved east of where it was", x)
	}
	if snapped := 64 + 0.0625; math.Abs(y-snapped) > 1e-9 {
		t.Errorf("the cart rides at y=%v, want the rail's lane %v", y, snapped)
	}
	if math.Abs(z-0.5) > 1e-9 {
		t.Errorf("the cart rides at z=%v, want pinned to the line's z=0.5", z)
	}

	// Two hundred ticks: eastward along the whole run, and stopped
	// somewhere on it by the empty cart's friction.
	x2, _, _ := trackOf(m, w, 200)
	if x2 <= x {
		t.Errorf("the cart ran to x=%v, want further east than the first step's %v", x2, x)
	}
	if vx := math.Abs(m.VX); vx > 0.001 {
		t.Errorf("the cart still runs at %v a tick after two hundred, want a stand", vx)
	}
	if x2 > 21.5 {
		t.Errorf("the cart reached x=%v, want the line's end held it", x2)
	}
}

// TestACartClimbsAnAscendingRailToTheRaisedLine: a cart with its head of
// steam reaches a slope and rides it up, its height rising the whole cell
// to the raised line's own plane, where it carries on east at the height
// the high rails keep.
// vanilla: OldMinecartBehavior.moveAlongTrack's ascending half
func TestACartClimbsAnAscendingRailToTheRaisedLine(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	cartGround(t, w, 0, 4, 64)
	// The slope at x=5 climbing east, the raised line from x=6 on.
	w.set(5, 63, 0, cartState(t, "minecraft:stone", nil))
	w.set(5, 64, 0, cartState(t, "minecraft:rail",
		map[string]string{"shape": "ascending_east"}))
	for x := int32(6); x <= 10; x++ {
		w.set(x, 64, 0, cartState(t, "minecraft:stone", nil))
		w.set(x, 65, 0, cartState(t, "minecraft:rail",
			map[string]string{"shape": "east_west"}))
	}
	m := NewMinecart(1, 1.5, 64.0625, 0.5)
	m.VX = 0.4
	for range 40 {
		m.Step(w, false)
	}
	if m.X < 7 {
		t.Fatalf("the cart stands at x=%v, want it past the slope onto the raised line", m.X)
	}
	if high := 65 + 0.0625; math.Abs(m.Y-high) > 0.01 {
		t.Errorf("the cart rides the raised line at y=%v, want its lane %v", m.Y, high)
	}
}

// TestACartRunOffTheLineDerailsOntoTheGroundAndStops: the end of the
// rails drops the cart onto the ground, where the derailed half-spends
// every tick brings it to a stand beside the line.
// vanilla: AbstractMinecart.comeOffTrack
func TestACartRunOffTheLineDerailsOntoTheGroundAndStops(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	cartGround(t, w, 0, 2, 64)
	// The ground the derail lands on, past the line's end: stone
	// without its rails.
	for x := int32(3); x <= 8; x++ {
		w.set(x, 63, 0, cartState(t, "minecraft:stone", nil))
	}
	m := NewMinecart(1, 1.5, 64.0625, 0.5)
	m.VX = 0.4
	for range 60 {
		m.Step(w, false)
	}
	if !m.OnGround {
		t.Fatalf("the cart off the line's end stands at y=%v, not on the ground", m.Y)
	}
	if snapped := 64.0; math.Abs(m.Y-snapped) > 1e-9 {
		t.Errorf("the derailed cart rests at y=%v, want the ground's top %v", m.Y, snapped)
	}
	if m.X > 4.5 {
		t.Errorf("the derailed cart ran on to x=%v, want the friction's stand", m.X)
	}
}

// TestACartIsPushedFromABodyAndTradesMotionWithAFurnaceCart: a body half a
// block west pushes the cart east by 0.05 and takes a quarter of that west;
// a furnace cart running into a plain one along its line keeps 0.95 of its
// motion and hands it on, and one across the line moves nothing.
// vanilla: AbstractMinecart.push, AbstractMinecart.pushOtherMinecart
func TestACartIsPushedFromABodyAndTradesMotionWithAFurnaceCart(t *testing.T) {
	const share = float64(float32(0.1)) * 0.5
	m := NewMinecart(1, 0, 64, 0)
	bx, bz := m.PushFrom(-0.5, 0, nil)
	if m.VX != share || m.VZ != 0 || bx != -share/4 || bz != 0 {
		t.Errorf("cart motion (%v, %v), body share (%v, %v); want (%v, 0) and (%v, 0)", m.VX, m.VZ, bx, bz, share, -share/4)
	}

	plain := NewMinecart(2, 0, 64, 0)
	furnace := NewMinecart(3, 1, 64, 0)
	furnace.Furnace = &FurnaceCart{}
	furnace.VX = -0.3
	plain.PushFrom(furnace.X, furnace.Z, furnace)
	if want := -0.3 - share; plain.VX != want || furnace.VX != -0.3*0.95 {
		t.Errorf("plain cart %v, furnace cart %v; want %v and %v", plain.VX, furnace.VX, want, -0.3*0.95)
	}

	across := NewMinecart(4, 0, 64, 0)
	across.Yaw = 90
	other := NewMinecart(5, 1, 64, 0)
	other.VX = -0.3
	across.PushFrom(other.X, other.Z, other)
	if across.VX != 0 || other.VX != -0.3 {
		t.Errorf("a cart facing south moved to %v, the other to %v; want neither changed", across.VX, other.VX)
	}
}
