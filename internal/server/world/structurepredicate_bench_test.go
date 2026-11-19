package world

import "testing"

var structureMembershipBench bool

func BenchmarkStructureSpawnMembership(b *testing.B) {
	w := NewDimensionWorld(b.TempDir(), true, NewFlatGenerator())
	defer w.Close()
	pos := ChunkPos{0, 0}
	w.Acquire(pos)
	defer w.Release(pos)
	ids := []string{"minecraft:swamp_hut"}
	w.StructureHasPieceAt(0, 64, 0, ids)
	b.ReportAllocs()
	for b.Loop() {
		structureMembershipBench = w.StructureHasPieceAt(0, 64, 0, ids)
	}
}
