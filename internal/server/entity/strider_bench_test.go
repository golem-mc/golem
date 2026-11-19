package entity

import (
	"github.com/golem-mc/golem/nbt"
	"testing"
)

// BenchmarkStridersStrollLava ticks active striders spread over a lava lake.
func BenchmarkStridersStrollLava(b *testing.B) {
	w := newTestWorld()
	floor(w, -48, -48, 48, 48, 62, defaultState(b, "minecraft:stone"))
	floor(w, -48, -48, 48, 48, 63, defaultState(b, "minecraft:lava"))
	mobs := make(Mobs, 0, 64)
	for i := range 64 {
		m := NewMob(TypeStrider, int32(i+1), float64(i%8)*8-28, 63.5, float64(i/8)*8-28, int64(i+1))
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		mobs = append(mobs, m)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, m := range mobs {
			m.NoActionTicks = 0
			m.SyncSpeedModifiers(m.Attributes())
		}
		mobs.Tick(w, nil, nil)
	}
}
