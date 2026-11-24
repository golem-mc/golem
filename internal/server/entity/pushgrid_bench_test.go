package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkPushCandidates2000 queries a tick's 2,000 zombies in a 64×64 pen.
func BenchmarkPushCandidates2000(b *testing.B) {
	const herd = 2000
	rng := rand.New(rand.NewSource(2429))
	mobs := make([]*Mob, herd)
	grid := takePushGrid(herd, nil)
	defer grid.release()
	for i := range mobs {
		mobs[i] = NewMob(TypeZombie, int32(i), (rng.Float64()-0.5)*64, 64, (rng.Float64()-0.5)*64, int64(i))
	}
	grid.build(mobs)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, m := range mobs {
			grid.candidates(m, 0)
		}
	}
	b.ReportMetric(float64(len(grid.cells)), "sparse-cells")
}

// BenchmarkPushCandidatesSparseScaled queries 200 zombies spread over a
// 512×512 area, so most cells hold one mob, with one scale-16 llama among them.
func BenchmarkPushCandidatesSparseScaled(b *testing.B) {
	const herd = 200
	rng := rand.New(rand.NewSource(2429))
	mobs := make([]*Mob, herd)
	grid := takePushGrid(herd, nil)
	defer grid.release()
	for i := range mobs {
		mobs[i] = NewMob(TypeZombie, int32(i), (rng.Float64()-0.5)*512, 64, (rng.Float64()-0.5)*512, int64(i))
	}
	mobs[0] = NewMob(TypeLlama, 0, 0, 64, 0, 0)
	mobs[0].SetAttributes(mobs[0].BuildAttributes(nbt.List{}))
	mobs[0].attrs.SetBase(registry.AttributeScale, 16)
	grid.build(mobs)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, m := range mobs {
			grid.candidates(m, 0)
		}
	}
}
