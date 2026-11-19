package entity

import (
	"math/rand"
	"testing"
)

// BenchmarkMobsTickSniffers steps a herd of three hundred sniffers on
// diggable ground, each in the middle of a dig, through a tick: the goals,
// and the canDig check with its path search that the dig continuation makes.
func BenchmarkMobsTickSniffers(b *testing.B) {
	dirt := defaultState(b, "minecraft:dirt")
	w := newTestWorld()
	floor(w, -129, -129, 128, 128, 63, dirt)
	rng := rand.New(rand.NewSource(3622))
	mobs := make(Mobs, 0, 300)
	for id := int32(1); len(mobs) < cap(mobs); id++ {
		x, z := (rng.Float64()-0.5)*256, (rng.Float64()-0.5)*256
		m := NewMob(TypeSniffer, id, x, 64, z, int64(id))
		m.Age = 0
		m.OnGround = true
		mobs = append(mobs, m)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, m := range mobs {
			// Back into a dig each tick, whatever the last one finished.
			m.sniffer.digging, m.sniffer.sniffCooldown = true, 0
		}
		mobs.Tick(w, nil, nil)
	}
}
