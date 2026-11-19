package fluids

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// The waterlogged block as a water source, black-box: the water a
// waterlogged fence holds leaves through the faces the fence's shape
// leaves open — the thin sides and the open floor below it — and the water
// a bucket pours into a dry fence starts flowing the same way. The bottom
// slab's open sides drain while its closed ceiling feeds nothing above,
// and the stairs' full back face walls the water it holds in.
// vanilla: SimpleWaterloggedBlock.placeLiquid, FenceBlock.updateShape,
// FlowingFluid.spread

// expectWaterAt checks the cell holds water at the level given — 0 a
// source, 1-7 a flow, 8 the falling column.
func expectWaterAt(t *testing.T, srv *server.Server, x, y, z int32, level, label string) {
	t.Helper()
	name, props := servertest.StateProps(srv.BlockAt(x, y, z))
	if name != "minecraft:water" || props["level"] != level {
		t.Errorf("%s: the cell at (%d, %d, %d) = %s %v, want water level %s",
			label, x, y, z, name, props, level)
	}
}

// expectAirAt checks nothing stands at the cell.
func expectAirAt(t *testing.T, srv *server.Server, x, y, z int32, label string) {
	t.Helper()
	if got := srv.BlockAt(x, y, z); got != registry.BlockStates["minecraft:air"] {
		name, props := servertest.StateProps(got)
		t.Errorf("%s: the cell at (%d, %d, %d) = %s %v, want air", label, x, y, z, name, props)
	}
}

// waterloggedScene is an operator at a flat stretch of ground away from
// spawn, the tick loop stopped for the test to drive.
func waterloggedScene(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))
	srv.StopTick()
	return srv, c
}

// TestAWaterloggedFenceSpreadsItsWater checks the fence as a source: the
// water it holds leaves through all four thin sides at the flow's first
// step and pours down where the floor under it is open, as the falling
// column the open face lets through.
// vanilla: FenceBlock.updateShape, FlowingFluid.getSpread
func TestAWaterloggedFenceSpreadsItsWater(t *testing.T) {
	t.Parallel()
	srv, c := waterloggedScene(t)

	// An open floor under the fence: the water pours down where the
	// ground is not.
	servertest.SetFarmBlock(t, c, srv, 10, 63, 0, "minecraft:air", nil)
	servertest.SetFarmBlock(t, c, srv, 10, 64, 0, "minecraft:oak_fence",
		map[string]string{"waterlogged": "true"})
	srv.TickPhysics(40)

	expectWaterAt(t, srv, 9, 64, 0, "1", "the fence's west side")
	expectWaterAt(t, srv, 11, 64, 0, "1", "the fence's east side")
	expectWaterAt(t, srv, 10, 64, -1, "1", "the fence's north side")
	expectWaterAt(t, srv, 10, 64, 1, "1", "the fence's south side")
	expectWaterAt(t, srv, 10, 63, 0, "8", "the pour through the open floor")
	name, props := servertest.StateProps(srv.BlockAt(10, 64, 0))
	if name != "minecraft:oak_fence" || props["waterlogged"] != "true" {
		t.Errorf("the fence after the spread = %s %v, want it still waterlogged", name, props)
	}
}

// TestAWaterloggedSlabLetsWaterOutItsOpenSides checks the bottom slab's
// faces: the water it holds drains sideways through the open half of each
// side face, and the ceiling its top half makes feeds nothing above.
// vanilla: SlabBlock.updateShape, FlowingFluid.spread
func TestAWaterloggedSlabLetsWaterOutItsOpenSides(t *testing.T) {
	t.Parallel()
	srv, c := waterloggedScene(t)

	servertest.SetFarmBlock(t, c, srv, 10, 64, 0, "minecraft:oak_slab",
		map[string]string{"type": "bottom", "waterlogged": "true"})
	srv.TickPhysics(40)

	expectWaterAt(t, srv, 9, 64, 0, "1", "the slab's west side")
	expectWaterAt(t, srv, 11, 64, 0, "1", "the slab's east side")
	expectWaterAt(t, srv, 10, 64, -1, "1", "the slab's north side")
	expectWaterAt(t, srv, 10, 64, 1, "1", "the slab's south side")
	expectAirAt(t, srv, 10, 65, 0, "above the slab's closed ceiling")
}

// TestAWaterloggedStairsWallsItsTallHalf checks the stairs' faces: the
// full back face the tall half makes holds the water in, while the low
// half's side drains. Stone walls hem the stairs east and west, so the
// south leak cannot curl around the ends — the corridor north of the
// stairs is fed by nothing but the back face itself.
// vanilla: StairBlock.updateShape, FlowingFluid.canPassThroughWall
func TestAWaterloggedStairsWallsItsTallHalf(t *testing.T) {
	t.Parallel()
	srv, c := waterloggedScene(t)

	for z := int32(-2); z <= 2; z++ {
		servertest.SetFarmBlock(t, c, srv, 9, 64, z, "minecraft:stone", nil)
		servertest.SetFarmBlock(t, c, srv, 11, 64, z, "minecraft:stone", nil)
	}
	servertest.SetFarmBlock(t, c, srv, 10, 64, 0, "minecraft:oak_stairs",
		map[string]string{"facing": "north", "waterlogged": "true"})
	srv.TickPhysics(40)

	expectAirAt(t, srv, 10, 64, -1, "behind the stairs' tall half")
	expectWaterAt(t, srv, 10, 64, 1, "1", "the open side above the low half")
}
