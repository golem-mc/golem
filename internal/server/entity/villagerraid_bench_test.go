package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkMobsTickVillagerRaid steps villagers hiding around a shared bed.
func BenchmarkMobsTickVillagerRaid(b *testing.B) {
	w := newTestWorld()
	floor(w, -30, -30, 30, 30, 63, defaultState(b, "minecraft:stone"))
	bed := registry.MustState("minecraft:red_bed", map[string]string{"part": "head"})
	w.set(0, 64, 0, bed)
	village := fakeVillage{w: w, pois: []Waypoint{{0, 64, 0}}}
	herd := make(Mobs, 24)
	for i := range herd {
		m := NewMob(TypeVillager, int32(i+1), float64(i%6)*3-8.5, 64, float64(i/6)*3-5.5, int64(i))
		m.Village = village
		m.SetRaidView(RaidView{Has: true, Active: true})
		herd[i] = m
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
