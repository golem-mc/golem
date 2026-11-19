package entity

import "testing"

// BenchmarkPiglinCelebration rolls a herd's dance decisions for a hoglin kill.
func BenchmarkPiglinCelebration(b *testing.B) {
	var piglins [32]*Mob
	for i := range piglins {
		piglins[i] = NewMob(TypePiglin, int32(i+1), 0, 64, 0, int64(i+1))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for tick := range b.N {
		for _, m := range piglins {
			m.GameTime = int64(tick)
			m.endCelebration()
			m.celebrateAt(0, 64, 0, true)
		}
	}
}
