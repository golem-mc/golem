package server

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkPluginChunkEntities times Chunk.Entities with 2000 mobs over 1024
// loaded chunks.
func BenchmarkPluginChunkEntities(b *testing.B) {
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
	rng := rand.New(rand.NewSource(264))
	for range 2000 {
		x, z := (rng.Float64()-0.5)*512, (rng.Float64()-0.5)*512
		id := s.spawnMobInDim(dim, entity.TypeCow, x, float64(world.GroundLevel), z, int64(rng.Uint32()), nil)
		s.mobsMu.Lock()
		if ent := s.mobByEntityIDLocked(w, id); ent != nil {
			ent.mob.NoAI = true
		}
		s.mobsMu.Unlock()
	}
	host := &pluginHost{srv: s}
	pw := pluginWorld{srv: s, dim: dim, host: host}
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		c := pluginChunk{world: pw, chunk: w.LoadedChunk(chunks[i%1024])}
		_ = c.Entities()
		i++
	}
}
