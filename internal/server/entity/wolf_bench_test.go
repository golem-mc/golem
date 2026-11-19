package entity

import "testing"

// BenchmarkMobsTickWolves steps wild and tame packs with and without llamas.
func BenchmarkMobsTickWolves(b *testing.B) {
	for _, scene := range []string{"wild", "tame", "llamas"} {
		b.Run(scene, func(b *testing.B) {
			w := newTestWorld()
			floor(w, -80, -80, 80, 80, 63, defaultState(b, "minecraft:stone"))
			mobs := make(Mobs, 0, 72)
			for id := int32(1); id <= 64; id++ {
				m := NewMob(TypeWolf, id, float64(id%8)*8-28, 64, float64(id/8)*8-28, int64(id))
				m.Tamable.Tame = scene == "tame"
				mobs = append(mobs, m)
			}
			if scene == "llamas" {
				for id := int32(65); id <= 72; id++ {
					m := NewMob(TypeLlama, id, float64(id-65)*8-28, 64, 0, int64(id))
					m.Horse.Strength, m.NoAI = 5, true
					mobs = append(mobs, m)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				mobs.Tick(w, nil, nil)
			}
		})
	}
}
