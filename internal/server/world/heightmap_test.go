package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestHeightmapColumn checks one column's three heightmaps against vanilla's
// predicates: WORLD_SURFACE counts anything that is not air, both motion maps
// count blocksMotion() or a non-empty fluid state, and only
// MOTION_BLOCKING_NO_LEAVES drops the leaves. vanilla: Heightmap.Types
func TestHeightmapColumn(t *testing.T) {
	t.Parallel()

	stone := registry.BlockStates["minecraft:stone"]
	leaves := rsState(t, "minecraft:oak_leaves", nil)
	water := rsState(t, "minecraft:water", nil)
	waterlogged := rsState(t, "minecraft:ladder", map[string]string{"facing": "north", "waterlogged": "true"})
	seagrass := rsState(t, "minecraft:tall_seagrass", map[string]string{"half": "lower"})
	caveAir := rsState(t, "minecraft:cave_air", nil)

	for _, tc := range []struct {
		name                      string
		dim                       string
		blocks                    map[[3]int32]uint16
		surface, motion, noLeaves int32
	}{
		{
			// A leaf blocks motion, so it tops MOTION_BLOCKING but is left
			// out of MOTION_BLOCKING_NO_LEAVES.
			"leaves", DimensionName,
			map[[3]int32]uint16{{0, 63, 0}: stone, {0, 64, 0}: leaves},
			129, 129, 128,
		},
		{
			// A water column tops both motion maps at its surface.
			"water column", DimensionName,
			map[[3]int32]uint16{{0, 63, 0}: stone, {0, 64, 0}: water, {0, 65, 0}: water},
			130, 130, 130,
		},
		{
			// A waterlogged block holds water, so it blocks motion too.
			"waterlogged block", DimensionName,
			map[[3]int32]uint16{{0, 63, 0}: stone, {0, 64, 0}: waterlogged},
			129, 129, 129,
		},
		{
			// An always-water plant holds a water source in every state.
			"water plant", DimensionName,
			map[[3]int32]uint16{{0, 63, 0}: stone, {0, 64, 0}: seagrass},
			129, 129, 129,
		},
		{
			// A column of air reads zero in every map.
			"empty column", DimensionName,
			nil,
			0, 0, 0,
		},
		{
			// The overworld's top block is y=319: 384 above the floor.
			"world top", DimensionName,
			map[[3]int32]uint16{{0, 319, 0}: stone},
			384, 384, 384,
		},
		{
			// cave_air is air, so it is not ground.
			"cave air", DimensionName,
			map[[3]int32]uint16{{0, 63, 0}: stone, {0, 64, 0}: caveAir},
			128, 128, 128,
		},
		{
			"void air and unknown state", DimensionName,
			map[[3]int32]uint16{{0, 63, 0}: stone, {0, 64, 0}: registry.BlockVoidAir.DefaultState(), {0, 65, 0}: 65535},
			128, 128, 128,
		},
		{
			// The nether is 256 tall from y=0, so its top block reads 256.
			"nether", NetherName,
			map[[3]int32]uint16{{0, 254, 0}: stone, {0, 255, 0}: leaves},
			256, 256, 255,
		},
		{
			// The end is 256 tall from y=0 too; its floor block reads one.
			"end", EndName,
			map[[3]int32]uint16{{0, 0, 0}: stone},
			1, 1, 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := &World{dim: dimensionShapes[tc.dim]}
			c := newChunk(0, 0)
			for pos, state := range tc.blocks {
				c.SetBlock(pos[0], pos[1], pos[2], state)
			}
			surface, motion, noLeaves := w.heightmapColumn(c, 0, 0)
			if surface != tc.surface || motion != tc.motion || noLeaves != tc.noLeaves {
				t.Errorf("surface/motion/noLeaves = %d/%d/%d, want %d/%d/%d",
					surface, motion, noLeaves, tc.surface, tc.motion, tc.noLeaves)
			}
		})
	}
}

// TestNoLeavesHeight checks the world scan MOTION_BLOCKING_NO_LEAVES reads
// against real chunk storage: the leaf exclusion drops a canopy's leaves down
// to the ground, and a column with no ground, or in a chunk that is not loaded,
// reads one below the world floor. vanilla:
// Heightmap.Types.MOTION_BLOCKING_NO_LEAVES
func TestNoLeavesHeight(t *testing.T) {
	t.Parallel()

	ground := registry.BlockStates["minecraft:grass_block"]
	leaves := rsState(t, "minecraft:oak_leaves", nil)

	c := newChunk(0, 0)
	c.SetBlock(3, 63, 5, ground)
	c.SetBlock(3, 64, 5, leaves)

	w := &World{
		dim:    dimensionShapes[DimensionName],
		chunks: map[ChunkPos]*loadedChunk{{X: 0, Z: 0}: {chunk: c}},
	}
	for _, tc := range []struct {
		name string
		x, z int32
		want int32
	}{
		{"leaves above ground", 3, 5, 63},
		{"empty column", 1, 1, w.dim.minY - 1},
		{"unloaded column", 40, 5, w.dim.minY - 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := w.NoLeavesHeight(tc.x, tc.z); got != tc.want {
				t.Errorf("NoLeavesHeight(%d, %d) = %d, want %d", tc.x, tc.z, got, tc.want)
			}
		})
	}
}

// TestHeightmapTopMatchesFullScan checks the shared top-section bound against
// a scan of every block in the dimension, for a void chunk, blocks at each
// dimension limit, one high block and storage below the dimension floor.
func TestHeightmapTopMatchesFullScan(t *testing.T) {
	t.Parallel()

	stone := registry.BlockStates["minecraft:stone"]
	for _, tc := range []struct {
		name   string
		dim    string
		blocks map[[3]int32]uint16
	}{
		{"void end", EndName, nil},
		{"end limits", EndName, map[[3]int32]uint16{{0, 0, 0}: stone, {15, 255, 15}: stone}},
		{"end high one column", EndName, map[[3]int32]uint16{{7, 200, 3}: stone}},
		{"end below floor", EndName, map[[3]int32]uint16{{1, -5, 1}: stone}},
		{"overworld limits", DimensionName, map[[3]int32]uint16{{0, -64, 0}: stone, {15, 319, 15}: stone}},
		{"overworld void", DimensionName, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := &World{dim: dimensionShapes[tc.dim]}
			c := newChunk(0, 0)
			for pos, state := range tc.blocks {
				c.SetBlock(pos[0], pos[1], pos[2], state)
			}
			for lx := range int32(16) {
				for lz := range int32(16) {
					var want [3]int32
					for y := w.dim.minY + w.dim.height - 1; y >= w.dim.minY; y-- {
						state := c.BlockAt(lx, y, lz)
						if IsAirState(state) {
							continue
						}
						raw := y - w.dim.minY + 1
						if want[0] == 0 {
							want[0] = raw
						}
						if want[1] == 0 && motionBlockingByState[state] {
							want[1] = raw
						}
						if want[2] == 0 && noLeavesBlockingByState[state] {
							want[2] = raw
						}
					}
					s, m, n := w.heightmapColumn(c, lx, lz)
					if [3]int32{s, m, n} != want {
						t.Fatalf("column %d,%d = %d/%d/%d, want %v", lx, lz, s, m, n, want)
					}
				}
			}
		})
	}
}
