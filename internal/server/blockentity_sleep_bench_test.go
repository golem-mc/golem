package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkIdleFurnaces2000 measures the complete block-entity pass over a
// base's idle furnaces, including dispatch and the unchanged tick-list copy.
func BenchmarkIdleFurnaces2000(b *testing.B) {
	s, w := newBlockBenchServer(b, 1)
	for i := range int32(2000) {
		x, y, z := i%16, world.GroundLevel+1+i/256, i/16%16
		w.SetBlock(x, y, z, registry.BlockFurnace.DefaultState())
		w.EnsureBlockEntity(x, y, z, world.KindFurnace)
	}
	w.FlushLighting()
	for range 5 {
		s.tickBlockEntities()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickBlockEntities()
	}
	b.StopTimer()
}

// BenchmarkIdleCatalysts2000 includes the physics pass that adopts saved
// spreader cursors, which is independent of the server's block-entity pass.
func BenchmarkIdleCatalysts2000(b *testing.B) {
	s, w := newBlockBenchServer(b, 1)
	for i := range int32(2000) {
		x, y, z := i%16, world.GroundLevel+1+i/256, i/16%16
		w.SetBlock(x, y, z, registry.BlockSculkCatalyst.DefaultState())
		w.EnsureBlockEntity(x, y, z, world.KindSculkCatalyst)
	}
	w.FlushLighting()
	p := s.dimensionOfWorld(w).physics
	for age := range int64(5) {
		p.Tick(age)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for age := range b.N {
		p.Tick(int64(age) + 5)
	}
	b.StopTimer()
}
