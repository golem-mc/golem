package entity

import (
	"math/rand"
	"testing"
)

// BenchmarkMobsTickCopperGolems steps a herd of three hundred copper golems,
// a quarter at each weathering stage and every fifth waxed, through a tick:
// the goals, the weathering timer and the oxidized golems' statue roll, with
// a turned golem stood back up before the next tick.
func BenchmarkMobsTickCopperGolems(b *testing.B) {
	w := newTestWorld()
	floor(w, -129, -129, 128, 128, 63, defaultState(b, "minecraft:stone"))
	rng := rand.New(rand.NewSource(3624))
	mobs := make(Mobs, 0, 300)
	for id := int32(1); len(mobs) < cap(mobs); id++ {
		x, z := (rng.Float64()-0.5)*256, (rng.Float64()-0.5)*256
		m := NewMob(TypeCopperGolem, id, x, 64, z, int64(id))
		m.OnGround = true
		m.SetCopperWeather(int(id) % 4)
		if id%5 == 0 {
			m.CopperWax()
		}
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
