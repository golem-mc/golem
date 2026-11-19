package entity

import "testing"

// BenchmarkCropAndEyeblossomContact ticks eight ravagers and eight bees held
// in empty cells, the scan cost every such mob pays each tick. The ravagers
// stand on wheat's farmland row rather than in the crop: the test world never
// applies a break event, so trampled wheat would be re-reported every tick.
func BenchmarkCropAndEyeblossomContact(b *testing.B) {
	w := griefingWorld{newTestWorld()}
	stone := defaultState(b, "minecraft:stone")
	floor(w.testWorld, -20, -20, 20, 20, 63, stone)
	mobs := make(Mobs, 0, 16)
	for i := range 8 {
		x := int32(i*4 - 16)
		r := NewMob(TypeRavager, int32(i+1), float64(x)+0.5, 64, 0.5, int64(i+1))
		r.NoAI = true
		bee := NewMob(TypeBee, int32(i+101), float64(x)+0.5, 64, 8.5, int64(i+101))
		bee.NoAI = true
		mobs = append(mobs, r, bee)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.Tick(w, nil, nil)
	}
}
