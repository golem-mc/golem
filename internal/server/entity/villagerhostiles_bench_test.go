package entity

import (
	"math/rand"
	"testing"
)

// BenchmarkMobsTickZombiesAndVillagers steps two thousand persistent mobs
// over 200x200 blocks, zombies and villagers alternating, after a warm-up
// that leaves about a thousand of them holding a mob target, which every
// goal resolves by entity ID each tick.
func BenchmarkMobsTickZombiesAndVillagers(b *testing.B) {
	stone := defaultState(b, "minecraft:stone")
	w := newTestWorld()
	floor(w, -129, -129, 128, 128, 63, stone)
	rng := rand.New(rand.NewSource(7))
	mobs := make(Mobs, 0, 2000)
	for id := int32(1); id <= 2000; id++ {
		kind := TypeVillager
		if id%2 == 1 {
			kind = TypeZombie
		}
		m := NewMob(kind, id, (rng.Float64()-0.5)*200, 64, (rng.Float64()-0.5)*200, int64(id))
		m.Persistence = true
		mobs = append(mobs, m)
	}
	for range 100 {
		mobs.Tick(w, nil, nil)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.Tick(w, nil, nil)
	}
}
