package entity

import (
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Breath (LivingEntity air supply, metadata index 1): down with the eyes
// under water, back up out of it, drowning at the floor.

// airRefillPerTick is LivingEntity.increaseAirSupply's four.
const airRefillPerTick = 4

// drownDamage is LivingEntity.baseTick's drowning hurt at the air floor.
const drownDamage = 2.0

// canBreatheUnderwater reports the kinds that never spend air submerged.
// vanilla: LivingEntity.canBreatheUnderwater, SulfurCube.canBreatheUnderwater,
// HappyGhast.canBreatheUnderwater
func (m *Mob) canBreatheUnderwater() bool {
	return m.SulfurCubeFilled() || m.Type == TypeHappyGhast && m.IsBaby() ||
		m.inTag(registry.EntityTypeTagIDCanBreatheUnderWater)
}

// airStep runs a tick of breath. Water-breathing kinds and effects spare the
// drain; the nautilus effect freezes breath under water instead of refilling
// it. A bubble column at the eyes refills rather than drains (baseTick skips
// the loss). Water kinds use waterAirStep.
// vanilla: LivingEntity.baseTick, AgeableWaterCreature.handleAirSupply
func (m *Mob) airStep(events []MobEvent) []MobEvent {
	if m.waterFluid.eyesIn && !m.waterFluid.bubbleAtEyes && m.Alive() &&
		m.Vehicle != nil && m.Vehicle.inTag(registry.EntityTypeTagIDDismountsUnderwater) {
		// vanilla: LivingEntity.baseTick, Entity.dismountsUnderwater
		events = append(events, MobEvent{Mob: m, Kind: MobDismountedUnderwater})
	}
	if m.Type == TypeNautilus {
		return m.nautilusAirStep(events)
	}
	if m.Type == TypeAxolotl {
		return events
	}
	if m.kind().water && m.Type != TypeDolphin {
		return m.waterAirStep(events)
	}
	if m.IsGuardian() && m.InWater() {
		// vanilla: Guardian.aiStep refills the breath after baseTick spends it
		m.SetMetadataAirSupply(rules.MaxAirSupply)
		return events
	}
	// The effects are copied only for a body whose eyes are under water.
	var effects []effect.Instance
	submerged := m.waterFluid.eyesIn && !m.waterFluid.bubbleAtEyes
	if submerged {
		effects = m.effects.All()
	}
	maxAir := rules.MaxAirSupply
	increase := increaseLivingAirSupply
	if m.Type == TypeDolphin {
		maxAir = dolphinMaxAir
		// vanilla: Dolphin.increaseAirSupply
		increase = func(int) int { return dolphinMaxAir }
	}
	air := m.AirSupply
	drowned := livingAirTick(&air, maxAir, submerged, !m.canBreatheUnderwater() && !rules.HasWaterBreathing(effects), rules.ShouldRefillAirSupply(effects), func() bool {
		if m.Type == TypeIronGolem {
			return false
		}
		bonus := m.attrOr(registry.AttributeOxygenBonus, 0)
		return bonus <= 0 || m.Rand.Float64() < 1/(bonus+1)
	}, increase)
	m.SetMetadataAirSupply(air)
	if drowned {
		landed, soft := m.plainHurt(drownDamage, rules.DamageTypeDrown, false)
		if landed {
			m.noteLastDamage(rules.DamageTypeDrown)
		}
		if landed || !m.Dead {
			events = append(events, MobEvent{Mob: m, Kind: MobBurned, DamageType: rules.DamageTypeDrown, Softened: soft || !landed})
		}
	}
	return events
}

// waterAirStep is AgeableWaterCreature.handleAirSupply, the inverse: out of
// water the supply drops one a tick past its floor, drowning every 21st
// tick; in water it snaps back to the full 300 rather than refilling by
// fours. The bite is drown damage but never bursts a bubble.
// vanilla: AgeableWaterCreature.handleAirSupply
func (m *Mob) waterAirStep(events []MobEvent) []MobEvent {
	if m.InWater() {
		m.SetMetadataAirSupply(rules.MaxAirSupply)
		return events
	}
	if !m.Alive() {
		return events
	}
	m.SetMetadataAirSupply(m.AirSupply - 1)
	if m.AirSupply <= rules.AirDrownFloor {
		m.SetMetadataAirSupply(0)
		if landed, soft := m.plainHurt(drownDamage, rules.DamageTypeDrown, false); landed {
			m.noteLastDamage(rules.DamageTypeDrown)
			events = append(events, MobEvent{Mob: m, Kind: MobBurned,
				DamageType: rules.DamageTypeDrown, Softened: soft, NoBubble: true})
		}
	}
	return events
}
