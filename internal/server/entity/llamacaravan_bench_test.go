package entity

import "testing"

// BenchmarkMobsTickLlamaPen steps 2048 unleashed llamas packed into a 32 by
// 64 pen, where every llama's caravan search sees hundreds of others and none
// can join.
func BenchmarkMobsTickLlamaPen(b *testing.B) {
	w := newTestWorld()
	floor(w, -20, -36, 20, 36, 63, defaultState(b, "minecraft:stone"))
	mobs := make(Mobs, 2048)
	for i := range mobs {
		m := NewMob(TypeLlama, int32(i+1), float64(i%32)-15.5, 64, float64(i/32)-31.5, int64(i))
		m.GameTime, m.OnGround, m.Persistence = 1000, true, true
		mobs[i] = m
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.Tick(w, nil, nil)
	}
}
