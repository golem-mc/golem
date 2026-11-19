package entity

import "testing"

// BenchmarkMobsTickBreezes steps sixteen fighting breezes across open flat
// ground, exercising their shoot, long-jump and slide decisions.
func BenchmarkMobsTickBreezes(b *testing.B) {
	w := newTestWorld()
	floor(w, -70, -70, 70, 70, 63, defaultState(b, "minecraft:stone"))
	mobs := make(Mobs, 0, 16)
	for i := range 16 {
		mobs = append(mobs, NewMob(TypeBreeze, int32(i+1),
			float64(i%4)*16-24, 64, float64(i/4)*16-24, int64(i+1)))
	}
	players := []Player{survivor(1, 12.5)}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.Tick(w, players, nil)
	}
}
