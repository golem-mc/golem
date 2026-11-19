package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkMobsTickZombieSiege steps a siege-sized crowd of night zombies
// near a village, so every tick polls moveThroughVillage.CanStart.
func BenchmarkMobsTickZombieSiege(b *testing.B) {
	w := newTestWorld()
	floor(w, -40, -40, 40, 40, 63, defaultState(b, "minecraft:stone"))
	bed := registry.MustState("minecraft:red_bed", map[string]string{"part": "head"})
	var pois []Waypoint
	for i := range 8 {
		x, z := int32(i%4)*4-6, int32(i/4)*4-2
		w.set(x, 64, z, bed)
		pois = append(pois, Waypoint{x, 64, z})
	}
	village := fakeVillage{w: w, pois: pois}
	horde := make(Mobs, 20)
	for i := range horde {
		m := NewMob(TypeZombie, int32(i+1), float64(i%5)*3-20.5, 64, float64(i/5)*3-20.5, int64(i))
		m.Village = village
		horde[i] = m
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := range b.N {
		for _, m := range horde {
			m.GameTime, m.DayTime = int64(n), 18000
		}
		horde.Tick(w, nil, nil)
	}
}
