package server

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// pluginChunkBench builds a server with 1024 loaded chunks and returns it with
// its dimension, the chunks and the plugin's world handle.
func pluginChunkBench(b *testing.B) (*Server, *dimension, []world.ChunkPos, pluginWorld) {
	b.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatal(err)
	}
	dim := s.allDims()[0]
	w := dim.w
	var chunks []world.ChunkPos
	for cx := int32(-16); cx < 16; cx++ {
		for cz := int32(-16); cz < 16; cz++ {
			chunks = append(chunks, world.ChunkPos{X: cx, Z: cz})
		}
	}
	for _, pos := range chunks {
		w.Acquire(pos)
	}
	b.Cleanup(func() {
		for _, pos := range chunks {
			w.Release(pos)
		}
		s.Shutdown()
	})
	return s, dim, chunks, pluginWorld{srv: s, dim: dim, host: &pluginHost{srv: s}}
}

// benchChunkEntities times Chunk.Entities over the chunks.
func benchChunkEntities(b *testing.B, w *world.World, pw pluginWorld, chunks []world.ChunkPos) {
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		c := pluginChunk{world: pw, chunk: w.LoadedChunk(chunks[i%1024])}
		_ = c.Entities()
		i++
	}
}

// BenchmarkPluginChunkEntities times Chunk.Entities with 2000 mobs over 1024
// loaded chunks.
func BenchmarkPluginChunkEntities(b *testing.B) {
	s, dim, chunks, pw := pluginChunkBench(b)
	rng := rand.New(rand.NewSource(264))
	spawnPluginBenchCows(s, dim, rng)
	benchChunkEntities(b, dim.w, pw, chunks)
}

func spawnPluginBenchCows(s *Server, dim *dimension, rng *rand.Rand) {
	for range 2000 {
		x, z := (rng.Float64()-0.5)*512, (rng.Float64()-0.5)*512
		id := s.spawnMobInDim(dim, entity.TypeCow, x, float64(world.GroundLevel), z, int64(rng.Uint32()), nil)
		s.mobsMu.Lock()
		if ent := s.mobByEntityIDLocked(dim.w, id); ent != nil {
			ent.mob.NoAI = true
		}
		s.mobsMu.Unlock()
	}
}

// BenchmarkPluginEntitiesIn times moving box queries over 2000 cows and 2000 items.
func BenchmarkPluginEntitiesIn(b *testing.B) {
	s, dim, _, pw := pluginChunkBench(b)
	rng := rand.New(rand.NewSource(282))
	spawnPluginBenchCows(s, dim, rng)
	stone := protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
	for range 2000 {
		x, z := (rng.Float64()-0.5)*512, (rng.Float64()-0.5)*512
		s.spawnItem(dim.w, stone, x, float64(world.GroundLevel), z, 0, 0, 0, entity.BreakPickupDelay)
	}

	b.ReportAllocs()
	i := 0
	for b.Loop() {
		x, z := float64(i%400-200), float64((i*7)%400-200)
		_ = pw.EntitiesIn(plugin.Box{
			Min: plugin.Vec3{X: x - 8, Y: float64(pw.MinY()), Z: z - 8},
			Max: plugin.Vec3{X: x + 8, Y: float64(pw.MinY() + pw.Height()), Z: z + 8},
		})
		i++
	}
}

// BenchmarkPluginChunkEntitiesBodies times Chunk.Entities with 1000 items and
// 1000 armor stands over 1024 loaded chunks and no mobs.
func BenchmarkPluginChunkEntitiesBodies(b *testing.B) {
	s, dim, chunks, pw := pluginChunkBench(b)
	w := dim.w
	rng := rand.New(rand.NewSource(267))
	stone, _ := registry.ItemID("minecraft:stone")
	for range 1000 {
		x, z := (rng.Float64()-0.5)*512, (rng.Float64()-0.5)*512
		s.spawnItem(w, protocol.ItemStack{ItemID: stone, Count: 1}, x, float64(world.GroundLevel), z, 0, 0, 0, entity.BreakPickupDelay)
	}
	for range 1000 {
		x, z := (rng.Float64()-0.5)*512, (rng.Float64()-0.5)*512
		if _, err := s.summonArmorStand(w, x, float64(world.GroundLevel), z, nil, plugin.SpawnCommand); err != nil {
			b.Fatal(err)
		}
	}
	benchChunkEntities(b, w, pw, chunks)
}
