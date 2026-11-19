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
