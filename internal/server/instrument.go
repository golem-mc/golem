package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// handleUseInstrument plays a goat horn: it raises the horn for the use
// duration in the bow's session (bow.go), plays the sound to all in range but
// the player (whose client plays it), and starts the cooldown for the use
// duration. A horn on cooldown does nothing.
// vanilla: InstrumentItem.use, ServerPlayerGameMode.useItem
func (c *client) handleUseInstrument(hand int32) {
	held := c.heldItem(hand)
	if held.Empty() || registry.Item(held.ItemID) != registry.ItemGoatHorn {
		return
	}
	if c.player.Vitals().Dead || c.player.GameMode() == protocol.GameModeSpectator {
		return
	}
	instrument := registry.DefaultInstrumentData()
	if id, ok := held.InstrumentOf(); ok {
		if in, known := registry.InstrumentByID(id); known {
			instrument = in
		}
	}
	useTicks := int32(math.Floor(float64(instrument.UseSeconds) * 20))
	c.startInstrumentUse(hand, held.ItemID, useTicks)
	pos := c.player.Position()
	c.srv.broadcastEntitySoundID(c.theWorld(), c, c.player.EntityID, instrument.Sound,
		protocol.SoundCategoryRecord, pos.X, pos.Y, pos.Z, instrument.RangeBlocks/16, 1)
	c.gameEventAt(rules.GameEventInstrumentPlay, pos.X, pos.Y, pos.Z, 0)
	// vanilla: InstrumentItem.use
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	if useTicks > 0 {
		c.startItemCooldown(registry.CooldownGroup(held), useTicks)
	}
}

// startInstrumentUse raises the horn for useTicks unless another use (or a
// raised shield) is running, which the sound and cooldown still follow.
// vanilla: LivingEntity.startUsingItem
func (c *client) startInstrumentUse(hand, item, useTicks int32) {
	c.bowMu.Lock()
	defer c.bowMu.Unlock()
	if c.bow.ticks > 0 {
		return
	}
	c.shieldMu.Lock()
	raised := c.raise.raised
	c.shieldMu.Unlock()
	if raised {
		return
	}
	c.bow = bowSession{hand: hand, item: item, ticks: 1, limit: useTicks}
	c.updateAppearance(func(a *player.Appearance) { a.Eating = true; a.EatingHand = hand })
}
