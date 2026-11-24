package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func BenchmarkSoilToDirt(b *testing.B) {
	for _, tc := range []struct {
		name  string
		state uint16
		cover bool
	}{
		{"covered_farmland", registry.BlockFarmland.DefaultState(), true},
		{"covered_path", registry.BlockDirtPath.DefaultState(), true},
		{"dry_farmland", registry.BlockFarmland.DefaultState(), false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			w := newTestWorld(b, NewFlatGenerator())
			b.Cleanup(func() { _ = w.Close() })
			w.Acquire(ChunkPos{})
			p := NewPhysics(w)
			const x, y, z = 8, GroundLevel - 1, 8
			b.ReportAllocs()
			if tc.cover {
				w.SetBlock(x, y+1, z, registry.BlockStone.DefaultState())
				var now int64
				for b.Loop() {
					now++
					w.SetBlock(x, y, z, tc.state)
					p.schedule(x, y, z, now)
					changes, _, _, effects := p.Tick(now)
					p.RecycleTickResults(changes, effects)
				}
				return
			}
			var changes []BlockChange
			var decays []Decay
			for b.Loop() {
				w.SetBlock(x, y, z, tc.state)
				changes = changes[:0]
				p.randomVisit(x, y, z, tc.state, &changes, &decays)
				p.TakeRandomEffects()
			}
		})
	}
}
