package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkNoteBlockPower(b *testing.B) {
	for _, tc := range []struct {
		name  string
		cover registry.Block
	}{
		{"sounding", registry.BlockAir},
		{"silent_head", registry.BlockPlayerHead},
	} {
		b.Run(tc.name, func(b *testing.B) {
			s, w := newCommandBenchServer(b, 0)
			p := s.physicsFor(w)
			const y = int32(world.GroundLevel + 1)
			air := registry.BlockAir.DefaultState()
			power := registry.BlockRedstoneBlock.DefaultState()
			w.SetBlock(8, y, 8, registry.BlockNoteBlock.DefaultState())
			w.SetBlock(8, y+1, 8, tc.cover.DefaultState())
			p.NotifyChanged(8, y+1, 8, air)

			b.ReportAllocs()
			for b.Loop() {
				w.SetBlock(7, y, 8, power)
				_, effects, _ := p.NotifyChanged(7, y, 8, air)
				for _, e := range effects {
					s.tellEffect(w, e)
				}
				w.SetBlock(7, y, 8, air)
				p.NotifyChanged(7, y, 8, power)
			}
		})
	}
}
