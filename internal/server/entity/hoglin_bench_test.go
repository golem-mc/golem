package entity

import (
	"math/rand"
	"testing"
)

// BenchmarkMobsTickIdleHoglins steps a herd of three hundred idle,
// zombification-immune hoglins through a tick: their idle RunOne of stroll,
// walk to a look target and pause, the glances and the walks under way.
func BenchmarkMobsTickIdleHoglins(b *testing.B) {
	w := newTestWorld()
	floor(w, -129, -129, 128, 128, 63, defaultState(b, "minecraft:stone"))
	rng := rand.New(rand.NewSource(5518))
	mobs := make(Mobs, 0, 300)
	for id := int32(1); len(mobs) < cap(mobs); id++ {
		x, z := (rng.Float64()-0.5)*256, (rng.Float64()-0.5)*256
		m := NewMob(TypeHoglin, id, x, 64, z, int64(id))
		m.OnGround = true
		m.ZombificationImmune = true
		mobs = append(mobs, m)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, m := range mobs {
			m.gone = false
		}
		mobs.Tick(w, nil, nil)
	}
}

// BenchmarkHoglinPiglinHerd ticks adult hoglins idling among adult piglins
// over flat stone, the piglins' sight the hoglins' piglin checks scan.
func BenchmarkHoglinPiglinHerd(b *testing.B) {
	w := newTestWorld()
	floor(w, -32, -32, 32, 32, 63, defaultState(b, "minecraft:stone"))
	herd := make(Mobs, 0, 32)
	for i := range 32 {
		typ := TypeHoglin
		if i%2 == 0 {
			typ = TypePiglin
		}
		m := NewMob(typ, int32(i+1), float64(i%8)*3-10, 64, float64(i/8)*3-5, int64(i+1))
		m.OnGround = true
		m.ZombificationImmune = true
		m.barter.Immune = true
		herd = append(herd, m)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		herd.Tick(w, nil, nil)
	}
}
