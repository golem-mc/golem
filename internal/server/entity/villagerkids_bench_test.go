package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkMobsTickChildren steps children playing around occupied beds.
func BenchmarkMobsTickChildren(b *testing.B) {
	w := newTestWorld()
	floor(w, -30, -30, 30, 30, 63, defaultState(b, "minecraft:stone"))
	bed, ok := registry.FindState("minecraft:red_bed", map[string]string{"part": "head", "facing": "north", "occupied": "false"})
	if !ok {
		b.Fatal("missing bed head")
	}
	var pois []Waypoint
	for x := int32(-12); x <= 12; x += 2 {
		w.set(x, 64, 10, bed)
		pois = append(pois, Waypoint{x, 64, 10})
	}
	village := fakeVillage{w: w, pois: pois}
	herd := make(Mobs, 0, 24)
	for id := int32(1); id <= 24; id++ {
		m := NewMob(TypeVillager, id, float64(id%6)*3-8.5, 64, float64(id/6)*3-5.5, int64(id))
		m.BabyAge, m.AgeLocked = -24000, true
		m.Village = village
		herd = append(herd, m)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := range b.N {
		for _, m := range herd {
			m.GameTime, m.DayTime = int64(n), 3000
		}
		herd.Tick(w, nil, nil)
	}
}
