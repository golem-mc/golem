package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/console"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// savedPigs returns the UUID of every pig the entity files of the chunks
// hold, a duplicate once per copy.
func savedPigs(t *testing.T, level string, chunks ...world.ChunkPos) []uuid.UUID {
	t.Helper()
	var pigs []uuid.UUID
	for _, pos := range chunks {
		for _, compound := range servertest.ReadChunkEntities(t, level, pos) {
			if kind, _ := nbt.Get[nbt.String](compound, "id"); kind != "minecraft:pig" {
				continue
			}
			words, _ := nbt.Get[nbt.IntArray](compound, "UUID")
			var id uuid.UUID
			for i, word := range words {
				id[i*4], id[i*4+1], id[i*4+2], id[i*4+3] = byte(word>>24), byte(word>>16), byte(word>>8), byte(word)
			}
			pigs = append(pigs, id)
		}
	}
	return pigs
}

// TestSummonsIntoSavedChunksKeepTheirMobs summons twelve pigs from the
// console into chunks nobody holds, with no player online, and stops the
// server, three times on one world: each summon wakes a chunk whose file
// holds the earlier pigs while the tick is writing the chunks that left.
func TestSummonsIntoSavedChunksKeepTheirMobs(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	const pigs, rounds = 12, 3
	var script strings.Builder
	var chunks []world.ChunkPos
	for i := range pigs {
		x, z := float64(100*i-600)+0.5, float64(33*(i%3))+0.5
		fmt.Fprintf(&script, "summon minecraft:pig %.1f 64 %.1f\n", x, z)
		chunks = append(chunks, world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)})
	}
	for range rounds {
		srv, _ := servertest.Start(t, cfg)
		console.New(strings.NewReader(script.String()), io.Discard, io.Discard, srv).Serve()
		srv.Shutdown()
	}

	if got := len(savedPigs(t, cfg.LevelName, chunks...)); got != pigs*rounds {
		t.Fatalf("the entity files hold %d pigs, want %d", got, pigs*rounds)
	}
}

// TestTheSavedItemOfAChunkTheSaveWokeComesBackWhenItLoads has the save
// write a pig into a chunk the herd never woke, whose file holds a pig and
// an item: once the chunk is loaded the item stands in the world, and the
// chunk's unload writes all three back once.
func TestTheSavedItemOfAChunkTheSaveWokeComesBackWhenItLoads(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	srv, _ := servertest.Start(t, cfg)
	srv.StopTick()

	srv.SpawnMob("minecraft:pig", 8.5, 64, 8.5)
	srv.SpawnItemEntity(9.5, 64, 9.5, "minecraft:stone", 1)
	srv.TickMobs(1) // nobody holds the chunk: the pig and the item go to its file
	walker := srv.SpawnMob("minecraft:pig", 24.5, 64, 8.5)
	srv.MoveMob(walker, 9.5, 8.5)
	srv.FlushMobs()

	release := srv.HoldChunk(9, 9)
	srv.TickMobs(1)
	if got := srv.ItemCount(); got != 1 {
		t.Fatalf("%d items stand in the loaded chunk, want 1", got)
	}
	if got := srv.MobCount(); got != 2 {
		t.Fatalf("%d pigs stand in the loaded chunk, want 2", got)
	}
	release()
	srv.TickMobs(1)

	var pigs, items int
	for _, compound := range servertest.ReadChunkEntities(t, cfg.LevelName, world.ChunkPos{}) {
		switch kind, _ := nbt.Get[nbt.String](compound, "id"); kind {
		case "minecraft:pig":
			pigs++
		case "minecraft:item":
			items++
		}
	}
	if pigs != 2 || items != 1 {
		t.Fatalf("the entity file holds %d pigs and %d items, want 2 and 1", pigs, items)
	}
}
