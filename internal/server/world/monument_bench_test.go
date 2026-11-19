package world

import "testing"

func BenchmarkMonumentChunkGenerate(b *testing.B) {
	// Exclude process-wide lazy registry and feature-table initialization.
	NewOverworldGenerator(-7343414273836990780).Generate(newChunk(111, 110))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		NewOverworldGenerator(-7343414273836990780).Generate(newChunk(111, 110))
	}
}
