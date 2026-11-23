package entity

import "testing"

// BenchmarkPufferfishNoAIStings ticks full NoAI pufferfish in a water tank
// each with a pig in reach and a herd of pigs around them. The player contact
// is the server's (applyPufferTouches).
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
		x, z := float64(i%4)*4-6, float64(i/4)*4-6
		m := NewMob(TypePufferfish, int32(i+1), x, 4, z, int64(i+1))
		m.NoAI = true
		m.SetPuffState(PuffFull)
		mobs = append(mobs, m)
		// A pig NoAI in each fish's reach, so every tick queues a sting.
		pig := NewMob(TypePig, int32(i+100), x, 4, z, int64(i+100))
		pig.NoAI = true
		mobs = append(mobs, pig)
	}
	for i := range 48 {
		mobs = append(mobs, NewMob(TypePig, int32(i+200), float64(i%8)*4-14, 1, float64(i/8)*4-14, int64(i+200)))
	}
	stings := func() (n int) {
		for _, ev := range mobs.Tick(w, nil, nil) {
			if ev.Kind == MobAttackedPlayer && ev.Mob.Type == TypePufferfish {
				n++
			}
		}
		return n
	}
	if n := stings(); n < 16 {
		b.Fatalf("%d stings in a tick, want one per fish", n)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.Tick(w, nil, nil)
	}
}
