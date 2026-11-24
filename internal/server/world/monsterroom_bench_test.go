package world

import "testing"

// Seed 0's chunk (3,-1) holds a dungeon, so generation reaches its spawner.
func BenchmarkOverworldGenerateDungeon(b *testing.B) {
	NewOverworldGenerator(0).Generate(newChunk(3, -1))
	b.ReportAllocs()
	for b.Loop() {
		NewOverworldGenerator(0).Generate(newChunk(3, -1))
	}
}
