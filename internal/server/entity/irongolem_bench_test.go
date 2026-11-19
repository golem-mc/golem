package entity

import "testing"

// BenchmarkMobsTickVillageDefense includes reputation scans and golem melee.
func BenchmarkMobsTickVillageDefense(b *testing.B) {
	for _, withPlayer := range []bool{false, true} {
		name := "NoPlayers"
		if withPlayer {
			name = "BadReputationPlayer"
		}
		b.Run(name, func(b *testing.B) {
			w := newTestWorld()
			floor(w, -24, -24, 24, 24, 63, defaultState(b, "minecraft:stone"))
			mobs := Mobs{}
			for i := range 24 {
				m := NewMob(TypeVillager, int32(i+1), float64(i%6)-3, 64, float64(i/6)+4, int64(i))
				m.NoAI = true
				m.OnReputationEvent(ReputationVillagerKilled, gossipA)
				mobs = append(mobs, m)
			}
			for i := range 4 {
				mobs = append(mobs, NewMob(TypeIronGolem, int32(25+i), float64(i*3)-4, 64, 0.5, int64(i)))
			}
			var players []Player
			if withPlayer {
				players = []Player{{EntityID: 1000, UUID: gossipA, X: 0.5, Y: 64, Z: 0.5, Huntable: true}}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				mobs.Tick(w, players, nil)
			}
		})
	}
}
