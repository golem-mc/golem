package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// vanilla: AbstractMinecart.tick, getDefaultGravity, comeOffTrack
func TestMinecartSpawnedInWaterUsesPreviousTickFluid(t *testing.T) {
	w := newTestWorld()
	water := registry.BlockWater.DefaultState()
	for x := int32(-1); x <= 2; x++ {
		for y := int32(64); y <= 67; y++ {
			for z := int32(-1); z <= 2; z++ {
				w.set(x, y, z, water)
			}
		}
	}
	m := NewMinecart(1, .5, 65, .5)
	m.VX, m.VY, m.VZ = .25, .3, .15
	for range 3 {
		m.Step(w, false)
	}
	// Vanilla: X 0.1805 and Y 0.2137, versus 0.1715 and 0.2437
	// when the first tick uses water's cap and gravity.
	if math.Abs(m.VX-.1805) > 1e-6 || math.Abs(m.VY-.213655) > 1e-6 {
		t.Fatalf("motion=(%g,%g), want (0.1805,0.213655)", m.VX, m.VY)
	}
}

// vanilla: AbstractMinecart.tick, OldMinecartBehavior.moveAlongTrack
func TestMinecartRailWaterDragUsesPreviousTickFluid(t *testing.T) {
	w := newTestWorld()
	cartGround(t, w, 0, 4, 64)
	waterRail := cartState(t, "minecraft:rail", map[string]string{"shape": "east_west", "waterlogged": "true"})
	for x := int32(0); x <= 4; x++ {
		w.set(x, 64, 0, waterRail)
	}
	m := NewMinecart(1, .5, 64.0625, .5)
	m.VX = .3
	m.Step(w, false)
	if math.Abs(m.X-.8) > 1e-6 || math.Abs(m.VX-.3*.96) > 1e-6 {
		t.Fatalf("first tick: X=%g VX=%g, want air cap and drag", m.X, m.VX)
	}
	cartGround(t, w, 0, 4, 64)
	m.Step(w, false)
	if math.Abs(m.X-1) > 1e-6 || math.Abs(m.VX-.3*.96*.96*.95) > 1e-6 {
		t.Fatalf("second tick: X=%g VX=%g, want cached water cap and drag", m.X, m.VX)
	}
	beforeX, beforeVX := m.X, m.VX
	m.Step(w, false)
	if math.Abs(m.X-beforeX-beforeVX) > 1e-6 || math.Abs(m.VX-beforeVX*.96) > 1e-6 {
		t.Fatalf("third tick: X=%g VX=%g, want air cap and drag", m.X, m.VX)
	}
}

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

// TestACartRidingEastDoesNotFlipOnEveryTick: a cart rolling east keeps
// the same heading and the same flip — the angle turned is wrapped before
// the near-reverse test, so a whole turn's difference does not spin the
// client's model half a turn every other tick.
// vanilla: OldMinecartBehavior.tick, Mth.wrapDegrees
func TestACartRidingEastDoesNotFlipOnEveryTick(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	cartGround(t, w, 0, 20, 64)
	m := NewMinecart(1, 1.5, 64.0625, 0.5)
	m.VX = 0.4
	m.Step(w, false)
	if math.Abs(float64(m.Yaw)) > 1e-4 || !m.Flipped {
		t.Fatalf("after one step the cart faces yaw=%v flipped=%v, want the east heading 0, flipped", m.Yaw, m.Flipped)
	}
	m.Step(w, false)
	if math.Abs(float64(m.Yaw)) > 1e-4 || !m.Flipped {
		t.Errorf("after two steps the cart faces yaw=%v flipped=%v, want it still eastbound at 0, flipped", m.Yaw, m.Flipped)
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

// TestACartTakesTheSouthEastCurve: the south-east curve's line joins the
// south and the east neighbour — a cart rolling north onto it comes out
// eastbound, its z run ending at the curve's lane, its x run beginning.
// vanilla: RailBlock's south_east, OldMinecartBehavior.moveAlongTrack
func TestACartTakesTheSouthEastCurve(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	// A north-south line north through (2, 64, 3) — the curve there — and
	// an east-west line out of it eastward.
	for z := int32(4); z <= 6; z++ {
		w.set(2, 63, z, cartState(t, "minecraft:stone", nil))
		w.set(2, 64, z, cartState(t, "minecraft:rail",
			map[string]string{"shape": "north_south"}))
	}
	w.set(2, 63, 3, cartState(t, "minecraft:stone", nil))
	w.set(2, 64, 3, cartState(t, "minecraft:rail",
		map[string]string{"shape": "south_east"}))
	for x := int32(3); x <= 6; x++ {
		w.set(x, 63, 3, cartState(t, "minecraft:stone", nil))
		w.set(x, 64, 3, cartState(t, "minecraft:rail",
			map[string]string{"shape": "east_west"}))
	}

	// A cart rolling north: its z run ends through the curve, its x run
	// east along the line beyond.
	m := NewMinecart(1, 2.5, 64.0625, 5.5)
	m.VZ = -0.4
	for range 20 {
		m.Step(w, false)
	}
	if m.Z > 3.51 {
		t.Fatalf("the cart stands at z=%v, want it north through the curve", m.Z)
	}
	if m.X <= 3.5 {
		t.Errorf("the cart stands at x=%v after the curve, want the eastbound run", m.X)
	}
	if math.Abs(m.VZ) > 0.001 || m.VX < 0.02 {
		t.Errorf("after the curve the cart runs (%v, %v), want east alone", m.VX, m.VZ)
	}
}

// TestADarkPoweredRailHaltsASlowCart: the halt a dark powered rail lays
// on a cart that creeps onto it — the hundredth of a block under which a
// cart is stopped dead.
// vanilla: OldMinecartBehavior.moveAlongTrack's haltTrack half
func TestADarkPoweredRailHaltsASlowCart(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	cartGround(t, w, 0, 4, 64)
	w.set(2, 64, 0, cartState(t, "minecraft:powered_rail",
		map[string]string{"shape": "east_west", "powered": "false"}))

	m := NewMinecart(1, 1.5, 64.0625, 0.5)
	m.VX = 0.2
	for range 8 {
		m.Step(w, false)
	}
	if x := m.X; x < 1.5 || x > 4 {
		t.Fatalf("the halted cart drifted to x=%v", x)
	}
	if m.VX != 0 {
		t.Errorf("the dark rail left the cart running at %v, want a dead stop", m.VX)
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

// TestACartsMotionDirTurnsItsYawAQuarterClockwise checks the dismount's
// travel direction, turned about for a flipped cart.
func TestACartsMotionDirTurnsItsYawAQuarterClockwise(t *testing.T) {
	for _, tc := range []struct {
		yaw      float32
		flipped  bool
		wdx, wdz int32
	}{
		{0, false, -1, 0}, {90, false, 0, -1}, {180, false, 1, 0}, {270, false, 0, 1},
		{0, true, 1, 0},
	} {
		m := NewMinecart(1, 0, 0, 0)
		m.Yaw, m.Flipped = tc.yaw, tc.flipped
		if dx, dz := m.MotionDir(); dx != tc.wdx || dz != tc.wdz {
			t.Errorf("yaw %v flipped %v went (%d, %d), want (%d, %d)", tc.yaw, tc.flipped, dx, dz, tc.wdx, tc.wdz)
		}
	}
}

// TestACartKeepsItsOwnRollingPaceOverTheRidersDefault: a cart with a pace of
// its own rolls further than the empty default's twenty-fifth off each tick.
// vanilla: AbstractMinecartContainer.applyNaturalSlowdown
func TestACartKeepsItsOwnRollingPaceOverTheRidersDefault(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	cartGround(t, w, 0, 40, 64)
	plain := NewMinecart(1, 1.5, 64.0625, 0.5)
	plain.VX = 0.3
	own := NewMinecart(2, 1.5, 64.0625, 0.5)
	own.VX = 0.3
	own.Slowdown = 0.995
	plainX, _, _ := trackOf(plain, w, 5)
	ownX, _, _ := trackOf(own, w, 5)
	if ownX <= plainX {
		t.Errorf("a cart with its own pace reached x=%v, want past the default cart's %v", ownX, plainX)
	}
}

// TestACartOnARailKeepsNoVerticalMotion checks the rail holds a cart up: its
// vertical motion is zeroed every rail tick, resting or rolling, so gravity
// never builds up while it rides the line.
// vanilla: AbstractMinecart.applyNaturalSlowdown
func TestACartOnARailKeepsNoVerticalMotion(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	cartGround(t, w, -40, 40, 64)
	for _, tc := range []struct {
		name   string
		vx     float64
		ridden bool
	}{
		{"resting", 0, false},
		{"ridden", 0.2, true},
	} {
		m := NewMinecart(1, 0.5, 64.0625, 0.5)
		m.VX = tc.vx
		for range 50 {
			m.Step(w, tc.ridden)
		}
		if m.VY != 0 {
			t.Errorf("the %s cart on the rail has VY %v after 50 ticks, want 0", tc.name, m.VY)
		}
	}
}

// TestMinecartBoardableTurnsAWitherAway: a moving cart never picks up a
// wither, which may not ride though it is pushable; a pig it takes.
// vanilla: WitherBoss.canRide
func TestMinecartBoardableTurnsAWitherAway(t *testing.T) {
	wither := NewMob(TypeWither, 1, 0.5, 64, 0.5, 1)
	if !wither.Pushable() {
		t.Fatal("the wither is not pushable; the test no longer reaches the canRide rule")
	}
	if MinecartBoardable(wither) {
		t.Error("MinecartBoardable(wither) = true, want false")
	}
	if pig := NewMob(TypePig, 2, 0.5, 64, 0.5, 1); !MinecartBoardable(pig) {
		t.Error("MinecartBoardable(pig) = false, want true")
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
