package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

var spawnBenchEmpty bool

func BenchmarkEmptySpawnBlock(b *testing.B) {
	states := [...]uint16{registry.BlockAir.DefaultState(), registry.BlockStone.DefaultState(), registry.BlockFire.DefaultState(), registry.BlockWater.DefaultState(), registry.BlockRail.DefaultState(), registry.BlockMagmaBlock.DefaultState(), registry.BlockShortGrass.DefaultState(), registry.BlockLava.DefaultState()}
	i := 0
	b.ReportAllocs()
	for b.Loop() {
		spawnBenchEmpty = emptySpawnBlock(TypeZombie, states[i&7])
		i++
	}
}

var spawnWalkResult int32

func BenchmarkSpawnWalkDie(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	var result int32
	b.ReportAllocs()
	for b.Loop() {
		result += spawnWalkDie(rng)
	}
	spawnWalkResult = result
}
