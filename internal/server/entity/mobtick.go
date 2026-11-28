package entity

import "github.com/golem-mc/golem/internal/server/rules"

// baseTick preserves contact, effect, and death-countdown order before goals.
func (m *Mob) baseTick(w World, scan *contactScan, players []Player, events []MobEvent) ([]MobEvent, bool) {
	var diedInBaseTick bool
	if m.BoardingCooldown > 0 {
		m.BoardingCooldown--
	}
	// Fluids are read first (baseTick's updateFluidInteraction precedes the
	// fire countdown), then fire blocks light and bite, then the standing
	// fire ticks. Lava runs last so its relight lands after the countdown's
	// spend: a body in lava stays at the full fifteen seconds, as vanilla's
	// countdown also precedes the lava's block effects.
	wasInWater := m.InWater()
	m.updateMobFluids(w)
	if !wasInWater && m.InWater() && m.Age > 1 {
		events = m.waterSplash(events)
	}
	if m.Type == TypeStrider {
		events = m.striderVoiceStep(events)
		if !m.NoAI {
			m.striderColdStep(w)
		}
	}
	events = m.hotFloorContact(w, events)
	events = m.stepOnBlock(w, events)
	events = m.fireContact(w, scan, events)
	events = m.cactusContact(w, scan, events)
	events = m.fireStep(events)
	events = m.wallStep(w, events)
	events = m.WaterSensitiveStep(w, events)
	if m.Type == TypeEnderman {
		events = m.endermanStep(w, events)
	}
	events = m.voidStep(w, events)
	events = m.airStep(events)
	if m.Type == TypeAxolotl {
		events = m.axolotlAirStep(w, events)
	}
	events = m.lavaContact(w, scan, events)
	events = m.powderSnowContact(w, scan, events)
	events = m.powderSnowFlakes(w, players, events)
	events = m.cauldronContact(w, scan, events)
	events = m.douseStep(w, events)
	// Freeze runs after the fire and lava that clear it: vanilla's freeze
	// bite lands after applyEffectsFromBlocks.
	events = m.freezeStep(w, scan, events)
	arrows := m.ArrowCount
	rules.StuckStep(&arrows, &m.removeArrowTime)
	m.SetMetadataArrowCount(arrows)
	m.TickTimers()
	// These bites land ahead of vanilla's tickDeath, so a body they kill
	// counts from this tick rather than the next.
	diedInBaseTick = m.Dead
	// Effects tick here, after tickDeath as in baseTick, so a body an effect
	// kills counts as a baseTick death with its corpse from the next tick.
	// vanilla: LivingEntity.baseTick
	events = m.effectsStep(events)
	// Wither rose after the effects tick (aiStep's applyEffectsFromBlocks
	// follows baseTick's tickEffects), so its first bite falls a tick after
	// stepping in.
	// vanilla: WitherRoseBlock.entityInside
	events = m.witherRoseContact(w, scan, events)
	events = m.eyeblossomContact(w, scan, events)
	events = m.cropContact(w, scan, events)
	events = m.packLocatedStep(events)

	// The lead runs at the end of baseTick, before goals and walk, so its
	// stretch adds to this tick's travel.
	// vanilla: Leashable.tickLeash
	events = m.leashStep(w, events)
	events = m.reinforcementStep(w, events)
	events = m.queuedStep(events)
	return events, diedInBaseTick
}

// queuedStep hands over the events queued since the last step.
func (m *Mob) queuedStep(events []MobEvent) []MobEvent {
	events = append(events, m.queued...)
	m.queued = nil
	return events
}
