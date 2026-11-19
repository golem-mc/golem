package world

import "testing"

// BenchmarkEndCityGenerate includes terrain, decoration and saved structures
// at seed 0's nearest city, with the generator's start and terrain caches warm.
func BenchmarkEndCityGenerate(b *testing.B) {
	g := NewEndGenerator(0)
	b.ReportAllocs()
	for b.Loop() {
		g.Generate(newChunk(-77, -79))
	}
}
