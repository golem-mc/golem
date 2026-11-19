package rules

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TestBoatFluidSurfaceHeight checks the height a fluid cell's surface
// rests a boat at, the shape the fluid engine's own clips read: a source's
// and a falling column's eight ninths of the cell, a running flow's own
// level thinned by, and the whole cell under the same fluid — water under
// water, or waterlogged by water.
func TestBoatFluidSurfaceHeight(t *testing.T) {
	cases := []struct {
		label, fluid, fluidProps, above, aboveProps string
		want                                        float64
	}{
		{"a source under air", "minecraft:water", "", "minecraft:air", "", 8.0 / 9},
		{"a source named so", "minecraft:water", "level=0", "minecraft:air", "", 8.0 / 9},
		{"water under water", "minecraft:water", "", "minecraft:water", "", 1},
		{"water under waterlogged", "minecraft:water", "", "minecraft:oak_fence", "waterlogged=true", 1},
		{"the first step of a flow", "minecraft:water", "level=1", "minecraft:air", "", 7.0 / 9},
		{"the far end of a flow", "minecraft:water", "level=7", "minecraft:air", "", 1.0 / 9},
		{"a falling column under air", "minecraft:water", "level=8", "minecraft:air", "", 8.0 / 9},
		{"lava under air", "minecraft:lava", "", "minecraft:air", "", 8.0 / 9},
		{"lava under lava", "minecraft:lava", "", "minecraft:lava", "", 1},
		{"waterlogged is not water above lava", "minecraft:lava", "", "minecraft:oak_fence", "waterlogged=true", 8.0 / 9},
	}
	for _, tc := range cases {
		fluid := stateWith(t, tc.fluid, registry.ParseStateProps(tc.fluidProps))
		above := stateWith(t, tc.above, registry.ParseStateProps(tc.aboveProps))
		if got := FluidSurfaceHeight(fluid, above); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("FluidSurfaceHeight(%s %q, %s %q) = %v, want %v (%s)",
				tc.fluid, tc.fluidProps, tc.above, tc.aboveProps, got, tc.want, tc.label)
		}
	}
}

// spotWorld is the plainest world a placement can read of: the clip's
// answer stated outright, the blocks set in a map, and the collision the
// same cells-over-span the world's own check runs.
type spotWorld struct {
	hits   func() (x, y, z int32, liquid, hit bool)
	blocks map[[3]int32]uint16
}

func (w spotWorld) ClipAnyFluid(ox, oy, oz, dx, dy, dz, reach float64) (int32, int32, int32, bool, bool) {
	return w.hits()
}

func (w spotWorld) BlockAt(x, y, z int32) uint16 { return w.blocks[[3]int32{x, y, z}] }

// BoxCollides is the plain collision the stub's cells carry: a cell whose
// block has no collision shape is walked through, any other is a crossing —
// the stone and the shape-less plants the cases set.
func (w spotWorld) BoxCollides(minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	for cx := int32(math.Floor(minX)); cx <= int32(math.Floor(maxX)); cx++ {
		for cy := int32(math.Floor(minY)); cy <= int32(math.Floor(maxY)); cy++ {
			for cz := int32(math.Floor(minZ)); cz <= int32(math.Floor(maxZ)); cz++ {
				name, _, ok := registry.BlockStateName(w.blocks[[3]int32{cx, cy, cz}])
				if ok && !registry.NoCollision[name] {
					return true
				}
			}
		}
	}
	return false
}

// TestABoatsDismountSpotStepsTheRiderOutOntoTheFloor checks where a
// passenger steps off a boat: beside the hull along their own yaw, onto the
// floor there, and back on top of the hull where the water under the step
// leaves no floor.
// vanilla: AbstractBoat.getDismountLocationForPassenger
func TestABoatsDismountSpotStepsTheRiderOutOntoTheFloor(t *testing.T) {
	const width, height = 0.6, 1.8
	single := []DismountPose{{Dims: PlayerDimensions{Width: width, Height: height}}}
	// A stone floor at y=59, the hull resting on its top: the step stands on
	// the floor beside the hull, half the escape gap along yaw 0 (+Z).
	land := spotWorld{
		blocks: map[[3]int32]uint16{
			{0, 59, 0}: stateOf(t, "minecraft:stone"),
			{1, 59, 0}: stateOf(t, "minecraft:stone"),
			{0, 59, 1}: stateOf(t, "minecraft:stone"),
			{1, 59, 1}: stateOf(t, "minecraft:stone"),
		},
	}
	dims := PlayerDimensionsOf(protocol.PoseStanding)
	if dims.Width != width || dims.Height != height {
		t.Fatalf("the standing player is %vx%v, want %vx%v", dims.Width, dims.Height, width, height)
	}
	wantZ := 0.5 + (BoatHalfWidth*2*math.Sqrt2+width+1e-5)/2
	x, y, z := BoatDismountSpot(land, 0.5, 60, 0.5, 0, single)
	if x != 0.5 || math.Abs(z-wantZ) > 1e-9 {
		t.Errorf("the dismount stands at (%v %v), want the step out along yaw 0 (0.5, %v)", x, z, wantZ)
	}
	if math.Abs(y-60) > 1e-9 {
		t.Errorf("the dismount stands at y=%v, want the floor's top 60", y)
	}

	// Water under the step leaves no floor, so the passenger comes back on
	// top of the hull.
	water := spotWorld{
		blocks: map[[3]int32]uint16{{0, 60, 1}: stateOf(t, "minecraft:water")},
	}
	_, y, _ = BoatDismountSpot(water, 0.5, 60.5, 0.5, 0, single)
	if math.Abs(y-(60.5+BoatHeight)) > 1e-9 {
		t.Errorf("the dismount over the water stands at y=%v, want the hull's top %v", y, 60.5+BoatHeight)
	}

	// A water plant or a bubble column holds water too, so the step over one
	// leaves no floor either.
	for _, plant := range []string{
		"minecraft:seagrass", "minecraft:tall_seagrass", "minecraft:kelp",
		"minecraft:kelp_plant", "minecraft:bubble_column",
	} {
		wet := spotWorld{
			blocks: map[[3]int32]uint16{
				{0, 59, 1}: stateOf(t, "minecraft:stone"),
				{0, 60, 1}: stateOf(t, plant),
			},
		}
		_, y, _ = BoatDismountSpot(wet, 0.5, 60.5, 0.5, 0, single)
		if math.Abs(y-(60.5+BoatHeight)) > 1e-9 {
			t.Errorf("the dismount over a %s stands at y=%v, want the hull's top %v", plant, y, 60.5+BoatHeight)
		}
	}
}
