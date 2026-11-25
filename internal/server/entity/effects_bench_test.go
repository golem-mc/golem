package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func effectBenchMob() *Mob {
	m := NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1)
	m.SetAttributes(m.BuildAttributes(nbt.List{}))
	for _, id := range []int32{registry.MobEffectSpeed, registry.MobEffectStrength, registry.MobEffectJumpBoost, registry.MobEffectRegeneration} {
		m.AddEffect(effect.New(id, 1200, 1, false, true, true))
	}
	return m
}

// BenchmarkMobTrackingTickWithEffects is the per-tick modifier sync of a mob
// with an installed map and active effects.
func BenchmarkMobTrackingTickWithEffects(b *testing.B) {
	m := effectBenchMob()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.SyncSpeedModifiers(m.Attributes())
	}
}

// BenchmarkMobEffectChange adds and removes health boost on a mob that
// already carries effects.
func BenchmarkMobEffectChange(b *testing.B) {
	m := effectBenchMob()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.AddEffect(effect.New(registry.MobEffectHealthBoost, 1200, 1, false, true, true))
		m.RemoveEffect(registry.MobEffectHealthBoost)
	}
}
