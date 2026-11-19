package server

import (
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkTickIdleBlockEntities measures a tick's block-entity and physics
// passes over 16384 chests, block entities that have no ticker.
func BenchmarkTickIdleBlockEntities(b *testing.B) {
	s := newBlockEntityBenchServer(b)
	w := s.world
	chest := registry.MustState("minecraft:chest", map[string]string{"type": "single"})
	for x := range int32(128) {
		for z := range int32(128) {
			w.SetBlock(x, 64, z, chest)
			w.EnsureBlockEntity(x, 64, z, world.KindChest)
		}
	}
	w.FlushLighting()
	physics := s.dimensionOfWorld(w).physics
	b.ReportAllocs()
	b.ResetTimer()
	for age := range b.N {
		s.tickBlockEntities()
		physics.Tick(int64(age))
	}
}

// BenchmarkTickBlockEntitiesAmongItems measures a block entity tick with 2000
// item entities at rest and no block entity anywhere.
func BenchmarkTickBlockEntitiesAmongItems(b *testing.B) {
	s := newBlockEntityBenchServer(b)
	for i := range 2000 {
		x, z := float64(i%128)+0.5, float64(i/128*8)+0.5
		s.spawnItem(s.world, protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1},
			x, world.GroundLevel+1.125, z, 0, 0, 0, 1000)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickBlockEntities()
	}
}

// newBlockEntityBenchServer is a flat world server holding the 8x8 chunks
// from 0,0.
func newBlockEntityBenchServer(b *testing.B) *Server {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() { slog.SetDefault(logger) })
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatal(err)
	}
	w := s.world
	var holds []world.ChunkPos
	for x := range int32(8) {
		for z := range int32(8) {
			pos := world.ChunkPos{X: x, Z: z}
			w.Acquire(pos)
			holds = append(holds, pos)
		}
	}
	b.Cleanup(func() {
		for _, pos := range holds {
			w.Release(pos)
		}
		s.Shutdown()
	})
	return s
}
