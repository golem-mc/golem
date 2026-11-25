package entity

import "testing"

// BenchmarkMobsTickBoatPassengers steps 64 NoAI pigs, each seated among
// three hulls. The default cramming limit leaves them alive for the run.
func BenchmarkMobsTickBoatPassengers(b *testing.B) {
	w := newTestWorld()
	floor(w, -2, -2, 34, 34, 63, defaultState(b, "minecraft:stone"))
	var mobs Mobs
	var crowd PushCrowd
	for i := range 64 {
		x, z := float64(i%8)*4+0.5, float64(i/8)*4+0.5
		m := NewMob(TypePig, int32(i+1), x, 64, z, int64(i+1))
		m.RidesVehicle, m.NoAI = true, true
		mobs = append(mobs, m)
		for range 3 {
			crowd.Add(PushBody{Box: NewAABB(x-BoatHalfWidth, 64, z-BoatHalfWidth, x+BoatHalfWidth, 64+BoatHeight, z+BoatHalfWidth)})
		}
	}
	var events []MobEvent
	var paths MobPathScratch
	b.ReportAllocs()
	for b.Loop() {
		events = mobs.TickInto(events, w, nil, nil, &crowd, nil, nil, &paths)
	}
}
