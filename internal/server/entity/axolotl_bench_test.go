package entity

import "testing"

// BenchmarkAxolotlBrains steps sixteen axolotls in an enclosed water tank.
func BenchmarkAxolotlBrains(b *testing.B) {
	w := newTestWorld()
	stone, water := defaultState(b, "minecraft:stone"), defaultState(b, "minecraft:water")
	floor(w, -9, -9, 9, 9, 0, stone)
	for y := int32(1); y <= 9; y++ {
		floor(w, -9, -9, 9, 9, y, stone)
		floor(w, -8, -8, 8, 8, y, water)
	}
	mobs := make(Mobs, 0, 16)
	for i := range 16 {
		mobs = append(mobs, NewMob(TypeAxolotl, int32(i+1), float64(i%4)*2-3, 4, float64(i/4)*2-3, int64(i+1)))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.Tick(w, nil, nil)
	}
}
