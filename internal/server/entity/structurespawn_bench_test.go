package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

var structureSpawnBenchList []spawnData

func BenchmarkStructureSpawnList(b *testing.B) {
	w := world.NewDimensionWorld(b.TempDir(), true, world.NewFlatGenerator())
	defer w.Close()
	pos := world.ChunkPos{X: 0, Z: 0}
	w.Acquire(pos)
	defer w.Release(pos)
	spawnListAt(w, 0, 64, 0, CategoryMonster, registry.BiomeSwamp)
	b.ReportAllocs()
	for b.Loop() {
		structureSpawnBenchList = spawnListAt(w, 0, 64, 0, CategoryMonster, registry.BiomeSwamp)
	}
}
