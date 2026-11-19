package world

import (
	"github.com/golem-mc/golem/registry"
	"testing"
)

var fireBenchState uint16

func BenchmarkFireStateFor(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	defer w.Close()
	pos := ChunkPos{X: 0, Z: 0}
	w.Acquire(pos)
	defer w.Release(pos)
	p := NewPhysics(w)
	for _, tc := range []struct {
		name  string
		below uint16
	}{
		{"floor", registry.MustState("minecraft:stone", nil)},
		{"sides", 0},
	} {
		w.SetBlock(8, GroundLevel-1, 8, tc.below)
		w.SetBlock(7, GroundLevel, 8, registry.MustState("minecraft:oak_planks", nil))
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				fireBenchState = p.fireStateFor(8, GroundLevel, 8, 3)
			}
		})
	}
}
