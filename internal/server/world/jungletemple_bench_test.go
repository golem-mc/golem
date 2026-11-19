package world

import "testing"

// The chunks contain vanilla jungle temple starts on seeds 0 and 12345.
func BenchmarkOverworldGenerateJungleTemple(b *testing.B) {
	for _, tc := range []struct {
		name string
		seed int64
		x, z int32
	}{
		{"seed0", 0, 8, 69}, {"seed12345", 12345, -95, 2},
	} {
		b.Run(tc.name, func(b *testing.B) {
			NewOverworldGenerator(tc.seed).Generate(newChunk(tc.x, tc.z))
			b.ReportAllocs()
			for b.Loop() {
				NewOverworldGenerator(tc.seed).Generate(newChunk(tc.x, tc.z))
			}
		})
	}
}
