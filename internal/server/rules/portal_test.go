package rules

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The portal rules as ordinary unit tests, on frames built in the map a
// testWorld is. The black-box tests in internal/server/server_portals_test.go remain
// the proof of the lighting and the travelling around these decisions; these
// are the checks of the shapes and the collapse decision themselves.

// buildFrame closes a 2-wide, 3-tall interior around (x, y, z) along the X
// axis: obsidian under, over and at both ends of it, and the given state in
// the six cells the vanilla frame leaves for the portal. Nothing else is set,
// so the rest of the world is air.
func buildFrame(w testWorld, x, y, z int32, interior uint16) {
	for i := x - 1; i <= x+2; i++ {
		w.set(i, y-1, z, ObsidianState) // the floor
		w.set(i, y+3, z, ObsidianState) // the ceiling
	}
	for j := y; j <= y+2; j++ {
		w.set(x-1, j, z, ObsidianState) // the west end
		w.set(x+2, j, z, ObsidianState) // the east end
	}
	if interior != 0 {
		for i := x; i <= x+1; i++ {
			for j := y; j <= y+2; j++ {
				w.set(i, j, z, interior)
			}
		}
	}
}

// litPortal stands a lit portal in a complete frame, the 2x3 along X at
// (0..1, 64..66, 0), filled with axis=x portal blocks.
func litPortal(w testWorld) {
	buildFrame(w, 0, 64, 0, NetherPortalX)
}

// TestACompleteFrameShapesAValidPortal checks the shape walk on the frame
// everyone knows: a fire lit inside - the ignition's ask, with air the rest
// of the interior - walks out to a 2x3 interior closed on every side. The
// same frame closes no portal along the other axis, a Z portal's floor being
// where this frame's walls stand.
func TestACompleteFrameShapesAValidPortal(t *testing.T) {
	w := testWorld{}
	buildFrame(w, 0, 64, 0, 0)
	w.set(0, 64, 0, FireState)

	cells, ok := PortalShape(w, 1, 0, 0, 64, 0, true)
	if !ok {
		t.Fatal("PortalShape said nothing of a complete frame with a fire in it")
	}
	want := map[[3]int32]bool{
		{0, 64, 0}: true, {0, 65, 0}: true, {0, 66, 0}: true,
		{1, 64, 0}: true, {1, 65, 0}: true, {1, 66, 0}: true,
	}
	if len(cells) != len(want) {
		t.Fatalf("the interior has %d cells, want %d", len(cells), len(want))
	}
	for _, cell := range cells {
		if !want[cell] {
			t.Errorf("cell %+v is in the interior, want only the six inside the frame", cell)
		}
		delete(want, cell)
	}
	if len(want) > 0 {
		t.Errorf("the interior is missing %+v", want)
	}

	if _, ok := PortalShape(w, 0, 1, 0, 64, 0, true); ok {
		t.Error("PortalShape found a Z-running portal in an X-running frame, want no such shape")
	}
}

// TestABrokenFrameAndAPunchedHoleShapeNothing checks the two frames a
// revalidation throws out: one with the obsidian taken out of its side, and
// one with air in the interior - a portal somebody has punched a hole in,
// which the ignition's allowAir is not there to excuse.
func TestABrokenFrameAndAPunchedHoleShapeNothing(t *testing.T) {
	w := testWorld{}
	litPortal(w)
	delete(w, [3]int32{2, 66, 0}) // a corner of the east side broken off

	if _, ok := PortalShape(w, 1, 0, 0, 64, 0, false); ok {
		t.Error("PortalShape accepted a frame with a broken corner, want it refused")
	}

	w = testWorld{}
	litPortal(w)
	delete(w, [3]int32{1, 65, 0}) // a hole punched in the middle of the interior

	if _, ok := PortalShape(w, 1, 0, 0, 64, 0, false); ok {
		t.Error("PortalShape accepted a hole in the interior, want it refused")
	}
	if _, ok := PortalShape(w, 1, 0, 0, 64, 0, true); !ok {
		t.Error("PortalShape refused air in the interior the ignition is lighting into, want it accepted")
	}
}

// TestAPortalCollapsesWhenItsFrameBreaks checks the neighbour-update decision
// on the classic case: an obsidian frame block becoming air pulls the portal
// beside it out whole, and the decision says so by handing back the portal
// block whose frame was lost, for the server to collapse around.
func TestAPortalCollapsesWhenItsFrameBreaks(t *testing.T) {
	w := testWorld{}
	litPortal(w)
	w.set(2, 65, 0, airState) // the middle of the east side broken

	collapsing := CollapsingPortals(w, 2, 65, 0)
	if len(collapsing) != 1 {
		t.Fatalf("CollapsingPortals returned %d positions, want the one portal block beside the break", len(collapsing))
	}
	if collapsing[0] != [3]int32{1, 65, 0} {
		t.Errorf("CollapsingPortals returned %+v, want the portal block at (1, 65, 0)", collapsing[0])
	}
}

// TestAPortalHoldsOverTheChangesItSurvives checks the other side of the
// neighbour-update decision: a change in front of the portal - off its axis,
// where neither side of the frame is - is none of the portal's business, and
// so is a change to a portal block itself, which is how /setblock can build
// a portal one block at a time and leave it standing.
func TestAPortalHoldsOverTheChangesItSurvives(t *testing.T) {
	w := testWorld{}
	litPortal(w)

	if got := CollapsingPortals(w, 1, 65, 1); len(got) != 0 {
		t.Errorf("a change in front of the portal collapsed %+v, want the portal left standing", got)
	}

	// The change itself being a portal block: /setblock's way of placing
	// portal blocks one at a time.
	if got := CollapsingPortals(w, 1, 65, 0); len(got) != 0 {
		t.Errorf("a change of portal into portal collapsed %+v, want the portal left standing", got)
	}

	// The change being obsidian: the frame being built, block by block,
	// never takes the portal beside it.
	w.set(2, 64, 0, ObsidianState)
	if got := CollapsingPortals(w, 2, 64, 0); len(got) != 0 {
		t.Errorf("a change to obsidian collapsed %+v, want the portal left standing", got)
	}
}

// The search weighs every portal block in its square, takes the nearest in
// 3D (ties to the lower Y) and answers with the bottom of its column, where
// the arriving player's feet go; blocks outside the square or the border
// never count.
func TestPortalSearchTakesTheNearestInTheSquare(t *testing.T) {
	w := testWorld{}
	w.set(10, 64, 5, NetherPortalZ) // 10 blocks out, its column's foot
	w.set(10, 65, 5, NetherPortalZ) // the cell of the column at the centre's Y
	w.set(0, 64, 13, NetherPortalZ) // 13 blocks out in the centre's chunk
	w.set(17, 70, 0, NetherPortalZ) // outside the 16-block square
	w.set(-3, 70, 0, NetherPortalZ) // nearest, but past the border

	search := PortalSearch{At: world.SpawnPos{X: 0, Y: 65, Z: 0}, Radius: 16, InBorder: func(x, _ int32) bool { return x >= 0 }}
	n := 0
	for pos := range search.Chunks() {
		search.ScanChunk(w, pos, 0, 127)
		n++
	}
	if n != 9 {
		t.Errorf("scanned %d chunks, want the 9 touching x and z -16..16", n)
	}
	spot, ok := search.Found()
	if !ok || spot != (world.SpawnPos{X: 10, Y: 64, Z: 5}) {
		t.Errorf("found %+v (%v), want the foot of the nearest column (10, 64, 5)", spot, ok)
	}

	empty := PortalSearch{At: world.SpawnPos{X: 0, Y: 65, Z: 0}, Radius: 16}
	empty.ScanChunk(testWorld{}, world.ChunkPos{}, 0, 127)
	if _, ok := empty.Found(); ok {
		t.Error("a search of an empty chunk found a portal, want none")
	}
}

func TestEntityPortalStep(t *testing.T) {
	var p EntityPortal
	kind, due := p.Step(NetherPortalX, PortalTransitionTime, 300)
	if kind != PortalNether || !due || p.Cooldown != 300 {
		t.Fatalf("first tick = %v %v cooldown %d", kind, due, p.Cooldown)
	}
	if _, due := p.Step(NetherPortalX, PortalTransitionTime, 300); due {
		t.Fatal("crossed again during cooldown")
	}
}

func TestPortalTurn(t *testing.T) {
	if got := PortalTurn(PortalEnd, [2]int32{1, 0}, [2]int32{1, 0}, 30); got != 60 {
		t.Fatalf("end turn = %v", got)
	}
	if got := PortalTurn(PortalNether, [2]int32{1, 0}, [2]int32{0, 1}, 0); got != 90 {
		t.Fatalf("nether turn = %v", got)
	}
}

func TestNetherPortalExitFloorsTheScaledCoordinate(t *testing.T) {
	if toNether, at, r := NetherPortalExit(false, -0.5, 70.5, 17); !toNether || at != (world.SpawnPos{X: -1, Y: 70, Z: 2}) || r != 16 {
		t.Errorf("overworld (-0.5, 70.5, 17) leads to nether=%v %+v within %d, want the nether (-1, 70, 2) within 16", toNether, at, r)
	}
	if toNether, at, r := NetherPortalExit(true, -0.3, 40, 1.2); toNether || at != (world.SpawnPos{X: -3, Y: 40, Z: 9}) || r != 128 {
		t.Errorf("nether (-0.3, 40, 1.2) leads to nether=%v %+v within %d, want the overworld (-3, 40, 9) within 128", toNether, at, r)
	}
}

func TestOnlyTheOverworldAndTheNetherLightAFrame(t *testing.T) {
	w := testWorld{}
	buildFrame(w, 0, 64, 0, 0)
	if lit, ok := LightPortal(w, world.NetherName, 0, 64, 0); !ok || len(lit) != 6 || lit[0].State != NetherPortalX {
		t.Errorf("the nether lit %d cells (%v), want the six of an X portal", len(lit), ok)
	}
	if _, ok := LightPortal(w, world.EndName, 0, 64, 0); ok {
		t.Error("the end lit a frame")
	}
}

func TestAFarGlobalEventIsHeardThirtyTwoBlocksOut(t *testing.T) {
	if x, y, z := GlobalEventAt(0, 0, 0, 100, 0, 0); x != 32 || y != 0 || z != 0 {
		t.Errorf("heard at (%v, %v, %v), want (32, 0, 0)", x, y, z)
	}
	if x, _, _ := GlobalEventAt(0, 0, 0, 20, 0, 0); x != 20 {
		t.Errorf("a near event heard at x %v, want the source's 20", x)
	}
}

// siteWorld is a world for BuildNetherPortal: the cells set in it, stone
// below floor everywhere else and air above, inside a border of the columns
// x >= minX.
type siteWorld struct {
	testWorld
	floor, minX int32
}

var siteStone = registry.MustState("minecraft:stone", nil)

func (w siteWorld) BlockAt(x, y, z int32) uint16 {
	if s, ok := w.testWorld[[3]int32{x, y, z}]; ok {
		return s
	}
	if y < w.floor {
		return siteStone
	}
	return airState
}

func (w siteWorld) MotionBlockingHeight(x, z int32) int32 {
	for y := int32(127); y >= 0; y-- {
		if w.BlockAt(x, y, z) != airState {
			return y
		}
	}
	return -1
}

func (w siteWorld) ContainsBlock(x, _ int32) bool { return x >= w.minX }

func (w siteWorld) ClampBlock(x, z int32) (int32, int32) { return max(x, w.minX), z }

func newSiteWorld(floor int32) siteWorld {
	return siteWorld{testWorld: testWorld{}, floor: floor, minX: -1 << 20}
}

// buildIn runs BuildNetherPortal from (0, 64, 0) in the nether's height and
// applies its changes to w.
func buildIn(t *testing.T, w siteWorld, axis [2]int32) world.SpawnPos {
	t.Helper()
	corner, changes, ok := BuildNetherPortal(w, w, world.SpawnPos{X: 0, Y: 64, Z: 0}, axis, 0, 127)
	if !ok {
		t.Fatal("BuildNetherPortal found no site and built no platform")
	}
	for _, ch := range changes {
		w.set(ch.X, ch.Y, ch.Z, ch.State)
	}
	return corner
}

// checkPortal checks the 2x3 portal of state along axis at corner and its
// sixteen frame cells.
func checkPortal(t *testing.T, w siteWorld, corner world.SpawnPos, axis [2]int32, state uint16) {
	t.Helper()
	for width := int32(-1); width < 3; width++ {
		for height := int32(-1); height < 4; height++ {
			x, y, z := corner.X+width*axis[0], corner.Y+height, corner.Z+width*axis[1]
			want := state
			if width == -1 || width == 2 || height == -1 || height == 3 {
				want = ObsidianState
			}
			if got := w.BlockAt(x, y, z); got != want {
				t.Errorf("(%d, %d, %d) = %d, want %d", x, y, z, got, want)
			}
		}
	}
}

func TestSpiralAroundStartsAtTheCentreAndCoversTheSquare(t *testing.T) {
	var got [][2]int32
	seen := map[[2]int32]bool{}
	for x, z := range spiralAround(10, 20, 16) {
		if len(got) < 9 {
			got = append(got, [2]int32{x - 10, z - 20})
		}
		if x < 10-16 || x > 10+16 || z < 20-16 || z > 20+16 || seen[[2]int32{x, z}] {
			t.Fatalf("(%d, %d) is outside the square or repeated", x, z)
		}
		seen[[2]int32{x, z}] = true
	}
	want := [][2]int32{{0, 0}, {1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1}}
	if !slices.Equal(got, want) {
		t.Errorf("the spiral starts %v, want %v", got, want)
	}
	if len(seen) != 33*33 {
		t.Errorf("the spiral visits %d columns, want %d", len(seen), 33*33)
	}
}

func TestABuiltPortalStandsOnTheFloorAtTheOriginAlongTheSourceAxis(t *testing.T) {
	for _, axis := range [][2]int32{{1, 0}, {0, 1}} {
		w := newSiteWorld(64)
		state := NetherPortalX
		if axis[1] != 0 {
			state = NetherPortalZ
		}
		corner := buildIn(t, w, axis)
		if corner != (world.SpawnPos{X: 0, Y: 64, Z: 0}) {
			t.Errorf("axis %v: built at %+v, want the origin", axis, corner)
		}
		checkPortal(t, w, corner, axis, state)
	}
}

// TestABuiltPortalStandsBesideAPillarAtTheOrigin walls the origin's columns
// in stone up to y 100: the nearest site with room for the frame and both
// faces is 4 blocks out on the floor, not on the pillar's top.
func TestABuiltPortalStandsBesideAPillarAtTheOrigin(t *testing.T) {
	w := newSiteWorld(64)
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			for y := int32(64); y <= 100; y++ {
				w.set(x, y, z, siteStone)
			}
		}
	}
	axis := [2]int32{1, 0}
	corner := buildIn(t, w, axis)
	if dx, dz := corner.X, corner.Z; corner.Y != 64 || dx*dx+dz*dz != 16 {
		t.Errorf("built at %+v, want on the floor 4 blocks from the origin", corner)
	}
	checkPortal(t, w, corner, axis, NetherPortalX)
}

// TestABuiltPortalStaysInTheBorder: the origin column's portal would cross
// the border, so it is built inside it.
func TestABuiltPortalStaysInTheBorder(t *testing.T) {
	w := newSiteWorld(64)
	w.minX = 3
	corner := buildIn(t, w, [2]int32{1, 0})
	if corner.X < 3 {
		t.Errorf("built at %+v, outside the border x >= 3", corner)
	}
}

// TestABuiltPortalWithNoSiteStandsOnAPlatform: with nothing solid in reach
// the portal is forced at y 70, the lowest start, on an obsidian platform
// with air on both faces.
func TestABuiltPortalWithNoSiteStandsOnAPlatform(t *testing.T) {
	w := newSiteWorld(-1000)
	axis := [2]int32{0, 1}
	corner := buildIn(t, w, axis)
	if corner != (world.SpawnPos{X: 0, Y: 70, Z: -1}) {
		t.Fatalf("built at %+v, want (0, 70, -1): one step back along the axis at the lowest start", corner)
	}
	checkPortal(t, w, corner, axis, NetherPortalZ)
	for _, x := range [2]int32{-1, 1} {
		for z := int32(-1); z <= 0; z++ {
			if got := w.BlockAt(x, 69, z); got != ObsidianState {
				t.Errorf("platform (%d, 69, %d) = %d, want obsidian", x, z, got)
			}
		}
	}
}

func TestABuiltPortalFailsWithNoRoomUnderTheLogicalTop(t *testing.T) {
	w := newSiteWorld(-1000)
	if _, _, ok := BuildNetherPortal(w, w, world.SpawnPos{Y: 64}, [2]int32{1, 0}, 0, 70); ok {
		t.Error("built a portal with no room for a start between 70 and the top less 9")
	}
}

func TestARiderInAPortalHoldsItsCountAndIsNeverDue(t *testing.T) {
	var p EntityPortal
	p.StepRider(EndPortalState, 300)
	if p.Inside != 0 || p.Cooldown != 0 {
		t.Fatalf("a rider's first tick in the portal = %+v, want nothing counted", p)
	}
	p = EntityPortal{Inside: 3, Kind: PortalEnd, Cooldown: 2}
	p.StepRider(EndPortalState, 300)
	if p.Inside != 0 || p.Cooldown != 300 {
		t.Fatalf("a rider in the portal on cooldown = %+v, want the cooldown restarted", p)
	}
	p = EntityPortal{Inside: 3, Kind: PortalEnd}
	p.StepRider(EndPortalState, 300)
	if p.Inside != 3 {
		t.Fatalf("a rider's count in the portal = %d, want it held at 3", p.Inside)
	}
	p.StepRider(0, 300)
	if p.Inside != 0 {
		t.Fatalf("a rider's count out of the portal = %d, want it decayed to 0", p.Inside)
	}
}
