package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// useCampfire puts one item with a campfire recipe on the first empty slot
// of a campfire, lit or not, the click spent either way; else a
// douses_campfires item puts a lit one out.
// vanilla: CampfireBlock.useItemOn
func (c *client) useCampfire(hand, x, y, z int32) bool {
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	if !registry.BlockOf(state).InTag(registry.TagIDCampfires) {
		return false
	}
	held := c.heldItem(hand)
	if registry.CookingMatch(world.KindCampfire.Name(), held) == nil {
		return c.douseCampfire(hand, x, y, z, state, held)
	}
	be := w.EnsureBlockEntity(x, y, z, world.KindCampfire)
	if be == nil {
		return false
	}
	placed := false
	be.Use(func(b *world.BlockEntity) {
		placed = b.PlaceFood(held) && w.TouchEntity(b)
	})
	c.useSwing = swingNone
	if placed {
		c.useSwing = swingServer
		c.awardStat(player.Custom(registry.CustomStatInteractWithCampfire), 1)
		c.spendHeld(hand)
		c.srv.broadcastBlockEntity(w, x, y, z, be)
		c.gameEventAt(rules.GameEventBlockChange, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, state)
	}
	return true
}

// douseCampfire puts a lit campfire out with a douses_campfires item: the
// extinguish event to everyone, one wear. vanilla: CampfireBlock.useItemOn, douse
func (c *client) douseCampfire(hand, x, y, z int32, state uint16, held protocol.ItemStack) bool {
	if held.Empty() || !registry.Item(held.ItemID).InTag(registry.ItemTagIDDousesCampfires) || !registry.StateLit(state) {
		return false
	}
	out, ok := registry.WithBool(state, registry.PropLit, false)
	if !ok {
		return false
	}
	w := c.theWorld()
	c.srv.broadcastLevelEvent(w, rules.LevelEventExtinguishFire, x, y, z, 0)
	c.gameEventAtCell(rules.GameEventBlockChange, x, y, z)
	c.setUsedBlock(w, x, y, z, out)
	c.wearHeld(hand, 1)
	return true
}

// tickCampfire cooks a lit campfire's items and cools an unlit one's,
// dropping finished items. vanilla: CampfireBlockEntity.cookTick, cooldownTick
func (s *Server) tickCampfire(w *world.World, be *world.BlockEntity) {
	var state uint16
	var done []protocol.ItemStack
	changed := false
	be.Tick(func(b *world.BlockEntity) bool {
		state = w.BlockAt(b.X, b.Y, b.Z)
		if !registry.BlockOf(state).InTag(registry.TagIDCampfires) {
			return false
		}
		var cook func(int, protocol.ItemStack, protocol.ItemStack) (protocol.ItemStack, bool)
		if ev := s.blockEntityEvents(w); ev != nil {
			cook = func(_ int, source, result protocol.ItemStack) (protocol.ItemStack, bool) {
				return ev.cook(b, source, result)
			}
		}
		done, changed = b.CampfireTickWith(registry.StateBool(state, registry.PropLit), cook)
		return !changed
	})
	if changed {
		w.Touch(be.X, be.Y, be.Z)
	}
	if len(done) > 0 {
		for _, stack := range done {
			s.spillContainer(w, be.X, be.Y, be.Z, []protocol.ItemStack{stack})
			s.gameEvent(w, rules.GameEventBlockChange, float64(be.X)+0.5, float64(be.Y)+0.5, float64(be.Z)+0.5, rules.GameEventContext{Affected: state})
		}
		s.broadcastBlockEntity(w, be.X, be.Y, be.Z, be)
	}
}
