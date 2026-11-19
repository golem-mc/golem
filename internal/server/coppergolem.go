package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// takeFromCopperGolem is an empty-handed click: the golem throws the stack it
// carries toward the player.
// vanilla: CopperGolem.mobInteract, BehaviorUtils.throwItem
func (c *client) takeFromCopperGolem(ent *mobEntity) error {
	s := c.srv
	pos := c.player.Position()
	s.mobsMu.Lock()
	ev, ok := ent.mob.CopperGolemThrowCarried(pos.X, pos.Y, pos.Z)
	s.mobsMu.Unlock()
	c.useSwing = swingNone
	if ok {
		c.useSwing = swingPredicted
		s.throwPiglinItem(piglinThrow{w: ent.w, event: ev, thrower: ent.uuid})
	}
	return nil
}

// useOnCopperGolem is a click the golem accepts (CopperAccepts): shearing the
// antenna's item, waxing with honeycomb, scraping the wax or one stage of
// oxidation with an axe. The golem's weathering stage reaches watchers with
// its next metadata delta.
// vanilla: CopperGolem.mobInteract, shear
func (c *client) useOnCopperGolem(ent *mobEntity, hand int32, held protocol.ItemStack) error {
	s := c.srv
	item := registry.Item(held.ItemID)
	s.mobsMu.Lock()
	m := ent.mob
	x, y, z, category := m.X, m.Y, m.Z, mobSoundCategory(m.Type)
	var scraped entity.CopperScrape
	switch item {
	case registry.ItemShears:
	case registry.ItemHoneycomb:
		m.CopperWax()
	default:
		scraped = m.CopperScrapeWith()
	}
	s.mobsMu.Unlock()
	blockX, blockY, blockZ := int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
	switch item {
	case registry.ItemShears:
		if take, done := s.shearOff(ent, held); done {
			s.tellShear(ent.w, take, protocol.SoundCategoryPlayer)
		}
		s.gameEvent(ent.w, rules.GameEventShear, x, y, z, c.gameEventContext(0))
		c.wearHeld(hand, 1)
	case registry.ItemHoneycomb:
		c.useSwing = swingServer
		s.broadcastLevelEvent(ent.w, rules.LevelEventWaxOn, blockX, blockY, blockZ, 0)
		s.broadcastSoundID(ent.w, rules.SoundWaxOn, protocol.SoundCategoryBlock, x, y, z, 1, 1)
		return c.consumeHeldOutsideCreative(hand)
	default:
		c.useSwing = swingServer
		event := int32(rules.LevelEventScrape)
		if scraped == entity.CopperScrapeWax {
			event = rules.LevelEventWaxOff
		}
		s.broadcastEntitySoundID(ent.w, nil, m.ID, registry.SoundItemAxeScrape, category, x, y, z, 1, 1)
		s.broadcastLevelEvent(ent.w, event, blockX, blockY, blockZ, 0)
		c.wearHeld(hand, 1)
	}
	return nil
}
