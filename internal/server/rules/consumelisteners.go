package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// omenTicks is the Bad Omen an ominous bottle gives.
// vanilla: OminousBottleAmplifier.EFFECT_DURATION
const omenTicks = 120000

// ConsumeListeners applies the stack's stew effects and Bad Omen components.
// vanilla: SuspiciousStewEffects.onConsume, OminousBottleAmplifier.onConsume
func ConsumeListeners(v *player.Vitals, effects *effect.Set, stack protocol.ItemStack) []effect.Change {
	var changes []effect.Change
	add := func(in effect.Instance) {
		if change, ok := AddEffect(v, effects, in); ok {
			changes = append(changes, change)
		}
	}
	for _, e := range stack.SuspiciousStewEffectsOf() {
		add(effect.New(e.Effect, e.Duration, 0, false, true, true))
	}
	if amplifier, ok := stack.OminousBottleAmplifier(); ok {
		add(effect.New(registry.MobEffectBadOmen, omenTicks, amplifier, false, false, true))
	}
	return changes
}
