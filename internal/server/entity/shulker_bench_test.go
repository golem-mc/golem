package entity

import "testing"

// BenchmarkMobsTickShulkers includes standing shells, NoAI shells and hurt boat passengers.
func BenchmarkMobsTickShulkers(b *testing.B) {
	w := newTestWorld()
	floor(w, -24, -24, 24, 24, 63, defaultState(b, "minecraft:stone"))
	var mobs Mobs
	for i := range 16 {
		mobs = append(mobs, NewMob(TypeShulker, int32(i+1), float64(i%4)*4+.5, 64, float64(i/4)*4+.5, int64(i+1)))
	}
	// A herd of standing NoAI shells, each checking its support every tick.
	for i := range 64 {
		m := NewMob(TypeShulker, int32(100+i), float64(i%8)*2-15.5, 64, float64(i/8)*2-15.5, int64(100+i))
		m.NoAI = true
		mobs = append(mobs, m)
	}
	var events []MobEvent
	var paths MobPathScratch
	b.ReportAllocs()
	for b.Loop() {
		for i, m := range mobs {
			if i%2 == 0 {
				m.RidesVehicle = true
				m.shulker.teleport = true
			}
		}
		events = mobs.TickInto(events, w, nil, nil, nil, nil, &paths)
	}
}
