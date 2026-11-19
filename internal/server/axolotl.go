package server

import (
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Axolotl.applySupportingEffects
func (c *client) applyAxolotlSupport() {
	const regen = registry.MobEffectRegeneration
	c.changeEffectsFrom(func(v *player.Vitals, s *effect.Set, _ *player.Attributes) []effect.Change {
		have, ok := s.Get(regen)
		if ok && (have.IsInfinite() || have.Duration >= 2400) {
			return nil
		}
		duration := int32(100)
		if ok {
			duration = min(2400, duration+have.Duration)
		}
		ch, changed := rules.AddEffect(v, s, effect.New(regen, duration, 0, false, true, true))
		if !changed {
			return nil
		}
		return []effect.Change{ch}
	}, "minecraft:axolotl")
	// Removal has no source in LivingEntity.onEffectRemoved.
	c.RemoveEffect(registry.MobEffectMiningFatigue)
}
