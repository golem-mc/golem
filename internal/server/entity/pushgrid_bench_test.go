package entity

import (
	"math/rand"
	"testing"
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
