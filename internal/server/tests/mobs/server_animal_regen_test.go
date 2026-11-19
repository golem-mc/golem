package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
)

// entityEntryPath is the entity region file holding a chunk of the named
// dimension.
func entityEntryPath(level, dimension string, pos world.ChunkPos) string {
	folder := strings.TrimPrefix(dimension, "minecraft:")
	return filepath.Join(level, "dimensions", "minecraft", folder, "entities",
		fmt.Sprintf("r.%d.%d.mca", pos.X>>5, pos.Z>>5))
}

// entityEntryOffset is the region-file offset of a chunk's entity entry, zero
// when the region file or the entry is absent.
func entityEntryOffset(t *testing.T, path string, pos world.ChunkPos) uint32 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read entity region: %v", err)
	}
	index := (int(pos.X&31) + int(pos.Z&31)*32) * 4
	return binary.BigEndian.Uint32(raw[index:]) >> 8
}

// deleteEntityEntry clears a chunk's entity entry in its region file, leaving
// whatever other entries the file holds in place.
func deleteEntityEntry(t *testing.T, path string, pos world.ChunkPos) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return // no file is a missing entry too
		}
		t.Fatalf("read entity region: %v", err)
	}
	index := (int(pos.X&31) + int(pos.Z&31)*32) * 4
	binary.BigEndian.PutUint32(raw[index:], 0)
	binary.BigEndian.PutUint32(raw[4096+index:], 0)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write entity region: %v", err)
	}
}

// TestAChunkWhoseEntityEntryIsGoneGrowsNoNewAnimals reloads a chunk whose
// herd was killed and whose entity entry was deleted: the populated chunk was
// saved to disk, so the reload reads its terrain and grows no animals a second
// time, a missing entity entry meaning no entities.
func TestAChunkWhoseEntityEntryIsGoneGrowsNoNewAnimals(t *testing.T) {
	// Not parallel: an overworld of real chunks is the heaviest thing the
	// generation pool grows, the way the other natural-generation tests are.
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelSeed = "3"

	srv, err := server.NewSeeded(cfg, servertest.RootSeed(t, cfg))
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	srv.SetUsercacheFile(filepath.Join(t.TempDir(), "usercache.json"))
	srv.SetSpawning(false)
	servertest.HoldTicks(t, srv)
	t.Cleanup(srv.StopTick)

	pos := world.ChunkPos{X: world.ChunkOf(32), Z: world.ChunkOf(-16)}
	release := srv.HoldChunk(32, -16)
	servertest.UntilTicks(t, srv, 400, "condition", func() bool { return srv.MobCount() == 4 })

	// Kill the herd the chunk's generation grew.
	for _, m := range srv.Mobs() {
		srv.KillMob(m.ID)
	}
	servertest.UntilTicks(t, srv, 200, "condition", func() bool { return srv.MobCount() == 0 })

	// A populated chunk is saved when it is released, so the reload reads its
	// terrain rather than generating and repopulating it. The entity entry it
	// held is gone with the herd: a chunk holding no entities keeps none.
	release()
	srv.FlushSaves()
	srv.Shutdown()

	path := entityEntryPath(cfg.LevelName, world.DimensionName, pos)
	deleteEntityEntry(t, path, pos)
	if offset := entityEntryOffset(t, path, pos); offset != 0 {
		t.Fatalf("the entity entry was not deleted: offset %d", offset)
	}

	restarted := *cfg
	again, err := server.NewSeeded(&restarted, servertest.RootSeed(t, &restarted))
	if err != nil {
		t.Fatalf("restart server: %v", err)
	}
	again.SetUsercacheFile(filepath.Join(t.TempDir(), "usercache.json"))
	again.SetSpawning(false)
	servertest.HoldTicks(t, again)

	againRelease := again.HoldChunk(32, -16)
	// The wake pass runs the tick after the chunk lands; give it a few.
	across := again.TicksRun() + 40
	servertest.UntilTicks(t, again, 200, "the wake pass ticks", func() bool { return again.TicksRun() >= across })
	if n := again.MobCount(); n != 0 {
		t.Errorf("the reloaded chunk grew %d animals, want the none its terrain was saved without", n)
	}
	againRelease()
	again.Shutdown()
}

// TestAnEmptyChunkEntityEntryIsRemoved checks that saving a chunk that holds
// no entities removes its entry rather than writing an empty list. The nether
// and the end are covered beside the overworld: the same store and the same
// wake decide all three.
func TestAnEmptyChunkEntityEntryIsRemoved(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	srv, _ := servertest.Start(t, cfg)
	srv.StopTick()
	srv.SetGameRule("mob_drops", false)

	for _, dim := range []string{world.DimensionName, world.NetherName, world.EndName} {
		t.Run(dim, func(t *testing.T) {
			pos := world.ChunkPos{}
			srv.SpawnMobInDimension("minecraft:pig", dim, 8.5, 64, 8.5)
			srv.TickMobs(1) // nobody holds the chunk: the pig goes to its file
			path := entityEntryPath(cfg.LevelName, dim, pos)
			if offset := entityEntryOffset(t, path, pos); offset == 0 {
				t.Fatal("the pig's chunk has no entity entry to remove")
			}

			// Wake the pig back and kill it, leaving the chunk holding
			// nothing.
			release := srv.HoldChunkInDimension(dim, 8, 8)
			srv.TickMobs(1)
			id := int32(0)
			for _, m := range srv.MobsInDimension(dim) {
				id = m.ID
			}
			if id == 0 {
				t.Fatal("the pig did not wake with its chunk")
			}
			if !srv.KillMob(id) {
				t.Fatal("the pig was not there to kill")
			}
			srv.TickMobs(1) // the death tick moves the body out of the herd
			release()

			srv.TickMobs(2) // the chunk leaves and its empty entry is removed
			if offset := entityEntryOffset(t, path, pos); offset != 0 {
				t.Errorf("the empty chunk's entity entry was written at offset %d, want it removed", offset)
			}
		})
	}
}
