package entity

import (
	"github.com/golem-mc/golem/registry"

	"testing"
)

// BenchmarkTickWaterHerd exercises swimming control and, for nautiluses,
// charge targeting among two hundred water mobs.
func BenchmarkTickWaterHerd(b *testing.B) {
	for _, kind := range []MobType{TypeAxolotl, registry.EntityTypeNautilus, registry.EntityTypeZombieNautilus} {
		b.Run(kind.Name(), func(b *testing.B) {
			if !KnownMobKind(kind) {
				b.Skip("mob kind is not implemented")
			}
			w := newTestWorld()
			water := defaultState(b, "minecraft:water")
			for x := int32(-2); x < 34; x++ {
				for y := int32(60); y < 68; y++ {
					for z := int32(-2); z < 34; z++ {
						w.set(x, y, z, water)
					}
				}
			}
			herd := make([]*Mob, 200)
			for i := range herd {
				t := kind
				if i%2 == 0 {
					t = TypeCod
				}
				herd[i] = NewMob(t, int32(i+1), float64(i%16)*2+0.5, 63, float64(i/16)*2+0.5, int64(i+1))
				herd[i].Persistence = true
			}
			var events []MobEvent
			b.ReportAllocs()
			b.ResetTimer()
			for step := range b.N {
				for i, m := range herd {
					if i%2 != 0 && step%100 == 0 {
						m.Health = m.MaxHealth
						m.HurtByMob(1, herd[i-1])
					}
					// Keep the herd in water throughout long benchmark samples.
					m.X, m.Y, m.Z = float64(i%16)*2+0.5, 63, float64(i/16)*2+0.5
					events = m.tickInto(events[:0], &tickView{}, w, nil, herd)
				}
			}
		})
	}
}
