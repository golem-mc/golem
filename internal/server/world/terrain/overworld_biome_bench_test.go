package terrain

import "testing"

func BenchmarkOverworldBiomeFillChunk(b *testing.B) {
	source := NewOverworldBiomeSource(NewRandomState(Overworld, 12345))
	chunks := [...][2]int32{{0, 0}, {-1, -1}, {3, -4}, {-4, 3}, {6, 1}, {60, 62}, {-70, 5},
		{100, -100}, {-380, -378}, {25, -31}, {-12, 47}, {210, 40}, {1500, -900}, {-2400, 2600}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		c := chunks[i%len(chunks)]
		source.FillChunk(c[0], c[1], func(_, _, _ int32, _ OverworldBiome) {})
	}
}
