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

// BenchmarkPushCandidatesOversized queries the 2,000 zombies' pen with a
// scale-16 slime far off in a sparse cell, which widens every query square.
func BenchmarkPushCandidatesOversized(b *testing.B) {
	const herd = 2000
	rng := rand.New(rand.NewSource(2429))
	mobs := make([]*Mob, herd+1)
	grid := takePushGrid(len(mobs), nil)
	defer grid.release()
	for i := range herd {
		mobs[i] = NewMob(TypeZombie, int32(i), (rng.Float64()-0.5)*64, 64, (rng.Float64()-0.5)*64, int64(i))
	}
	slime := NewMob(TypeSlime, herd, 100000.5, 64, 0.5, herd)
	slime.Size = 127
	slime.SetAttributes(slime.BuildAttributes(nbt.List{}))
	slime.attrs.SetBase(registry.AttributeScale, 16)
	mobs[herd] = slime
	grid.build(mobs)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, m := range mobs[:herd] {
			grid.candidates(m, 0)
		}
	}
}

// BenchmarkPushGridBuild times building and resetting the push grid for 2,000
// zombies in a 64×64 pen.
func BenchmarkPushGridBuild(b *testing.B) {
	const herd = 2000
	rng := rand.New(rand.NewSource(2429))
	mobs := make([]*Mob, herd)
	grid := takePushGrid(herd, nil)
	defer grid.release()
	for i := range mobs {
		mobs[i] = NewMob(TypeZombie, int32(i), (rng.Float64()-0.5)*64, 64, (rng.Float64()-0.5)*64, int64(i))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		grid.build(mobs)
		grid.reset()
	}
}
