package entity

import "testing"

// BenchmarkPufferfishNoAIStings ticks full NoAI pufferfish in a water tank
// with a player and a herd of pigs around them.
func BenchmarkPufferfishNoAIStings(b *testing.B) {
	w := newTestWorld()
	stone, water := defaultState(b, "minecraft:stone"), defaultState(b, "minecraft:water")
	floor(w, -17, -17, 17, 17, 0, stone)
	for y := int32(1); y <= 9; y++ {
		floor(w, -17, -17, 17, 17, y, stone)
		floor(w, -16, -16, 16, 16, y, water)
	}
	mobs := make(Mobs, 0, 80)
	for i := range 16 {
		m := NewMob(TypePufferfish, int32(i+1), float64(i%4)*4-6, 4, float64(i/4)*4-6, int64(i+1))
		m.NoAI = true
		m.SetPuffState(PuffFull)
		mobs = append(mobs, m)
	}
	for i := range 64 {
		mobs = append(mobs, NewMob(TypePig, int32(i+100), float64(i%8)*4-14, 1, float64(i/8)*4-14, int64(i+100)))
	}
	players := []Player{{EntityID: 200, X: 0.5, Y: 4, Z: 0.5, Huntable: true, Pushable: true}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for i, m := range mobs {
			if i < 16 {
				m.X, m.Y, m.Z = float64(i%4)*4-6, 4, float64(i/4)*4-6
			}
		}
		mobs.Tick(w, players, nil)
	}
}
