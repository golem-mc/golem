package world

import "testing"

// BenchmarkEndTerrain builds the island terrain of distinct chunks, which is
// what a chunk's generation reads before decoration.
func BenchmarkEndTerrain(b *testing.B) {
	g := NewEndGenerator(5)
	b.ReportAllocs()
	i := int32(0)
	for b.Loop() {
		g.terrainOf(i%256-128, i/256)
		i++
	}
}
