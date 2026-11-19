package entity

import "testing"

// BenchmarkDolphinBrains steps sixteen dolphins beside players in a water tank.
func BenchmarkDolphinBrains(b *testing.B) {
	benchmarkDolphinBrains(b, false)
}

// BenchmarkDolphinBrainsSwimming includes the swim-with-player look requests.
func BenchmarkDolphinBrainsSwimming(b *testing.B) {
	benchmarkDolphinBrains(b, true)
}

func benchmarkDolphinBrains(b *testing.B, swimming bool) {
	w := newTestWorld()
	stone, water := defaultState(b, "minecraft:stone"), defaultState(b, "minecraft:water")
	floor(w, -17, -17, 17, 17, 0, stone)
	for y := int32(1); y <= 9; y++ {
		floor(w, -17, -17, 17, 17, y, stone)
		floor(w, -16, -16, 16, 16, y, water)
	}
	mobs := make(Mobs, 16)
	for i := range mobs {
		mobs[i] = NewMob(TypeDolphin, int32(i+1), float64(i%4)*4-6, 4, float64(i/4)*4-6, int64(i+1))
	}
	players := []Player{{EntityID: 17, X: 0.5, Y: 4, Z: 0.5, Pushable: true, Swimming: swimming}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for i, m := range mobs {
			m.X, m.Y, m.Z = float64(i%4)*4-6, 4, float64(i/4)*4-6
			m.AirSupply = 4800
		}
		mobs.Tick(w, players, nil)
	}
}
