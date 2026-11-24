package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

type dolphinTreasureInput struct {
	w       *world.World
	m       *entity.Mob
	x, y, z int32
	found   bool
}

// resolveDolphinTreasure runs the structure searches with no entity lock held.
func (s *Server) resolveDolphinTreasure(requests []dolphinTreasureInput) {
	if len(requests) == 0 {
		return
	}
	ids, _ := registry.StructureTagMembers("minecraft:dolphin_located")
	for i := range requests {
		r := &requests[i]
		r.x, r.z, _, r.found = r.w.NearestStructure(ids, r.x, r.y, r.z, 50)
	}
}

func (c *client) feedDolphin(ent *mobEntity, hand int32) error {
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	if !m.Alive() {
		s.mobsMu.Unlock()
		return nil
	}
	m.FeedDolphin()
	x, y, z, silent := m.X, m.Y, m.Z, m.Silent
	s.mobsMu.Unlock()
	if !silent {
		s.broadcastSoundID(ent.w, registry.SoundEntityDolphinEat, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
	}
	return c.consumeHeldOutsideCreative(hand)
}

// vanilla: Dolphin.DolphinSwimWithPlayerGoal.start/tick
func (c *client) applyDolphinGrace() {
	c.changeEffectsFrom(func(v *player.Vitals, s *effect.Set, _ *player.Attributes) []effect.Change {
		ch, changed := rules.AddEffect(v, s, effect.New(registry.MobEffectDolphinsGrace, 100, 0, false, true, true))
		if !changed {
			return nil
		}
		return []effect.Change{ch}
	}, "minecraft:dolphin")
}

func (t *mobDimTick) dolphinEvent(fx *mobEffects, in *mobTickInputs, ev entity.MobEvent) {
	if ev.Kind == entity.MobDolphinTreasure {
		fx.entityEvent(t.w, ev.Mob.ID, 38) // vanilla: DolphinSwimToTreasureGoal.start
	} else if c := in.clientByEntityID[ev.Player.EntityID]; c != nil {
		if ev.Kind == entity.MobBlinded {
			fx.blinded = append(fx.blinded, c)
		} else {
			fx.dolphinGrace = append(fx.dolphinGrace, c)
		}
	}
}

// applyBlindness is the illusioner's blindness spell landing on a player.
// vanilla: Illusioner.IllusionerBlindnessSpellGoal.performSpellCasting
func (c *client) applyBlindness() {
	c.changeEffectsFrom(func(v *player.Vitals, s *effect.Set, _ *player.Attributes) []effect.Change {
		ch, changed := rules.AddEffect(v, s, effect.New(registry.MobEffectBlindness, 400, 0, false, true, true))
		if !changed {
			return nil
		}
		return []effect.Change{ch}
	}, "minecraft:illusioner")
}
