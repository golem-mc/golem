package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestEndPillarCrystals saves the chunks the ring of pillars stands in and
// lists the end's saved entities: ten end crystals, each on its pillar's
// bedrock cap. Each subtest saves its own server's End; the overworld is the
// harness's flat one, since only the End's own terrain is read.
func TestEndPillarCrystals(t *testing.T) {
	t.Parallel()
	// None from a second wake of the same chunks.
	t.Run("EndPillarsCarryOneCrystalEach", func(t *testing.T) {
		t.Parallel()
		testEndPillarCrystals(t, false)
	})
	// Saved before the tick's first wake, the file is written by the wake
	// for a write.
	t.Run("EndPillarsSavedBeforeTheirWakeKeepTheirCrystals", func(t *testing.T) {
		t.Parallel()
		testEndPillarCrystals(t, true)
	})
}

func testEndPillarCrystals(t *testing.T, saveFirst bool) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, _ := servertest.Start(t, cfg)
	srv.StopTick()

	bedrock, _ := registry.FindState("minecraft:bedrock", nil)
	var releases []func()
	for cx := int32(-3); cx <= 2; cx++ {
		for cz := int32(-3); cz <= 2; cz++ {
			releases = append(releases, srv.HoldChunkInDimension(world.EndName, cx*16+8, cz*16+8))
		}
	}
	if saveFirst {
		// An edit marks each chunk for the save that lands before any wake.
		stone, _ := registry.FindState("minecraft:stone", nil)
		for cx := int32(-3); cx <= 2; cx++ {
			for cz := int32(-3); cz <= 2; cz++ {
				srv.SetBlockInDimension(world.EndName, cx*16+15, 250, cz*16+15, stone)
			}
		}
	} else {
		srv.TickMobs(1)
	}
	for _, release := range releases {
		release()
	}
	srv.FlushSaves() // the releases' chunk saves land and the chunks leave
	srv.TickMobs(1)  // the crystals leave with them, into their files
	srv.FlushSaves()

	w := world.NewDimensionWorld(cfg.LevelName, false, world.NewEndGenerator(0))
	crystals := 0
	for cx := int32(-3); cx <= 2; cx++ {
		for cz := int32(-3); cz <= 2; cz++ {
			tags, _, err := w.LoadChunkEntities(world.ChunkPos{X: cx, Z: cz})
			if err != nil {
				t.Fatalf("load entities of (%d, %d): %v", cx, cz, err)
			}
			for _, tag := range tags {
				c, ok := nbt.CompoundView(tag)
				if !ok {
					t.Fatalf("chunk (%d, %d) holds %T, want compound", cx, cz, tag)
				}
				if id, _ := nbt.Get[nbt.String](c, "id"); id != "minecraft:end_crystal" {
					t.Errorf("chunk (%d, %d) holds %q, want only end crystals", cx, cz, id)
					continue
				}
				pos, _ := nbt.Get[nbt.List](c, "Pos")
				at := func(i int) int32 { return int32(math.Floor(float64(pos.Items[i].(nbt.Double)))) }
				x, y, z := at(0), at(1)-1, at(2)
				if got := srv.LoadBlockInDimension(world.EndName, x, y, z); got != bedrock {
					t.Errorf("crystal at (%d, %d, %d) stands on block %d, want bedrock", x, y+1, z, got)
				}
				crystals++
			}
		}
	}
	if crystals != 10 {
		t.Errorf("%d end crystals saved, want 10", crystals)
	}
}
