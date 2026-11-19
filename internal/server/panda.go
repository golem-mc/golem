package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
)

// pandaScared is Panda.mobInteract's first branch: a scared panda passes.
func (s *Server) pandaScared(ent *mobEntity) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	return ent.mob.PandaScared()
}

// pandaStandUp is Panda.mobInteract on a panda lying on its back: it stands
// and the item stays in the hand.
func (s *Server) pandaStandUp(ent *mobEntity) bool {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	return ent.mob.Alive() && ent.mob.PandaStandUp()
}

// pandaSnack is the last branch of Panda.mobInteract: an adult that cannot
// breed sits and eats the food, dropping what it held unless the player has
// infinite materials.
// vanilla: Panda.mobInteract, Player.hasInfiniteMaterials
func (c *client) pandaSnack(ent *mobEntity, hand int32) (bool, error) {
	s := c.srv
	item := c.heldItem(hand)
	s.mobsMu.Lock()
	m := ent.mob
	if !m.Alive() || m.IsBaby() {
		s.mobsMu.Unlock()
		return false, nil
	}
	held, ok := m.PandaSnack(item.ItemID)
	x, y, z := m.X, m.Y, m.Z
	s.mobsMu.Unlock()
	if !ok {
		return false, nil
	}
	creative := c.player.GameMode() == protocol.GameModeCreative
	if !held.Empty() && !creative {
		s.spawnItemAtFeet(ent.w, held, x, y, z)
	}
	if !creative {
		if err := c.consumeHeld(hand); err != nil {
			return true, err
		}
	}
	c.useSwing = swingServer
	return true, nil
}

// pandaEvent applies a panda's dropped item and its eat game event.
// vanilla: Entity.spawnAtLocation, Panda.handleEating
func (t *mobDimTick) pandaEvent(fx *mobEffects, ev entity.MobEvent) {
	if ev.Kind == entity.MobDroppedItem {
		fx.deaths = append(fx.deaths, mobDrops{w: t.w, stacks: []protocol.ItemStack{ev.Stack}, by: t.h.MobUUID(ev.Mob), x: ev.X, y: ev.Y, z: ev.Z})
		return
	}
	fx.gameEvent(t.w, rules.GameEventEat, ev.Mob.X, ev.Mob.Y, ev.Mob.Z, rules.GameEventContext{Mob: t.h.MobUUID(ev.Mob)})
}

// foxDust is the block-break particles of a fox's faceplant.
func (t *mobDimTick) foxDust(ev entity.MobEvent) mobSend {
	return mobSend{w: t.w, id: protocol.IDPlayWorldEvent,
		data: worldEvent(protocol.WorldEventBlockBreak, int32(ev.X), int32(ev.Y), int32(ev.Z), ev.Status),
		near: true, x: ev.X, y: ev.Y, z: ev.Z, radius: levelEventRange}
}
