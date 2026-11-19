package world

import "testing"

func BenchmarkOverworldGenerateVillage(b *testing.B) {
	NewOverworldGenerator(0).Generate(newChunk(16, 58))
	b.ReportAllocs()
	for b.Loop() {
		NewOverworldGenerator(0).Generate(newChunk(16, 58))
	}
}
