package world

import "testing"

// BenchmarkRainingAt measures the clear-weather check each body makes per tick.
func BenchmarkRainingAt(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	w.SetWeather(false, false, 100000, 100000, 0)
	w.TickWeather()
	b.ReportAllocs()
	for b.Loop() {
		if w.RainingAt(0, GroundLevel, 0) {
			b.Fatal("expected clear weather")
		}
	}
}
