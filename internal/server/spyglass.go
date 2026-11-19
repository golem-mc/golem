package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The spyglass uses the bow's session (bow.go): Use Item raises
// hand-active with the use sound, release or the duration's end plays the
// stop sound, and a swap or death drops it silently.

// spyglassUseTicks is the use duration. vanilla: SpyglassItem.USE_DURATION
const spyglassUseTicks = 1200

func isSpyglassItem(item int32) bool {
	return registry.Item(item) == registry.ItemSpyglass
}

// handleUseSpyglass starts looking through the held spyglass, reporting
// whether it began. vanilla: SpyglassItem.use
func (c *client) handleUseSpyglass(hand int32) bool {
	if c.player.Vitals().Dead || c.player.GameMode() == protocol.GameModeSpectator {
		return false
	}
	held := c.heldItem(hand)
	if !isSpyglassItem(held.ItemID) {
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
	c.bow = bowSession{hand: hand, item: held.ItemID, ticks: 1, limit: spyglassUseTicks}
	c.bowMu.Unlock()
	c.updateAppearance(func(a *player.Appearance) { a.Eating = true; a.EatingHand = hand })
	c.playSpyglassSound(registry.SoundItemSpyglassUse)
	// vanilla: SpyglassItem.use
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	return true
}

// playSpyglassSound plays a spyglass sound at the holder to nearby players
// but not the holder, whose client plays it. vanilla: Player.playSound
func (c *client) playSpyglassSound(sound registry.Sound) {
	pos := c.player.Position()
	c.srv.broadcastSoundIDExcept(c.theWorld(), c, sound, protocol.SoundCategoryPlayer,
		pos.X, pos.Y, pos.Z, 1, 1)
}
