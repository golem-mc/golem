package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// A bundle in use empties into the world via the bow's session (bow.go):
// Use Item starts it, the tick calls tickBundleUse, and release or the
// duration's end stops it.

// handleUseBundle starts using a held bundle, empty or not, reporting
// whether it began. vanilla: BundleItem.use
func (c *client) handleUseBundle(hand int32) bool {
	if c.player.Vitals().Dead || c.player.GameMode() == protocol.GameModeSpectator {
		return false
	}
	held := c.heldItem(hand)
	if !rules.IsBundle(held.ItemID) {
		return false
	}
	c.bowMu.Lock()
	if c.bow.ticks > 0 {
		c.bowMu.Unlock()
		return false
	}
	c.shieldMu.Lock()
	raised := c.raise.raised
	c.shieldMu.Unlock()
	if raised {
		c.bowMu.Unlock()
		return false
	}
	c.bow = bowSession{hand: hand, item: held.ItemID, ticks: 1, limit: rules.BundleUseTicks}
	c.bowMu.Unlock()
	c.updateAppearance(func(a *player.Appearance) { a.Eating = true; a.EatingHand = hand })
	return true
}

// tickBundleUse runs a use tick; ticks is the session count after this
// tick's increment (2 on the first). It drops a stack when due.
// vanilla: BundleItem.onUseTick
func (c *client) tickBundleUse(hand int32, ticks int32) {
	if !rules.BundleDropsOnUseTick(int(ticks - 2)) {
		return
	}
	held := c.heldItem(hand)
	rest, removed, ok := rules.BundleRemoveFirst(held)
	if !ok {
		return
	}
	c.setHeld(hand, rest)
	c.throwFromHand(removed, nil)
	pos := c.player.Position()
	w := c.theWorld()
	// The holder's client plays remove_one locally. vanilla: Player.playSound
	c.srv.broadcastSoundIDExcept(w, c, registry.SoundItemBundleRemoveOne, protocol.SoundCategoryPlayer,
		pos.X, pos.Y, pos.Z, 0.8, float32(0.8+c.srv.soundRng.Float64()*0.4))
	// Played at the centre of the player's block.
	c.srv.broadcastSoundID(w, registry.SoundItemBundleDropContents, protocol.SoundCategoryPlayer,
		math.Floor(pos.X)+0.5, math.Floor(pos.Y)+0.5, math.Floor(pos.Z)+0.5, 0.8, float32(0.8+c.srv.soundRng.Float64()*0.4))
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
}
