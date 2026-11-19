package entity

import "github.com/golem-mc/golem/internal/server/effect"

// EffectGate lets the server veto or edit a change to a body's effects.
// Ask runs with every lock the caller held released, like HurtGate's.
type EffectGate interface {
	// Listening reports whether anything wants the change, so an unobserved
	// one pays only this test.
	Listening() bool
	// Ask puts old (nil when absent) turning into now (nil for a removal) to
	// the listeners; it returns what to apply, false to refuse.
	Ask(m *Mob, old, now *effect.Instance, cause effect.Cause) (effect.Instance, bool)
}

// takeEffectCause is the cause the caller named for the next change, spent.
func (m *Mob) takeEffectCause() effect.Cause {
	c := m.EffectCause
	m.EffectCause = effect.CauseOther
	return c
}

// askEffect reports the instance a change may apply, ok false when refused,
// or when the body was removed while the listeners ran.
func (m *Mob) askEffect(old *effect.Instance, now *effect.Instance, cause effect.Cause) (effect.Instance, bool) {
	out, ok := m.EffectGate.Ask(m, old, now, cause)
	return out, ok && !m.Dead
}

// RemoveEffect takes one effect off the body.
// vanilla: LivingEntity.removeEffect
func (m *Mob) RemoveEffect(id int32) (effect.Change, bool) {
	cause := m.takeEffectCause()
	if m.EffectGate != nil && m.EffectGate.Listening() {
		if old, ok := m.effects.Get(id); ok {
			if _, ok := m.askEffect(&old, nil, cause); !ok {
				return effect.Change{}, false
			}
		}
	}
	return m.LivingEffects.RemoveEffect(id)
}

// ClearEffects takes every effect off the body, each put to the gate on its
// own.
// vanilla: LivingEntity.removeAllEffects
func (m *Mob) ClearEffects() []effect.Change {
	cause := m.takeEffectCause()
	if m.EffectGate == nil || !m.EffectGate.Listening() {
		return m.LivingEffects.ClearEffects()
	}
	var changes []effect.Change
	for _, in := range m.effects.All() {
		old := in
		if _, ok := m.askEffect(&old, nil, cause); !ok {
			continue
		}
		if ch, ok := m.LivingEffects.RemoveEffect(in.ID); ok {
			changes = append(changes, ch)
		}
	}
	return changes
}
