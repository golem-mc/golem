package entity

import "testing"

// BenchmarkWitchesTickInRaid ticks eight witches among sixteen pillagers and
// a player on a stone floor, in a raid (the heal goal at work) and outside.
func BenchmarkWitchesTickInRaid(b *testing.B) {
	for _, raiding := range []bool{true, false} {
		name := "outside"
		if raiding {
			name = "raiding"
		}
		b.Run(name, func(b *testing.B) {
			w := newTestWorld()
			witchFloor(w, b)
			var mobs []*Mob
			for i := range 24 {
				kind := TypePillager
				if i%3 == 0 {
					kind = TypeWitch
				}
				m := NewMob(kind, int32(i+1), float64(i%6*4)-10, 64, float64(i/6*4)-6, int64(i))
				m.InRaid, m.Persistence = raiding, true
				mobs = append(mobs, m)
			}
			players := []Player{{X: 0.5, Y: 64, Z: 12.5, EntityID: 100, Huntable: true, Health: 20}}
			for b.Loop() {
				for _, m := range mobs {
					if m.Type == TypeWitch {
						m.tick(w, players, mobs)
					}
				}
			}
		})
	}
}
