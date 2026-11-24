package entity

import (
	"math/rand"
	"testing"
)

// BenchmarkMobsTickLlamas steps two thousand unleashed llamas, none in a
// caravan, through a tick: the caravan goal every one of them asks.
func BenchmarkMobsTickLlamas(b *testing.B) {
	stone := defaultState(b, "minecraft:stone")
	w := newTestWorld()
	floor(w, -129, -129, 128, 128, 63, stone)
	rng := rand.New(rand.NewSource(527))
	mobs := make(Mobs, 0, 2000)
	for id := int32(1); len(mobs) < cap(mobs); id++ {
		x, z := (rng.Float64()-0.5)*256, (rng.Float64()-0.5)*256
		mobs = append(mobs, NewMob(TypeLlama, id, x, 64, z, int64(id)))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.Tick(w, nil, nil)
	}
}
