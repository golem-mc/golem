package world

import "testing"

func BenchmarkOverworldFindSpawn(b *testing.B) {
	for b.Loop() {
		NewOverworldGenerator(0).FindSpawn()
	}
}
