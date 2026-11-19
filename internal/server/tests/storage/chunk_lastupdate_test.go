package storage

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestChunkSaveStampsTheGameTime checks the LastUpdate a saved chunk carries:
// the level's game time when it was saved, not the wall clock. A vanilla
// server reads the tag back as the chunk's last update time.
// vanilla: SerializableChunkData writes and reads LastUpdate (level.getGameTime())
func TestChunkSaveStampsTheGameTime(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, _ := servertest.Start(t, cfg)
	// The loop advances the world age every tick; hold it still so a save
	// stamps exactly the age set below. The nether and the end read the
	// overworld's game time, so one age covers all three.
	srv.StopTick()
	const age = 987654
	srv.SetWorldAge(age)

	pos := world.ChunkPos{}
	state := registry.BlockStates["minecraft:torch"]
	dimensions := []string{world.DimensionName, world.NetherName, world.EndName}
	for _, dimension := range dimensions {
		// The torch marks the chunk dirty and gets it written to its region
		// file without a player standing there.
		release := srv.HoldChunkInDimension(dimension, 5, 5)
		srv.SetBlockInDimension(dimension, 5, 64, 5, state)
		release()
	}
	srv.FlushSaves()

	for _, dimension := range dimensions {
		path := servertest.DimensionRegionPath(cfg.LevelName, dimension, pos)
		root := servertest.ReadBlockChunkNBT(t, path, pos)
		got, ok := nbt.Get[nbt.Long](root, "LastUpdate")
		if !ok {
			t.Errorf("%s: the saved chunk has no LastUpdate", dimension)
			continue
		}
		if got != age {
			t.Errorf("%s: LastUpdate = %d, want the game time %d, not a wall-clock stamp",
				dimension, got, age)
		}
	}
}
