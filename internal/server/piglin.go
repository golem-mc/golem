package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// piglinThrow is an item a mob tossed or left during the tick.
type piglinThrow struct {
	w       *world.World
	event   entity.MobEvent
	thrower uuid.UUID
}

// throwPiglinItem drops the item: a piglin's with the default pickup delay, a
// cat's gift with none and a dolphin's toss with a forty-tick delay.
// vanilla: BehaviorUtils.throwItem, ItemEntity
func (s *Server) throwPiglinItem(t piglinThrow) {
	ev := t.event
	delay := entity.BreakPickupDelay
	if ev.Mob.Type == entity.TypeDolphin {
		delay = entity.ThrownPickupDelay
	}
	if ev.Kind == entity.MobGaveGift {
		delay = 0
	}
	it := s.newItemEntity(t.w, ev.Stack, ev.X, ev.Y, ev.Z, ev.VX, ev.VY, ev.VZ, delay)
	it.thrower = t.thrower
	s.registerFreshItem(it)
	if ev.Liked != uuid.Nil {
		s.allayDropAdvancements(t.w, ev)
	}
}

// offerGoldToPiglin gives the held gold ingot to a piglin to admire before
// bartering; creative keeps the ingot. vanilla: PiglinAi.mobInteract
func (c *client) offerGoldToPiglin(ent *mobEntity, hand int32, held protocol.ItemStack) (bool, error) {
	s := c.srv
	s.mobsMu.Lock()
	taken := ent.mob.AdmireGold(held)
	after := s.bodyInteraction(ent)
	s.mobsMu.Unlock()
	if !taken {
		return false, nil
	}
	if c.player.GameMode() != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return true, err
		}
	}
	c.interactedWithEntityAdvancements(after, held)
	c.useSwing = swingServer
	return true, nil
}

// angerNearbyPiglins tells the piglins near the player that it opened a
// guarded container (needSight) or broke a guarded block.
// vanilla: PiglinAi.angerNearbyPiglins
func (c *client) angerNearbyPiglins(needSight bool) {
	s := c.srv
	at := c.player.Position()
	d := c.dims(c.player.Appearance().Pose)
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, m := range s.herdOf(c.theWorld()).herd {
		m.Provoke(c.player.EntityID, at.X, at.Y, at.Z, d.Width, d.Height, needSight)
	}
}

func (t *mobDimTick) threwItem(fx *mobEffects, ev entity.MobEvent) {
	throw := piglinThrow{w: t.w, event: ev}
	if ev.Owned {
		if ent := t.h.book[ev.Mob]; ent != nil {
			throw.thrower = ent.uuid
		}
	}
	fx.throws = append(fx.throws, throw)
}
