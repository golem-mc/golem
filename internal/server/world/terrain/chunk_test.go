package terrain_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/terrain"
)

func TestProtoChunkTopFilledY(t *testing.T) {
	const minY = -64
	type write struct {
		x, y, z  int32
		material terrain.Material
	}
	tests := []struct {
		name   string
		height int32
		writes []write
		reset  bool
		want   int32
	}{
		{name: "empty", height: 64, want: -65},
		{name: "negative world Y", height: 64, writes: []write{{0, -56, 0, terrain.Stone}}, want: -49},
		{name: "below section boundary", height: 64, writes: []write{{0, -33, 0, terrain.Stone}}, want: -33},
		{name: "at section boundary", height: 64, writes: []write{{0, -32, 0, terrain.Stone}}, want: -17},
		{
			name: "water above stone", height: 64, want: -17,
			writes: []write{{0, -33, 0, terrain.Stone}, {15, -32, 15, terrain.Water}},
		},
		{
			name: "remove highest block", height: 64, want: -33,
			writes: []write{{15, -34, 15, terrain.Stone}, {15, -16, 15, terrain.Stone}, {15, -16, 15, terrain.Air}},
		},
		{
			name: "reset", height: 64, reset: true, want: -65,
			writes: []write{{0, -56, 0, terrain.Stone}, {15, -16, 15, terrain.Stone}},
		},
		{
			name: "partial section above complete section", height: 35, want: -33,
			writes: []write{{15, -34, 15, terrain.Stone}, {15, -30, 15, terrain.Stone}},
		},
		{
			name: "only partial section occupied", height: 35, want: -65,
			writes: []write{{15, -30, 15, terrain.Stone}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunk := terrain.NewProtoChunk(minY, tt.height, terrain.Stone)
			for _, w := range tt.writes {
				chunk.Set(w.x, w.y, w.z, w.material)
			}
			if tt.reset {
				chunk.Reset()
			}
			if got := chunk.TopFilledY(); got != tt.want {
				t.Fatalf("TopFilledY() = %d, want %d", got, tt.want)
			}
		})
	}
}

func BenchmarkProtoChunkTopFilledY(b *testing.B) {
	chunk := terrain.NewProtoChunk(-64, 384, terrain.Stone)
	chunk.Set(15, 70, 15, terrain.Stone)
	b.ReportAllocs()
	for b.Loop() {
		if got := chunk.TopFilledY(); got != 79 {
			b.Fatalf("TopFilledY() = %d, want 79", got)
		}
	}
}
