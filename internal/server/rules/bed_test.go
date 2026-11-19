package rules

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// bedWorld is the bed tests' world: the blocks set in a map with the real
// per-state collision read, so a body's box is judged against a bed's nine
// sixteenths rather than its whole cell.
type bedWorld struct {
	blocks map[[3]int32]uint16
	border *world.World
}

func (w bedWorld) BlockAt(x, y, z int32) uint16 { return w.blocks[[3]int32{x, y, z}] }

func (w bedWorld) InBorderBox(minX, minZ, maxX, maxZ float64) bool {
	return w.border == nil || w.border.InBorderBox(minX, minZ, maxX, maxZ)
}

func (w bedWorld) BoxCollides(minX, minY, minZ, maxX, maxY, maxZ float64) bool {
	const eps = 1e-7
	minCX := int32(math.Floor(minX-eps)) - 1
	maxCX := int32(math.Floor(maxX+eps)) + 1
	minCY := int32(math.Floor(minY-eps)) - 1
	maxCY := int32(math.Floor(maxY+eps)) + 1
	minCZ := int32(math.Floor(minZ-eps)) - 1
	maxCZ := int32(math.Floor(maxZ+eps)) + 1
	for y := minCY; y <= maxCY; y++ {
		for z := minCZ; z <= maxCZ; z++ {
			for x := minCX; x <= maxCX; x++ {
				if world.StateCollides(w.BlockAt(x, y, z), x, y, z, minX, minY, minZ, maxX, maxY, maxZ) {
					return true
				}
			}
		}
	}
	return false
}

// bedTestWorld is a flat stone floor with a bed's two halves standing on it,
// which is the plainest world a bed's respawn stand-up search reads. extra
// names cells of the bed's own level to fill with stone.
func bedTestWorld(t *testing.T, extra [][2]int32) bedWorld {
	t.Helper()
	stone := stateOf(t, "minecraft:stone")
	foot, ok := registry.FindState("minecraft:red_bed", map[string]string{
		"part": "foot", "facing": "south", "occupied": "false",
	})
	if !ok {
		t.Fatal("no bed foot state")
	}
	head, ok := registry.FindState("minecraft:red_bed", map[string]string{
		"part": "head", "facing": "south", "occupied": "false",
	})
	if !ok {
		t.Fatal("no bed head state")
	}
	blocks := map[[3]int32]uint16{}
	for x := int32(-2); x <= 8; x++ {
		for z := int32(-2); z <= 8; z++ {
			blocks[[3]int32{x, 63, z}] = stone
		}
	}
	blocks[[3]int32{5, 64, 0}] = foot
	blocks[[3]int32{5, 64, 1}] = head
	for _, cell := range extra {
		blocks[[3]int32{cell[0], 64, cell[1]}] = stone
	}
	return bedWorld{blocks: blocks}
}

// TestBedStandUpSpotWalksFromTheStoredSide checks the cell a body stands in
// at a bed whose head is south-facing, and that the stored angle picks which
// side the walk starts on.
func TestBedStandUpSpotWalksFromTheStoredSide(t *testing.T) {
	w := bedTestWorld(t, nil)

	// Facing south at yaw 0: the bed's west side.
	px, py, pz, ok := BedStandUpSpot(w, 5, 64, 1, [2]int32{0, 1}, 0)
	if !ok || px != 4.5 || py != 64 || pz != 1.5 {
		t.Errorf("stand-up = (%v, %v, %v), %v; want (4.5, 64, 1.5), true", px, py, pz, ok)
	}
	if yaw := LookAtYaw(px, pz, 5, 1); yaw != -90 {
		t.Errorf("look-at yaw from the west cell = %v, want -90", yaw)
	}

	// yaw 90 turns the start to the bed's east side.
	px, py, pz, ok = BedStandUpSpot(w, 5, 64, 1, [2]int32{0, 1}, 90)
	if !ok || px != 6.5 || py != 64 || pz != 1.5 {
		t.Errorf("stand-up from yaw 90 = (%v, %v, %v), %v; want (6.5, 64, 1.5), true", px, py, pz, ok)
	}
}

// TestBedStandUpSpotStandsOnTheBed checks that a bed every cell around is
// solid still hands the search the bed's own halves, whose nine sixteenths a
// body stands on rather than a whole cell.
func TestBedStandUpSpotStandsOnTheBed(t *testing.T) {
	w := bedTestWorld(t, [][2]int32{
		{4, 1}, {4, 0}, {4, -1}, {5, -1}, {6, -1},
		{6, 0}, {6, 1}, {6, 2}, {5, 2}, {4, 2},
	})
	px, py, pz, ok := BedStandUpSpot(w, 5, 64, 1, [2]int32{0, 1}, 0)
	if !ok || px != 5.5 || math.Abs(py-(64+9.0/16)) > 1e-9 || pz != 1.5 {
		t.Errorf("stand-up = (%v, %v, %v), %v; want the bed's own cell (5.5, %v, 1.5), true",
			px, py, pz, ok, 64+9.0/16)
	}
}

// TestBedStandUpSpotRefusesABoxedInBed checks that a bed every cell around,
// and above, is solid has no stand-up cell, which is what makes a respawn
// fall back to the world spawn.
func TestBedStandUpSpotRefusesABoxedInBed(t *testing.T) {
	w := bedTestWorld(t, [][2]int32{
		{4, 1}, {4, 0}, {4, -1}, {5, -1}, {6, -1},
		{6, 0}, {6, 1}, {6, 2}, {5, 2}, {4, 2},
	})
	stone := stateOf(t, "minecraft:stone")
	w.blocks[[3]int32{5, 65, 0}] = stone
	w.blocks[[3]int32{5, 65, 1}] = stone
	if _, _, _, ok := BedStandUpSpot(w, 5, 64, 1, [2]int32{0, 1}, 0); ok {
		t.Error("a bed boxed in on every side still found a stand-up cell")
	}
}

// TestBedStandUpSpotStandsOnABedBeside checks that a cell holding a second
// bed, whose collision is its nine sixteenths, takes the body the search is
// looking for the way the bed's own halves do.
func TestBedStandUpSpotStandsOnABedBeside(t *testing.T) {
	w := bedTestWorld(t, nil)
	head, ok := registry.FindState("minecraft:red_bed", map[string]string{
		"part": "head", "facing": "south", "occupied": "false",
	})
	if !ok {
		t.Fatal("no bed head state")
	}
	w.blocks[[3]int32{4, 64, 1}] = head
	px, py, pz, ok := BedStandUpSpot(w, 5, 64, 1, [2]int32{0, 1}, 0)
	if !ok || px != 4.5 || math.Abs(py-(64+9.0/16)) > 1e-9 || pz != 1.5 {
		t.Errorf("stand-up = (%v, %v, %v), %v; want the neighbouring bed's cell (4.5, %v, 1.5), true",
			px, py, pz, ok, 64+9.0/16)
	}
}

// TestBedStandUpSpotSkipsACellOverAPartHeightBlock checks that an empty cell
// whose only support is a slab is refused: the floor below must be a whole
// block, so the search moves on to the next cell instead of standing the body
// half a block down.
// vanilla: BlockGetter.getBlockFloorHeight
func TestBedStandUpSpotSkipsACellOverAPartHeightBlock(t *testing.T) {
	w := bedTestWorld(t, nil)
	slab, ok := registry.FindState("minecraft:stone_slab", map[string]string{"type": "bottom"})
	if !ok {
		t.Fatal("no stone slab state")
	}
	w.blocks[[3]int32{4, 63, 1}] = slab

	px, py, pz, ok := BedStandUpSpot(w, 5, 64, 1, [2]int32{0, 1}, 0)
	if !ok || px != 4.5 || py != 64 || pz != 0.5 {
		t.Errorf("stand-up = (%v, %v, %v), %v; want the next cell (4.5, 64, 0.5), true", px, py, pz, ok)
	}
}

// TestBedStandUpSpotBunkBedUsesTheLowerLevel checks that a bed stacked on a
// bed sends the search to the lower bed's cells before the upper mattress: a
// body stands beside the lower bed rather than on top of the upper one.
// vanilla: BedBlock.findBunkBedStandUpPosition
func TestBedStandUpSpotBunkBedUsesTheLowerLevel(t *testing.T) {
	stone := stateOf(t, "minecraft:stone")
	w := bedTestWorld(t, nil)
	foot, ok := registry.FindState("minecraft:red_bed", map[string]string{
		"part": "foot", "facing": "south", "occupied": "false",
	})
	if !ok {
		t.Fatal("no bed foot state")
	}
	head, ok := registry.FindState("minecraft:red_bed", map[string]string{
		"part": "head", "facing": "south", "occupied": "false",
	})
	if !ok {
		t.Fatal("no bed head state")
	}
	// A second bed on top of the first, its surrounding cells solid except
	// the one column the lower search reaches first: that cell's headroom
	// must stay open for a body to stand beside the lower bed, and it is no
	// stand-up cell at the upper level, the lower level below it being air.
	w.blocks[[3]int32{5, 65, 0}] = foot
	w.blocks[[3]int32{5, 65, 1}] = head
	for _, cell := range [][2]int32{
		{4, 0}, {4, -1}, {5, -1}, {6, -1},
		{6, 0}, {6, 1}, {6, 2}, {5, 2}, {4, 2},
	} {
		w.blocks[[3]int32{cell[0], 65, cell[1]}] = stone
	}

	px, py, pz, ok := BedStandUpSpot(w, 5, 65, 1, [2]int32{0, 1}, 0)
	if !ok || px != 4.5 || py != 64 || pz != 1.5 {
		t.Errorf("bunk bed stand-up = (%v, %v, %v), %v; want the lower bed's cell (4.5, 64, 1.5), true",
			px, py, pz, ok)
	}
}

// TestBedStandUpSpotTurnsWithTheFacing checks the side the search starts from
// for a bed facing north, east and west, the south case the other tests cover.
func TestBedStandUpSpotTurnsWithTheFacing(t *testing.T) {
	stone := stateOf(t, "minecraft:stone")
	cases := []struct {
		facing       string
		foot, head   [2]int32
		forward      [2]int32
		wantX, wantZ float64
	}{
		{"north", [2]int32{5, 0}, [2]int32{5, -1}, [2]int32{0, -1}, 6.5, -0.5},
		{"east", [2]int32{5, 1}, [2]int32{6, 1}, [2]int32{1, 0}, 6.5, 0.5},
		{"west", [2]int32{5, 1}, [2]int32{4, 1}, [2]int32{-1, 0}, 4.5, 0.5},
	}
	for _, c := range cases {
		foot, ok := registry.FindState("minecraft:red_bed", map[string]string{
			"part": "foot", "facing": c.facing, "occupied": "false",
		})
		if !ok {
			t.Fatalf("no bed foot state facing %s", c.facing)
		}
		head, ok := registry.FindState("minecraft:red_bed", map[string]string{
			"part": "head", "facing": c.facing, "occupied": "false",
		})
		if !ok {
			t.Fatalf("no bed head state facing %s", c.facing)
		}
		blocks := map[[3]int32]uint16{}
		for x := int32(-2); x <= 8; x++ {
			for z := int32(-2); z <= 8; z++ {
				blocks[[3]int32{x, 63, z}] = stone
			}
		}
		blocks[[3]int32{c.foot[0], 64, c.foot[1]}] = foot
		blocks[[3]int32{c.head[0], 64, c.head[1]}] = head

		px, py, pz, ok := BedStandUpSpot(bedWorld{blocks: blocks}, c.head[0], 64, c.head[1], c.forward, 0)
		if !ok || px != c.wantX || py != 64 || pz != c.wantZ {
			t.Errorf("%s bed stand-up = (%v, %v, %v), %v; want (%v, 64, %v), true",
				c.facing, px, py, pz, ok, c.wantX, c.wantZ)
		}
	}
}

// TestFacingAngleAtTheQuadrants checks the exact axis-aligned yaws, where
// the vanilla sine table and float64 trigonometry part ways.
func TestFacingAngleAtTheQuadrants(t *testing.T) {
	cases := []struct {
		yaw  float32
		dir  [2]int32
		want bool
	}{
		{-90, [2]int32{0, 1}, false},
		{-180, [2]int32{1, 0}, false},
		{-180, [2]int32{-1, 0}, true},
		{0, [2]int32{0, 1}, true},
		{0, [2]int32{0, -1}, false},
		{90, [2]int32{-1, 0}, true},
		{-90, [2]int32{1, 0}, true},
	}
	for _, c := range cases {
		if got := facingAngle(c.dir, c.yaw); got != c.want {
			t.Errorf("facingAngle(%v, %v) = %v, want %v", c.dir, c.yaw, got, c.want)
		}
	}
}

// TestBedStandUpSkipsAMagmaFloor checks that the first cell of the walk,
// standing on a magma block, gives way to the next, standing on stone.
func TestBedStandUpSkipsAMagmaFloor(t *testing.T) {
	w := bedTestWorld(t, nil)
	w.blocks[[3]int32{4, 63, 1}] = stateOf(t, "minecraft:magma_block")
	px, py, pz, ok := BedStandUpSpot(w, 5, 64, 1, [2]int32{0, 1}, 0)
	if !ok || px != 4.5 || py != 64 || pz != 0.5 {
		t.Errorf("stand-up = (%v, %v, %v), %v; want (4.5, 64, 0.5), true", px, py, pz, ok)
	}
}

func TestBedStandUpSpotKeepsTheWholeBoxInsideTheBorder(t *testing.T) {
	for _, tc := range []struct {
		name             string
		centerX, centerZ float64
		size             float64
		wantX, wantZ     float64
		ok               bool
	}{
		{"west edge", 9.3, 0, 10, 5.5, -0.5, true},
		{"east edge", -0.3, 0, 10, 0, 0, false},
		{"north edge", 5, 6.3, 10, 6.5, 2.5, true},
		{"south edge", 5, -3.7, 10, 4.5, 0.5, true},
		{"box fits", 9.1, 0, 10, 4.5, 1.5, true},
		{"no box fits", 5.5, 1.5, 0.5, 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := bedTestWorld(t, nil)
			border := world.NewBorder()
			border.SetCenter(tc.centerX, tc.centerZ)
			border.SetSize(tc.size)
			w.border = &world.World{}
			w.border.SetBorder(border)
			px, py, pz, ok := BedStandUpSpot(w, 5, 64, 1, [2]int32{0, 1}, 0)
			if ok != tc.ok || ok && (px != tc.wantX || py != 64 || pz != tc.wantZ) {
				t.Errorf("stand-up = (%g,%g,%g), %v; want (%g,64,%g), %v", px, py, pz, ok, tc.wantX, tc.wantZ, tc.ok)
			}
		})
	}
}

// vanilla: EntityType.isBlockDangerous, NodeEvaluator.isBurningBlock
func TestPlayerBlockDangerousBurnsInLitCampfiresAndLavaCauldrons(t *testing.T) {
	for _, tc := range []struct {
		state uint16
		want  bool
	}{
		{registry.MustState("minecraft:campfire", map[string]string{"lit": "true"}), true},
		{registry.MustState("minecraft:campfire", map[string]string{"lit": "false"}), false},
		{registry.BlockLavaCauldron.DefaultState(), true},
		{registry.BlockCauldron.DefaultState(), false},
		{registry.BlockCactus.DefaultState(), true},
	} {
		if got := playerBlockDangerous(tc.state); got != tc.want {
			t.Errorf("playerBlockDangerous(state %d) = %v, want %v", tc.state, got, tc.want)
		}
	}
}
