package entity

import "testing"

// BenchmarkArmadilloHerd ticks armadillos and spiders beside a stationary undead threat.
func BenchmarkArmadilloHerd(b *testing.B) {
	w := newTestWorld()
	floor(w, -32, -32, 32, 32, 63, defaultState(b, "minecraft:stone"))
	herd := make(Mobs, 0, 33)
	for i := range 32 {
		typ := TypeArmadillo
		if i%2 == 0 {
			typ = TypeSpider
		}
		m := NewMob(typ, int32(i+1), float64(i%8)*3-10, 64, float64(i/8)*3-5, int64(i+1))
		m.OnGround = true
		herd = append(herd, m)
	}
	z := NewMob(TypeZombie, 33, 0.5, 64, 0.5, 33)
	z.NoAI = true
	herd = append(herd, z)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		herd.Tick(w, nil, nil)
	}
}
