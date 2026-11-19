package entity

import "testing"

// BenchmarkMobsTickPolarBears includes idle scans and hunts of stationary foxes.
func BenchmarkMobsTickPolarBears(b *testing.B) {
	for _, scene := range []string{"idle", "foxes", "unreachable"} {
		b.Run(scene, func(b *testing.B) {
			w := newTestWorld()
			stone := defaultState(b, "minecraft:stone")
			floor(w, -8, -8, 128, 128, 63, stone)
			mobs := make(Mobs, 0, 128)
			for id := int32(1); id <= 64; id++ {
				x, z := float64((id-1)%8)*16, float64((id-1)/8)*16
				mobs = append(mobs, NewMob(TypePolarBear, id, x+0.5, 64, z+0.5, int64(id)))
				if scene != "idle" {
					fox := NewMob(TypeFox, id+64, x+6.5, 64, z+0.5, int64(id+64))
					fox.NoAI = true
					// Keep the victims alive without excluding them from targeting.
					fox.Health = 1e20
					mobs = append(mobs, fox)
					if scene == "unreachable" {
						for dx := int32(3); dx <= 9; dx++ {
							for dz := int32(-3); dz <= 3; dz++ {
								if dx != 6 || dz != 0 {
									w.set(int32(x)+dx, 63, int32(z)+dz, 0)
								}
							}
						}
					}
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
