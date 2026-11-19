package entity

import "testing"

// BenchmarkMobsTickWardens measures the shared tick with sixteen persistent bodies.
func BenchmarkMobsTickWardens(b *testing.B) {
	for _, scene := range []string{"idle", "players"} {
		b.Run(scene, func(b *testing.B) {
			w := newTestWorld()
			floor(w, -80, -80, 80, 80, 63, defaultState(b, "minecraft:stone"))
			mobs := make(Mobs, 0, 16)
			var players []Player
			for id := int32(1); id <= 16; id++ {
				m := NewMob(TypeWarden, id, float64(id%4)*12, 64, float64(id/4)*12, int64(id))
				m.Persistence = true
				m.RestrictTo(24, 64, 24, 40)
				mobs = append(mobs, m)
				if scene == "players" {
					players = append(players, Player{EntityID: 100 + id, X: m.X + 8, Y: 64, Z: m.Z, Huntable: true})
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				mobs.Tick(w, players, nil)
			}
			b.StopTimer()
			if len(mobs) != 16 {
				b.Fatalf("benchmark lost bodies: %d", len(mobs))
			}
		})
	}
}
