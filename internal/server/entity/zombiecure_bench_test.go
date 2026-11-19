package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkMobsTickCuring steps 24 stationary cures among iron bars, including
// the one-percent nearby-block scan. The long saved timer keeps every body
// curing for the whole measurement.
func BenchmarkMobsTickCuring(b *testing.B) {
	w := newTestWorld()
	bars := defaultState(b, "minecraft:iron_bars")
	floor(w, -8, -8, 8, 8, 60, bars)
	var mobs Mobs
	for i := range int32(24) {
		m := NewMob(TypeZombieVillager, i+1, float64(i%8)-4, 64, float64(i/8)-1, int64(i))
		m.NoAI = true
		effects := effect.Encode([]effect.Instance{effect.New(registry.MustMobEffect("minecraft:strength"), 1<<30, 0, false, true, true)})
		m.LoadFrom(nbt.Compound{"ConversionTime": nbt.Int(1 << 30), "NoAI": nbt.Byte(1), "active_effects": *effects}, w, func() int64 { return 0 })
		mobs = append(mobs, m)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.Tick(w, nil, nil)
	}
}
