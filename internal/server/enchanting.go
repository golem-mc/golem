package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The enchanting table window as the client sees it; a press is
// internal/server/inventory's (EnchantPress).

// sendEnchantProperties sends all ten properties, as a window's first fill
// does, remembering them so later updates send only changes.
func (c *client) sendEnchantProperties(view windowView, props [protocol.EnchantPropertyCount]int32) error {
	for i, value := range props {
		if err := c.sendContainerProperty(view, int32(i), value); err != nil {
			return err
		}
	}
	if view.w != nil {
		view.w.SentProps = props
	}
	return nil
}

// recomputeEnchantOffers remakes the offers from the item, the player's
// seed and the shelves in reach, as the window's data slots. The caller
// holds the inventory lock.
func (c *client) recomputeEnchantOffers(view windowView) {
	shelves := rules.CountBookshelves(c.theWorld(), view.w.x, view.w.y, view.w.z)
	view.w.Props = inventory.EnchantProps(c.xpSeed.Load(), shelves, view.w.Menu[0])
}

// updateEnchantOffers recomputes the offers and sends changed properties.
// The seed is sent as its low 16 bits, all the client needs for the glyphs.
func (c *client) updateEnchantOffers(view windowView) error {
	if view.w == nil || view.w.virtual {
		return nil
	}
	listening := c.enchantListening(pluginruntime.PrepareEnchant)
	if listening {
		c.pluginPrepareEnchant(view)
	}
	var err error
	c.player.Inventory(func(inv *player.Inventory) {
		if !listening {
			c.recomputeEnchantOffers(view)
		}
		for i, value := range view.w.Props {
			if value == view.w.SentProps[i] {
				continue
			}
			if err = c.sendContainerProperty(view, int32(i), value); err != nil {
				return
			}
		}
		view.w.SentProps = view.w.Props
	})
	return err
}

// handleContainerButtonClick handles Click Container Button: an enchanting
// offer press, or a stonecutter recipe or loom pattern pick. An unaffordable
// press or a pick naming nothing changes nothing; a spectator's is dropped.
// Three lock passes: game mode and level read outside the inventory lock
// (whose callback holds the player's), slots changed under it, then sound,
// corrections and level cost after.
// vanilla: EnchantmentMenu.clickMenuButton, StonecutterMenu.clickMenuButton, LoomMenu.clickMenuButton
func (c *client) handleContainerButtonClick(r *protocol.Reader) error {
	windowID := r.ReadVarInt()
	button := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return c.decodeFailure("container_button_click", err)
	}
	if c.player.GameMode() == protocol.GameModeSpectator {
		return nil
	}
	view := c.currentView()
	if windowID != view.id || (view.kind != windowLectern &&
		view.kind != windowEnchant && view.kind != windowStonecutter && view.kind != windowLoom) {
		c.log.Debug("ignoring a button click on a window that has no buttons",
			"window", windowID, "button", button)
		return nil
	}
	// The window closes next tick; a press before then is refused.
	// vanilla: AbstractContainerMenu.stillValid
	if view.w == nil || !c.containerStillOpen(view.w) {
		c.log.Debug("ignoring a button click on a window out of reach", "window", windowID)
		return nil
	}
	if view.kind == windowLectern {
		return c.handleLecternButton(view, button)
	}
	if view.kind == windowLoom {
		return c.handleLoomButton(view, button)
	}
	if view.kind == windowStonecutter {
		return c.handleStonecutterButton(view, button)
	}
	// The click indexes the costs; vanilla drops one outside them rather than
	// reading costs[buttonId].
	if button < 0 || button > 2 {
		c.log.Debug("ignoring a button click outside the table's offers", "button", button)
		return nil
	}

	creative := c.player.GameMode() == protocol.GameModeCreative
	level := int32(c.player.Vitals().ExperienceLevel)

	var enchanted, lapis protocol.ItemStack
	var plan inventory.EnchantPlan
	var applied bool
	c.player.Inventory(func(inv *player.Inventory) {
		w := view.w
		costs := [3]int32(w.Props[:3])
		plan, applied = inventory.PlanEnchant(w.Menu[:], button, c.xpSeed.Load(), costs, level, creative)
	})
	if applied && c.enchantListening(pluginruntime.EnchantItem) {
		plan, applied = c.pluginEnchantPress(view, plan, button, level, creative)
		if !applied {
			// The client predicted nothing, but a cancelled press resends the table.
			c.player.Inventory(func(inv *player.Inventory) {
				_ = c.sendContainerSlot(inv, view, 0, view.w.Menu[0])
				_ = c.sendContainerSlot(inv, view, 1, view.w.Menu[1])
			})
		}
	}
	if !applied {
		return nil
	}
	cost := plan.Cost
	c.player.Inventory(func(inv *player.Inventory) {
		w := view.w
		applied = inventory.ApplyEnchant(w.Menu[:], plan, creative)
		if !applied {
			return
		}
		// vanilla: Player.onEnchantmentPerformed rerolls the seed over the full int
		// range.
		c.xpSeed.Store(int32(c.srv.levelRng.Uint32()))
		enchanted, lapis = w.Menu[0], w.Menu[1]
	})
	if !applied {
		return nil
	}
	c.awardStat(player.Custom(registry.CustomStatEnchantItem), 1)
	c.enchantedItemAdvancements(enchanted, plan.Cost)

	// The client cannot predict the enchantment, so both slots are corrected.
	var sendErr error
	c.player.Inventory(func(inv *player.Inventory) {
		if err := c.sendContainerSlot(inv, view, 0, enchanted); err != nil {
			sendErr = err
			return
		}
		sendErr = c.sendContainerSlot(inv, view, 1, lapis)
	})
	if sendErr != nil {
		return sendErr
	}
	// An enchanted item gets no offers, so costs drop to 0 and hovers hide.
	if err := c.updateEnchantOffers(view); err != nil {
		return err
	}
	// Levels are spent last; vanilla sends the new bar the tick after.
	var levelErr error
	c.player.UpdateVitals(func(v *player.Vitals) {
		rules.GiveExperienceLevels(v, -int(cost))
	})
	levelErr = c.sendExperience()
	if levelErr != nil {
		return levelErr
	}

	// Queued in the table's world after the click's direct writes, so it
	// follows them, where vanilla plays it inside the click before the slot
	// updates. Volume 1, pitch 0.9 to 1.0.
	// vanilla: EnchantmentMenu.clickMenuButton
	sound := soundEffectIDAt(registry.SoundBlockEnchantmentTableUse,
		protocol.SoundCategoryBlock,
		float64(view.w.x)+0.5, float64(view.w.y)+0.5, float64(view.w.z)+0.5,
		1.0, 0.9+0.1*c.srv.soundRng.Float32())
	c.srv.broadcastSoundPacketExcept(view.w.w, c, sound)
	c.sendIn(view.w.w, protocol.IDPlaySoundEffect, sound)
	return nil
}
