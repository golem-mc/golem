package world

import "testing"

// Vanilla start fixtures pin these chunks; each times the new placement path.
func BenchmarkOverworldGenerateSmallPorts(b *testing.B) {
	for _, tc := range []struct {
		name string
		seed int64
		x, z int32
	}{
		{"desert_pyramid", 0, 0, -188},
		{"igloo", 12345, 20, 72},
		{"swamp_hut", 0, -336, -171},
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
