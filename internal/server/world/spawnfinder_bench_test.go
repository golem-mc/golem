package world

import "testing"

func BenchmarkOverworldFindSpawn(b *testing.B) {
	for b.Loop() {
		NewOverworldGenerator(0).FindSpawn()
	}
}

// BenchmarkPluginOverworldReopen opens a saved, unoccupied normal plugin world
// of seed 7070, the constructor path a reload takes.
func BenchmarkPluginOverworldReopen(b *testing.B) {
	dir := b.TempDir()
	shape := dimensionShapes[DimensionName]
	open := func() *World {
		return NewCustomDimensionWorld(dir, "test:normal", shape, NewOverworldGenerator(7070))
	}
	w := open()
	w.SetOwnSeedAndSpawn(7070, SpawnPos{X: 3, Y: 70, Z: 3})
	w.SavePluginMeta()
	if err := w.Close(); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if err := open().Close(); err != nil {
			b.Fatal(err)
		}
	}
}
