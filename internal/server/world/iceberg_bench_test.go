package world

import "testing"

// BenchmarkOverworldGenerateFrozenOcean generates a fresh 9x9 region around
// seed 5's frozen ocean, including icebergs and their writes across chunks.
func BenchmarkOverworldGenerateFrozenOcean(b *testing.B) {
	g := NewOverworldGenerator(5)
	g.Generate(newChunk(1000, 1000)) // shared structure data stays out of the timing
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		g = NewOverworldGenerator(5)
		for x := int32(-92); x <= -84; x++ {
			for z := int32(-84); z <= -76; z++ {
				g.Generate(newChunk(x, z))
			}
		}
	}
}
