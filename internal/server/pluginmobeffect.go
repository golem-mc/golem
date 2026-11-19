package server

import (
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
)

func (s *Server) effectListened() bool {
	return s.plugins != nil && s.plugins.Has(pluginruntime.EntityPotionEffect)
}

// pluginEffectPtr is the plugin's view of an instance, nil for none.
func pluginEffectPtr(in *effect.Instance) *plugin.EffectInstance {
	if in == nil {
		return nil
	}
	return &plugin.EffectInstance{Effect: plugin.Effect(in.ID), Duration: int(in.Duration), Amplifier: int(in.Amplifier),
		Ambient: in.Ambient, Particles: in.Visible, Icon: in.ShowIcon}
}

// pluginEffectCause maps a cause to the plugin's, whose Other comes last.
func pluginEffectCause(c effect.Cause) plugin.EffectCause {
	if c == effect.CauseOther {
		return plugin.EffectOther
	}
	return plugin.EffectCause(c - 1)
}

// effectAnswer is the instance an answered event leaves to apply, false when
// the change is refused: cancelled, or an addition whose New was cleared.
func effectAnswer(e *plugin.EntityPotionEffectEvent, now *effect.Instance) (effect.Instance, bool) {
	if e.Cancelled() {
		return effect.Instance{}, false
	}
	if now == nil {
		return effect.Instance{}, true
	}
	if e.New == nil {
		return effect.Instance{}, false
	}
	out := *now
	out.Amplifier, out.Duration = int32(max(e.New.Amplifier, 0)), int32(e.New.Duration)
	out.Ambient, out.Visible, out.ShowIcon = e.New.Ambient, e.New.Particles, e.New.Icon
	return out, true
}

// effectEvent is EntityPotionEffectEvent for old turning into now.
func effectEvent(target plugin.LivingEntity, old, now *effect.Instance, cause effect.Cause) *plugin.EntityPotionEffectEvent {
	return &plugin.EntityPotionEffectEvent{Entity: target, Old: pluginEffectPtr(old), New: pluginEffectPtr(now), Cause: pluginEffectCause(cause)}
}

// mobEffectGate puts a mob's effect changes to EntityPotionEffectEvent
// listeners.
type mobEffectGate struct{ s *Server }

func (g mobEffectGate) Listening() bool { return g.s.effectListened() }

// Ask fires the event with every lock released; an edited New replaces the
// instance, and a New that is gone refuses the change.
// vanilla: LivingEntity.addEffect, removeEffect
func (g mobEffectGate) Ask(m *entity.Mob, old, now *effect.Instance, cause effect.Cause) (effect.Instance, bool) {
	ent := g.s.mobEntityOf(m)
	if ent == nil {
		return effect.Instance{}, true
	}
	e := effectEvent(pluginLivingHandle(ent.uuid), old, now, cause)
	g.s.askUnlocked(func() { g.s.plugins.Fire(e) })
	return effectAnswer(e, now)
}

// askPlayerEffect puts a player's effect change to listeners, outside any
// lock, and returns the instance to apply.
func (c *client) askPlayerEffect(id int32, now *effect.Instance) (effect.Instance, bool) {
	cause := c.effectCause
	c.effectCause = effect.CauseOther
	if !c.srv.effectListened() {
		if now == nil {
			return effect.Instance{}, true
		}
		return *now, true
	}
	var oldp *effect.Instance
	if old, ok := c.player.Effect(id); ok {
		oldp = &old
	} else if now == nil {
		return effect.Instance{}, true
	}
	e := effectEvent(c.pluginSelf(), oldp, now, cause)
	c.srv.firePluginEvent(e)
	return effectAnswer(e, now)
}
