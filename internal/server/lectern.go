package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

const (
	lecternPageProperty = 0
	lecternBookPutSound = registry.SoundItemBookPut
)

// lecternBookHeld reports whether an item is one of the lectern_books.
func lecternBookHeld(stack protocol.ItemStack) bool {
	return !stack.Empty() && (stack.ItemID == int32(registry.ItemWrittenBook) || stack.ItemID == int32(registry.ItemWritableBook))
}

// useLectern puts the held book on an empty lectern, reporting whether the
// click was spent; a lectern with a book leaves the click to its window.
// vanilla: LecternBlock.useItemOn, LecternBlock.tryPlaceBook
func (c *client) useLectern(hand, x, y, z int32) bool {
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockLectern || registry.StateBool(state, registry.PropHasBook) {
		return false
	}
	held := c.heldItem(hand)
	if !lecternBookHeld(held) {
		return false
	}
	be := w.EnsureBlockEntity(x, y, z, world.KindLectern)
	if be == nil {
		return false
	}
	one := held
	one.Count = 1
	// vanilla: LecternBlockEntity.setBook
	one, _ = rules.ResolveBook(one, func(t protocol.Text) protocol.Text { return command.ResolveText(c, t) })
	be.Use(func(b *world.BlockEntity) {
		b.Put(0, one)
		b.Page = 0
	})
	w.Touch(x, y, z)
	c.spendHeld(hand)
	c.srv.lecternBookChanged(w, x, y, z, true, c.gameEventContext(0))
	c.srv.broadcastSoundID(w,
		lecternBookPutSound, protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	return true
}

// useLecternWithoutItem is the lectern's main-hand use after the item
// declined: a booked lectern opens its window; a bookless one still
// swallows a click made with an item. vanilla: LecternBlock.useWithoutItem
func (c *client) useLecternWithoutItem(hand, x, y, z int32, state uint16) (bool, error) {
	if hand != protocol.HandMain {
		return false, nil
	}
	if !registry.StateBool(state, registry.PropHasBook) {
		c.useSwing = swingNone
		return !c.heldItem(hand).Empty(), nil
	}
	err := c.openContainer(windowLectern, x, y, z)
	c.awardOpenStat(registry.BlockLectern)
	return true, err
}

// lecternBookChanged writes has_book and clears powered, telling everyone.
// vanilla: LecternBlock.resetBookState
func (s *Server) lecternBookChanged(w *world.World, x, y, z int32, hasBook bool, ctx rules.GameEventContext) {
	s.afterLecternWake(w, func(p *world.Physics) ([]world.BlockChange, []world.Effect, []world.TNTPrime) {
		return p.LecternBookChanged(x, y, z, hasBook)
	})
	ctx.Affected = w.BlockAt(x, y, z)
	s.gameEvent(w, rules.GameEventBlockChange, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, ctx)
}

// lecternPageChanged pulses powered and plays the page turn within
// earshot. vanilla: LecternBlock.signalPageChange
func (s *Server) lecternPageChanged(w *world.World, x, y, z int32) {
	s.afterLecternWake(w, func(p *world.Physics) ([]world.BlockChange, []world.Effect, []world.TNTPrime) {
		return p.LecternPageChanged(x, y, z)
	})
	s.broadcastLevelEvent(w,
		protocol.WorldEventLecternPageTurn, x, y, z, 0)

}

func (s *Server) afterLecternWake(w *world.World, wake func(*world.Physics) ([]world.BlockChange, []world.Effect, []world.TNTPrime)) {
	changes, effects, primes := wake(s.physicsFor(w))
	for _, e := range effects {
		s.tellEffect(w, e)
	}
	for _, t := range primes {
		s.primeTNT(w, t)
	}
	s.tellBlockChanges(w, changes)
}

// handleLecternButton runs a lectern window press: turn a page or take the
// book; an impossible press changes nothing. vanilla: LecternMenu.clickMenuButton
func (c *client) handleLecternButton(view windowView, button int32) error {
	if view.w == nil || view.be == nil || c.player.GameMode() == protocol.GameModeSpectator {
		return nil
	}
	w, x, y, z := view.w.w, view.w.x, view.w.y, view.w.z
	if !lecternHasBookAt(w, x, y, z) {
		return nil
	}
	var moved bool
	var page int32
	var action inventory.LecternAction
	view.be.Use(func(b *world.BlockEntity) {
		var target int
		target, action = inventory.LecternPress(button, b.Page, rules.ModeMayBuild(c.player.GameMode()))
		if action == inventory.LecternTurn {
			moved = b.SetLecternPage(target)
			page = int32(b.Page)
		}
	})
	if action == inventory.LecternTake {
		return c.takeLecternBook(view)
	}
	if !moved {
		return nil
	}
	w.Touch(x, y, z)
	c.srv.lecternPageChanged(w, x, y, z)
	c.srv.lecternPageSent(w, x, y, z, page)
	return nil
}

// lecternPageSent tells every window on the lectern its page.
// vanilla: AbstractContainerMenu.broadcastChanges
func (s *Server) lecternPageSent(w *world.World, x, y, z, page int32) {
	s.containerViewers(w, x, y, z, nil, func(v *client) {
		if window := v.openContainerWindow(); window != nil && window.kind == windowLectern {
			v.queueContainerProperty(window.id, lecternPageProperty, page)
		}
	})
}

// takeLecternBook gives the player the book, or drops it with no room.
// vanilla: LecternMenu.clickMenuButton
func (c *client) takeLecternBook(view windowView) error {
	w, x, y, z := view.w.w, view.w.x, view.w.y, view.w.z
	var book protocol.ItemStack
	view.be.Use(func(b *world.BlockEntity) {
		book = b.Slots[0]
		b.Put(0, protocol.ItemStack{})
		b.Page = 0
	})
	w.Touch(x, y, z)
	c.srv.lecternBookChanged(w, x, y, z, false, c.gameEventContext(0))
	if book.Empty() {
		return nil
	}
	if left := c.addToInventory(book); left > 0 {
		book.Count = left
		c.throwItem(book)
	}
	return nil
}

// lecternHasBookAt reports whether the block is a lectern with has_book.
func lecternHasBookAt(w *world.World, x, y, z int32) bool {
	state := w.BlockAt(x, y, z)
	return registry.BlockOf(state) == registry.BlockLectern && registry.StateBool(state, registry.PropHasBook)
}

// spawnLecternBook throws a broken lectern's book out on the side it faced.
// vanilla: LecternBlockEntity.preRemoveSideEffects
func (s *Server) spawnLecternBook(w *world.World, x, y, z int32, state uint16, book protocol.ItemStack) {
	px, py, pz := entity.LecternBookDropSpot(registry.StateValue(state, registry.PropFacing), x, y, z)
	vx, vy, vz := entity.PopVelocity(s.popRng)
	s.spawnItem(w, book, px, py, pz, vx, vy, vz, entity.BreakPickupDelay)
}
