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

// TestBoatSeatOffset checks where a passenger's seat sits along the hull:
// the single-passenger offset at the hull's centre — 0.15 along in a chest
// boat — and the pair's +0.2 and -0.6 once two ride, the Animal's own 0.2
// added to whichever seat it holds.
// vanilla: AbstractBoat.getPassengerAttachmentPoint, getSinglePassengerXOffset
func TestBoatSeatOffset(t *testing.T) {
	cases := []struct {
		label        string
		raft, chest  bool
		count, index int
		animal       bool
		want         float64
	}{
		{"a lone rider sits centred", false, false, 1, 0, false, 0},
		{"a lone animal sits centred too", false, false, 1, 0, true, 0},
		{"a chest boat's lone rider sits 0.15 along", false, true, 1, 0, false, 0.15},
		{"a chest boat's lone animal sits 0.15 along", false, true, 1, 0, true, 0.15},
		{"the first of two sits 0.2 along", false, false, 2, 0, false, 0.2},
		{"the second of two sits 0.6 aft", false, false, 2, 1, false, -0.6},
		{"an animal in the bow seat sits 0.4 along", false, false, 2, 0, true, 0.4},
		{"an animal in the stern seat sits 0.4 aft", false, false, 2, 1, true, -0.4},
		{"a raft's lone rider sits 0.5 up", true, false, 1, 0, false, 0},
		{"a chest raft's lone rider sits 0.15 along", true, true, 1, 0, false, 0.15},
	}
	for _, tc := range cases {
		wantY := BoatSeatY
		if tc.raft {
			wantY = RaftSeatY
		}
		dx, dy, dz := BoatSeatOffset(tc.raft, tc.chest, tc.count, tc.index, tc.animal, 0)
		if math.Abs(dx) > 1e-9 || math.Abs(dy-wantY) > 1e-9 || math.Abs(dz-tc.want) > 1e-9 {
			t.Errorf("BoatSeatOffset(raft=%v, chest=%v, count=%d, index=%d, animal=%v) = (%v %v %v), want along %v, up %v (%s)",
				tc.raft, tc.chest, tc.count, tc.index, tc.animal, dx, dy, dz, tc.want, wantY, tc.label)
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

// TestBoatSpotStandsTheRayCrossAndRefusesWhatDoesNotFit checks the two
// halves of the spot a boat item's use stands its boat at: the point the
// look crossed into what it hit — on the water's surface for a source —
// and the refusal where the boat's box would stand inside the blocks of.
func TestBoatSpotStandsTheRayCrossAndRefusesWhatDoesNotFit(t *testing.T) {
	water := stateOf(t, "minecraft:water")
	stone := stateOf(t, "minecraft:stone")

	// A look straight down into a pool whose whole neighbourhood is
	// water: the clip stops at the pool's topmost source, and the boat
	// stands where the ray crossed its surface.
	pool := spotWorld{
		hits: func() (int32, int32, int32, bool, bool) { return 5, 63, 5, true, true },
		blocks: map[[3]int32]uint16{
			{5, 62, 5}: water, {5, 63, 5}: water,
			{4, 62, 5}: water, {6, 62, 5}: water,
			{5, 62, 4}: water, {5, 62, 6}: water,
			{4, 63, 5}: water, {6, 63, 5}: water,
			{5, 63, 4}: water, {5, 63, 6}: water,
		},
	}
	x, y, z, ok := BoatSpot(pool, 5.5, 70, 5.5, 0, -1, 0, 4.5)
	if !ok || !BoatFits(pool, x, y, z) {
		t.Fatal("the pool's own spot was refused")
	}
	if x != 5.5 || z != 5.5 {
		t.Errorf("the boat stands at (%v %v), want the column looked down (5.5, 5.5)", x, z)
	}
	if math.Abs(y-(63+8.0/9)) > 1e-9 {
		t.Errorf("the boat stands at y=%v, want the surface %v", y, 63+8.0/9)
	}

	// The same look over a pool's edge cell: the box's reach out of the
	// water crosses the stone of the ground beside it, and the spot is
	// refused.
	lone := spotWorld{
		hits: func() (int32, int32, int32, bool, bool) { return 5, 62, 5, true, true },
		blocks: map[[3]int32]uint16{
			{5, 62, 5}: water,
			{4, 62, 5}: stone, {6, 62, 5}: stone, {5, 62, 4}: stone, {5, 62, 6}: stone,
		},
	}
	if x, y, z, ok := BoatSpot(lone, 5.5, 70, 5.5, 0, -1, 0, 4.5); !ok || BoatFits(lone, x, y, z) {
		t.Error("a boat was stood where its box crosses the pool's side")
	}

	// A look at a block: the boat stands on the block's top, where the
	// ray crossed into the cube.
	land := spotWorld{
		hits: func() (int32, int32, int32, bool, bool) { return 2, 63, 2, false, true },
		blocks: map[[3]int32]uint16{
			{2, 63, 2}: stone,
			{1, 63, 2}: stone, {3, 63, 2}: stone, {2, 63, 1}: stone, {2, 63, 3}: stone,
		},
	}
	x, y, z, ok = BoatSpot(land, 2.5, 70, 2.5, 0, -1, 0, 4.5)
	if !ok || !BoatFits(land, x, y, z) {
		t.Fatal("the ground's own spot was refused")
	}
	if x != 2.5 || z != 2.5 || y != 64 {
		t.Errorf("the boat stands at (%v %v %v), want the block's top (2.5, 64, 2.5)", x, y, z)
	}

	// A look that stops at nothing stands nothing.
	miss := spotWorld{hits: func() (int32, int32, int32, bool, bool) { return 0, 0, 0, false, false }}
	if _, _, _, ok := BoatSpot(miss, 0, 70, 0, 0, -1, 0, 4.5); ok {
		t.Error("a boat was stood at a look that stopped at nothing")
	}

	// The air above the ground is where the box has to fit: a stone
	// filling the cell above the water the boat would stand on is the
	// refusal the box's height asks of.
	low := spotWorld{
		hits: func() (int32, int32, int32, bool, bool) { return 5, 62, 5, true, true },
		blocks: map[[3]int32]uint16{
			{5, 62, 5}: water, {5, 63, 5}: stone,
			{4, 62, 5}: water, {6, 62, 5}: water, {5, 62, 4}: water, {5, 62, 6}: water,
		},
	}
	if x, y, z, ok := BoatSpot(low, 5.5, 70, 5.5, 0, -1, 0, 4.5); !ok || BoatFits(low, x, y, z) {
		t.Error("a boat was stood into the stone over the water")
	}
}

// TestRayIntoBoxCrossesAtTheFirstFace checks the point the ray reaches the
// box at, entry by the face whose crossing comes last, the origin already
// inside crossing at itself, and the slab the ray only slides past being
// no crossing.
func TestRayIntoBoxCrossesAtTheFirstFace(t *testing.T) {
	// Straight down into a box a block wide topping at 62.888: the cross
	// is the surface itself.
	x, y, z, ok := rayIntoBox(5.5, 70, 5.5, 0, -1, 0, 5, 62, 5, 62+8.0/9)
	if !ok || x != 5.5 || z != 5.5 || math.Abs(y-(62+8.0/9)) > 1e-9 {
		t.Errorf("the down look crossed at (%v %v %v) (%v), want the surface (5.5, %v, 5.5)", x, y, z, ok, 62+8.0/9)
	}
	// A diagonal look entering by the side: the cross is where the side
	// was crossed, not where the floor was.
	x, y, z, ok = rayIntoBox(5.5, 63.4, 5.5, 1, -1, 0, 6, 62, 5, 63)
	if !ok || math.Abs(x-6) > 1e-9 || math.Abs(y-62.9) > 1e-9 || z != 5.5 {
		t.Errorf("the side's look crossed at (%v %v %v) (%v), want the side face (6, 62.9, 5.5)", x, y, z, ok)
	}
	// An origin inside the box crosses at itself.
	x, y, z, ok = rayIntoBox(6.2, 62.5, 5.5, 1, -1, 0, 6, 62, 5, 63)
	if !ok || x != 6.2 || y != 62.5 || z != 5.5 {
		t.Errorf("the inside origin crossed at (%v %v %v) (%v), want itself", x, y, z, ok)
	}
	// A look that crosses no face within the others' ranges is no
	// crossing: the ray reaches the surface's height only past the box's
	// far side.
	if _, _, _, ok := rayIntoBox(5.5, 70, 5.5, 1, -1, 0, 6, 62, 5, 63); ok {
		t.Error("a look past the box's side was taken for a crossing")
	}
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

// TestABoatsDismountTriesThePassengerPoses checks that the step tries each
// of a rider's dismount poses in turn, so a spot with room only for a
// crawling body still takes the rider, at the swimming pose's height.
// vanilla: AbstractBoat.getDismountLocationForPassenger, Player.getDismountPoses
func TestABoatsDismountTriesThePassengerPoses(t *testing.T) {
	// A stone step at y=60 with a ceiling at y=62: only the 0.6-tall
	// swimming box fits between them.
	low := spotWorld{
		blocks: map[[3]int32]uint16{
			{0, 60, 1}: stateOf(t, "minecraft:stone"),
			{0, 62, 1}: stateOf(t, "minecraft:stone"),
		},
	}
	_, y, _ := BoatDismountSpot(low, 0.5, 60.5, 0.5, 0, PlayerDismountPoses)
	if math.Abs(y-61) > 1e-9 {
		t.Errorf("the dismount stands at y=%v, want the step's top 61 under the low ceiling", y)
	}
}
