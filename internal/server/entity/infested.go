package entity

import (
	"math/rand"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

const effectInfested = registry.MobEffectInfested

// rollInfested is InfestedMobEffect.onMobHurt: on a roll within the effect's
// chance, one or two silverfish come out of the body's middle.
// vanilla: InfestedMobEffect.onMobHurt
func (m *Mob) rollInfested() {
	if _, ok := m.effects.Get(effectInfested); !ok {
		return
	}
	if m.infestRng == nil {
		m.infestRng = rand.New(rand.NewSource(m.Rand.Int63()))
	}
	r := m.infestRng
	if r.Float32() > rules.InfestedChance {
		return
	}
	lx, ly, lz := lookVector(m.Yaw, m.Pitch)
	for range rules.InfestedCount(r) {
		vx, vy, vz := rules.InfestedLaunch(r, lx, ly, lz)
		m.queued = append(m.queued, MobEvent{Mob: m, Kind: MobInfestedRelease,
			X: m.X, Y: m.Y + m.height()/2, Z: m.Z, VX: vx, VY: vy, VZ: vz})
	}
}

// SeedInfested replaces the dice the infested effect rolls on.
func (m *Mob) SeedInfested(seed int64) { m.infestRng = rand.New(rand.NewSource(seed)) }
