package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkFireworkFlight(b *testing.B) {
	w := world.NewDimensionWorld(b.TempDir(), true, world.NewFlatGenerator())
	defer w.Close()
	pos := world.ChunkPos{}
	w.Acquire(pos)
	defer w.Release(pos)
	w.SetBlock(8, 110, 8, registry.BlockDispenser.DefaultState())

	for _, tc := range []struct {
		name string
		x    float64
	}{
		{"open", 5.5},
		{"dispenser", 8.5},
	} {
		b.Run(tc.name, func(b *testing.B) {
			f := NewFirework(1, tc.x, 110.5, 7.99999, 0, 1, 0, 0, 3)
			f.ShotAtAngle = true
			f.VX, f.VY, f.VZ = 0.003, -0.001, -0.5
			b.ReportAllocs()
			for b.Loop() {
				f.Life = 0
				f.SetPos(tc.x, 110.5, 7.99999)
				f.Step(w, nil, nil, nil)
			}
		})
	}
}
