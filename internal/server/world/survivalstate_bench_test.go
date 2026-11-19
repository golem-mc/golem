package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkMossyCarpetState measures the shape update beside a wall, both on
// a base carpet and a topper constrained by the base's sides.
func BenchmarkMossyCarpetState(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	w.Acquire(ChunkPos{})
	const x, y, z = 5, GroundLevel + 2, 5
	w.SetBlock(x+1, y, z, registry.BlockStone.DefaultState())
	w.SetBlock(x+1, y+1, z, registry.BlockStone.DefaultState())
	base := registry.BlockPaleMossCarpet.DefaultState()
	side := registry.MustState(PaleMossCarpet, map[string]string{"east": "low"})
	w.SetBlock(x, y, z, side)
	topper := registry.MustState(PaleMossCarpet, map[string]string{"bottom": "false", "east": "low"})
	for _, tc := range []struct {
		name  string
		state uint16
		y     int32
		want  uint16
	}{
		{"base", base, y, side},
		{"topper", topper, y + 1, topper},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			var got uint16
			for range b.N {
				got = MossyCarpetState(w, tc.state, x, tc.y, z, false)
			}
			if got != tc.want {
				b.Fatalf("state = %d, want %d", got, tc.want)
			}
		})
	}
}
