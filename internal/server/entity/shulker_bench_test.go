package entity

import "testing"

// BenchmarkMobsTickShulkers includes standing shells and hurt boat passengers.
func BenchmarkMobsTickShulkers(b *testing.B) {
	w := newTestWorld()
	floor(w, -24, -24, 24, 24, 63, defaultState(b, "minecraft:stone"))
	var mobs Mobs
	for i := range 16 {
		mobs = append(mobs, NewMob(TypeShulker, int32(i+1), float64(i%4)*4+.5, 64, float64(i/4)*4+.5, int64(i+1)))
	}
	var events []MobEvent
	b.ReportAllocs()
	for b.Loop() {
		for i, m := range mobs {
			if i%2 == 0 {
				m.RidesVehicle = true
				m.shulker.teleport = true
			}
		}
		events = mobs.TickInto(events, w, nil, nil, nil, nil)
	}
}
