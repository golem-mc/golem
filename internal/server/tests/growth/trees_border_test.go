package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/registry"
)

// TestOverworldTreesCrossChunkBorders checks that a tree feature can land on
// any column of its chunk, so a trunk stands beside a border and its canopy
// spills into the neighbour. Holding every trunk to the chunk's middle leaves
// a visible grid every sixteen blocks; a placement that writes past the border
// puts some trunks within a few blocks of it.
func TestOverworldTreesCrossChunkBorders(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelType = "normal"
	cfg.LevelSeed = "12345"
	cfg.GenerateStructures = false

	srv, _ := servertest.Start(t, cfg)

	state := func(name string, props map[string]string) uint16 {
		s, ok := registry.FindState(name, props)
		if !ok {
			t.Fatalf("the block registry has no %s", name)
		}
		return s
	}
	// A trunk stands on dirt: TrunkPlacer.placeBelowTrunkBlock.
	dirtState := state("minecraft:dirt", nil)
	logs := map[uint16]bool{
		state("minecraft:oak_log", nil):                            true,
		state("minecraft:oak_log", map[string]string{"axis": "x"}): true,
		state("minecraft:oak_log", map[string]string{"axis": "z"}): true,
		state("minecraft:birch_log", nil):                          true,
		state("minecraft:spruce_log", nil):                         true,
	}
	// What a tree puts above its column, so the walk steps through a canopy
	// to the ground the trunk stands on.
	grownOn := map[uint16]bool{
		state("minecraft:short_grass", nil): true,
		state("minecraft:snow", nil):        true,
	}
	for s := range logs {
		grownOn[s] = true
	}
	for _, name := range []string{"minecraft:oak_leaves", "minecraft:birch_leaves", "minecraft:spruce_leaves"} {
		for d := 1; d <= 7; d++ {
			grownOn[state(name, map[string]string{"distance": strconv.Itoa(d)})] = true
		}
	}

	// A trunk standing on the chunk's own border column, local 0 or 15. The
	// reviewed inset held every trunk at least its canopy reach inside the
	// chunk, so this count was zero and a trunk could never cross.
	onBorder := 0
	total := 0
	// Chunk (8, 0) has a border-column trunk; the 3x3 includes its canopy neighbours.
	srv.ScanOverworldSurfaces(7*16, -16, 10*16-1, 2*16-1, 140, func(x, y, z int32, s uint16) bool {
		if logs[s] && srv.BlockAt(x, y-1, z) == dirtState {
			total++
			lx, lz := x&15, z&15
			if lx == 0 || lx == 15 || lz == 0 || lz == 15 {
				onBorder++
			}
		}
		return grownOn[s]
	})

	t.Logf("%d of %d trunks stand on a chunk border column", onBorder, total)
	if total == 0 {
		t.Fatal("the sweep found no tree, want the world's woods and plains")
	}
	if onBorder == 0 {
		t.Errorf("no trunk in %d stands on a chunk border column, want trees placed on any column so canopies cross", total)
	}
}
