package terrain

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

func BenchmarkBeardJunctionVolume(b *testing.B) {
	junctions := make([]levelgen.BeardJunction, 16)
	for i := range junctions {
		junctions[i] = levelgen.BeardJunction{X: (i % 4) * 4, GroundY: 64 + i%3, Z: (i / 4) * 4}
	}
	c := NewContext(NewBeardifier(nil, junctions))
	v := NewVolume(16, 32, 16, 0, 48, 0)
	out := make([]float32, 16*32*16)
	s := BeardifierSampler()
	b.ReportAllocs()
	for b.Loop() {
		s.SampleVolume(c, out, v)
	}
	if out[v.Index(8, 16, 8)] == 0 {
		b.Fatal("center sample is zero")
	}
}
