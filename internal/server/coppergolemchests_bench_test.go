package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkCopperGolemChestSearch is one golem's chest listing beside a chest
// with 8100 hoppers loaded in chunks outside its search box.
func BenchmarkCopperGolemChestSearch(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	hopper := registry.MustState("minecraft:hopper", map[string]string{"facing": "down", "enabled": "true"})
	for cx := int32(8); cx < 14; cx++ {
		for cz := range int32(6) {
			pos := world.ChunkPos{X: cx, Z: cz}
			w.Acquire(pos)
			b.Cleanup(func() { w.Release(pos) })
		}
	}
	for x := int32(128); x < 218; x++ {
		for z := range int32(90) {
			w.SetBlock(x, 64, z, hopper)
			w.EnsureBlockEntity(x, 64, z, world.KindHopper)
		}
	}
	w.SetBlock(6, 64, 4, registry.BlockChest.DefaultState())
	w.EnsureBlockEntity(6, 64, 4, world.KindChest)
	c := &golemChests{s: s, w: w, dim: s.allDims()[0]}
	var cells [][3]int32
	b.ReportAllocs()
	for b.Loop() {
		cells = c.ChestsIn(-28, 56, -28, 36, 72, 36, cells[:0])
	}
	if len(cells) != 1 {
		b.Fatalf("found %d chests, want 1", len(cells))
	}
}
