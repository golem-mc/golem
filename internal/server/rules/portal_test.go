package rules

import (
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
	corner, changes, ok := BuildNetherPortal(w, w, world.SpawnPos{X: 0, Y: 64, Z: 0}, PortalBuildRadius, axis, 0, 127)
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
