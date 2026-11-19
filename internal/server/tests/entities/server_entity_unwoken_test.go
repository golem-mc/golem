package entities

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// TestAnItemInAChunkNoTickHasWokenIsSaved drops an item into a chunk that is
// loaded but that no tick has woken yet, which a frozen game keeps that way:
// the save writes the item into the chunk's entity file all the same, rather
// than losing it because the herd never read the chunk.
func TestAnItemInAChunkNoTickHasWokenIsSaved(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	srv, _ := servertest.Start(t, cfg)
	srv.RunLine("tick freeze")
	release := srv.HoldChunk(1000, 1000)
	defer release()
	srv.SpawnItemEntity(1000.5, 64, 1000.5, "minecraft:diamond", 3)

	srv.FlushMobs()
	srv.Shutdown()

	pos := world.ChunkPos{X: world.ChunkOf(1000), Z: world.ChunkOf(1000)}
	servertest.EntityFileIs(t, cfg.LevelName, pos, "minecraft:item", func(compound nbt.Compound) {
		item, _ := nbt.Get[nbt.Compound](compound, "Item")
		if count, _ := nbt.Get[nbt.Int](item, "count"); count != 3 {
			t.Errorf("the saved stack's count = %d, want 3", count)
		}
	})
}

// TestASavedFreshChunkKeepsItsAnimals saves an item in fresh overworld
// chunks no tick has woken, the chunks whose generation grows a herd: the
// file the save writes is the one every later wake reads instead of growing
// the animals, so it carries the herd beside the item rather than leaving
// the chunk without the animals its generation owes it.
func TestASavedFreshChunkKeepsItsAnimals(t *testing.T) {
	// Not parallel: real overworld chunks, the way the herd tests are.
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelSeed = "3"

	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	srv.SetSpawning(false)
	srv.StartTick()
	srv.RunLine("tick freeze")

	// Chunks the level seed's dice grow a herd in: cows and chickens.
	at := [][2]int32{{32, -32}, {16, -32}}
	var releases []func()
	// A released chunk queues its save; the flush lands it before the
	// level directory is removed under it.
	t.Cleanup(func() {
		for _, release := range releases {
			release()
		}
		srv.FlushSaves()
	})
	for _, a := range at {
		releases = append(releases, srv.HoldChunk(a[0], a[1]))
		srv.SpawnItemEntity(float64(a[0])+0.5, 200, float64(a[1])+0.5, "minecraft:diamond", 1)
	}
	srv.FlushMobs()
	srv.StopTick()
	srv.FlushSaves()

	animals := 0
	for _, a := range at {
		pos := world.ChunkPos{X: world.ChunkOf(float64(a[0])), Z: world.ChunkOf(float64(a[1]))}
		items := 0
		for _, compound := range servertest.ReadChunkEntities(t, cfg.LevelName, pos) {
			switch id, _ := nbt.Get[nbt.String](compound, "id"); id {
			case "minecraft:item":
				items++
			case "minecraft:chicken", "minecraft:cow", "minecraft:pig", "minecraft:sheep":
				animals++
			}
		}
		if items != 1 {
			t.Errorf("chunk %v saved %d items, want the one dropped in it", pos, items)
		}
	}
	if animals < 2 {
		t.Errorf("the saved fresh chunks hold %d animals, want the herd their generation grows", animals)
	}
}
