package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkAllaysHearNote offers a note to sixteen allays behind a wall.
func BenchmarkAllaysHearNote(b *testing.B) {
	for _, tc := range []struct {
		name  string
		block registry.Block
	}{{"glass", registry.BlockGlass}, {"wool", registry.BlockWhiteWool}} {
		b.Run(tc.name, func(b *testing.B) {
			s, w := newCommandBenchServer(b, 0)
			for y := int32(64); y < 70; y++ {
				for z := range int32(8) {
					w.SetBlock(2, y, z, tc.block.DefaultState())
				}
			}
			for i := range 16 {
				s.spawnMobInDim(s.allDims()[0], entity.TypeAllay, 4.5+float64(i%4), 65, 0.5+float64(i/4), int64(i), nil)
			}
			b.ReportAllocs()
			for b.Loop() {
				s.allaysHearNote(w, 0, 64, 0)
			}
		})
	}
}
