package entity

import "testing"

// BenchmarkMobsTickCourtingHerd ticks pairs of cows in love through the
// shared animal tick and breed goal, re-arming their love every 120 ticks.
func BenchmarkMobsTickCourtingHerd(b *testing.B) {
	w := newTestWorld()
	floor(w, -40, -40, 40, 40, 63, defaultState(b, "minecraft:stone"))
	mobs := make(Mobs, 0, 64)
	for i := range int32(64) {
		pair := i / 2
		x, z := float64(pair%8*10-40+i%2)+0.5, float64(pair/8*10-20)+0.5
		mobs = append(mobs, NewMob(TypeCow, i+1, x, 64, z, int64(i)))
	}
	var events []MobEvent
	b.ReportAllocs()
	b.ResetTimer()
	for step := range b.N {
		if step%120 == 0 {
			for _, m := range mobs {
				m.BabyAge, m.InLove = 0, 600
			}
		}
		events = mobs.TickInto(events, w, nil, nil, nil, nil)
	}
}
