package entity

import (
	"math"
	"math/rand/v2"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// EnvironmentStep advances the environment even when travel is disabled.
// vanilla: Entity.baseTick, LivingEntity.baseTick, tickEffects, aiStep
func (m *Mannequin) EnvironmentStep(w World, hurt func(float64, registry.DamageType)) (drowned, extinguished bool) {
	m.Age++
	if m.Dead || m.Dying() {
		return
	}
	_, height := MannequinDimensions(m.Pose)
	box := NewAABB(m.BlastBox())
	// Step already read the fluids for a travelling body.
	if m.Immovable {
		updateFluid(w, box, m.X, m.Y+height*0.85, m.Z, true, &m.water, &m.lava)
	}
	var scan contactScan
	if scan.touches(w, box, contactFire) {
		livingFireContact(w, box, &m.FireTicks, func(ticks int, x, y, z int32) {
			m.IgniteForTicksBy(ticks, CombustSource{HasBlock: true, X: x, Y: y, Z: z})
		}, hurt)
	}
	if m.FireTicks != 0 {
		extinguished = livingFireTick(&m.FireTicks, rainLandsOn(w, m.X, m.Y, m.Z, height) || m.water.height > 0, false, m.lava.height > 0, hurt)
	}
	submerged := m.water.eyesIn && !m.water.bubbleAtEyes
	breathing := m.HasEffect(registry.MobEffectWaterBreathing) || m.HasEffect(registry.MobEffectConduitPower)
	nautilus := m.HasEffect(registry.MobEffectBreathOfTheNautilus)
	if livingAirTick(&m.AirSupply, rules.MaxAirSupply, submerged, !breathing && !nautilus, breathing || !nautilus, func() bool {
		bonus := m.Attributes.Value(registry.AttributeOxygenBonus)
		return bonus <= 0 || rand.Float64() < 1/(bonus+1)
	}, increaseLivingAirSupply) {
		drowned = true
		hurt(drownDamage, rules.DamageTypeDrown)
	}
	if m.lava.height > 0 || scan.touches(w, box, contactLavaCauldron) {
		livingLavaContact(w, box, m.lava.height > 0, func(ticks int, x, y, z int32) {
			m.IgniteForTicksBy(ticks, CombustSource{HasBlock: true, X: x, Y: y, Z: z})
		}, hurt)
	}
	inSnow := scan.touches(w, box, contactPowderSnow)
	if inSnow && m.FireTicks > 0 {
		m.FireTicks = 0
	}
	m.TicksFrozen = rules.FreezeTicksStep(m.TicksFrozen, inSnow, inSnow && !rules.FreezeImmuneWearables(m.Worn[:]...))
	if m.TicksFrozen >= rules.FullyFrozenTicks && m.Age%rules.FreezeHurtTicks == 0 {
		hurt(rules.FreezeHurt(false), rules.DamageTypeFreeze)
	}
	changes := m.effects.Tick(int32(m.Age), func(in *effect.Instance) bool {
		return applyLivingEffect(in, protocol.EntityTypeMannequin, m.Health, m.HealthCap(), m.Absorption, m.Heal, hurt)
	})
	if len(changes) > 0 {
		m.revision++
		m.clampAbsorption()
		rules.UpdateEffectAttributes(m.Attributes, m.AllEffects())
		m.SetHealth(min(m.Health, m.HealthCap()))
	}
	return
}

// IgniteForTicks applies the body's burning-time multiplier and clears frost.
// vanilla: LivingEntity.igniteForTicks
func (m *Mannequin) IgniteForTicks(ticks int) {
	m.IgniteForTicksBy(ticks, CombustSource{})
}

// IgniteForTicksBy is IgniteForTicks with the block or entity that caused it.
// vanilla: LivingEntity.igniteForTicks
func (m *Mannequin) IgniteForTicksBy(ticks int, src CombustSource) {
	ticks = rules.ScaledIgniteTicks(ticks, m.Attributes.Value(registry.AttributeBurningTime))
	if ticks > m.FireTicks && m.Combust != nil {
		ticks = m.Combust(ticks, src)
	}
	if ticks > m.FireTicks {
		m.FireTicks = ticks
	}
	m.TicksFrozen = 0
}

// HealthCap includes health boost's attribute modifier.
func (m *Mannequin) HealthCap() float64 { return rules.MaxHealthOf(&m.effects, m.Attributes) }

// Heal restores health up to the effect-adjusted cap.
func (m *Mannequin) Heal(amount float64) {
	if !m.Dying() {
		m.SetHealth(math.Min(m.HealthCap(), m.Health+amount))
	}
}

// RefusesEffectDamage is LivingEntity.hurtServer's fire-resistance gate.
func (m *Mannequin) RefusesEffectDamage(typ registry.DamageType) bool {
	_, resistant := m.effects.Amplifier(registry.MobEffectFireResistance)
	return resistant && typ.InTag(registry.DamageTypeTagIDIsFire)
}

// hurtAfterEffects spends resistance and absorption after the hurt window.
func (m *Mannequin) hurtAfterEffects(amount float64, typ registry.DamageType) float64 {
	return m.absorb(rules.Resist(m.AllEffects(), typ, amount))
}

var mannequinAttributeSupplier = func() *player.Supplier {
	s := &player.Supplier{Names: make(map[string]bool)}
	for _, name := range livingAttributes {
		s.Names["minecraft:"+name] = true
	}
	return s
}()
