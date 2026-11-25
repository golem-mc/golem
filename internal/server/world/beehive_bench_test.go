package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func BenchmarkBeehiveCells(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	for cx := int32(-2); cx <= 4; cx++ {
		for cz := int32(-2); cz <= 4; cz++ {
			pos := ChunkPos{X: cx, Z: cz}
			w.Acquire(pos)
			defer w.Release(pos)
		}
	}
	hive := [3]int32{30, GroundLevel + 2, 30}
	w.SetBlock(hive[0], hive[1], hive[2], registry.BlockBeehive.DefaultState())
	if got := w.BeehiveCells(25, GroundLevel+1, 25, 20); len(got) != 1 || got[0] != hive {
		b.Fatalf("BeehiveCells = %v, want [%v]", got, hive)
	}
	b.ReportAllocs()
	for b.Loop() {
		w.BeehiveCells(25, GroundLevel+1, 25, 20)
	}
}
