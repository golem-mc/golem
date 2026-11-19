package server

import (
	"maps"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

// The merchant window: Open Screen, contents, Merchant Offers, and Select
// Trade, which fills the payment slots. Slots, result and take are
// inventory/merchant.go's.
// vanilla: ServerPlayer.openMenu, ServerPlayer.sendMerchantOffers,
// ServerGamePacketListenerImpl.handleSelectTrade

// merchant is a merchant window's counterpart: offers a take updates in
// place and what the window shows. One player trades with it at a time.
type merchant struct {
	title protocol.Text
	// ent is the villager or trader; nil for a merchant with no body.
	ent        *mobEntity
	offers     []inventory.Offer
	level, xp  int32
	regular    bool
	canRestock bool

	// persist writes offer state to the owner and rewards it for the trades
	// just made, in order; release frees it for the next player. Both are optional and take the owner's lock, so they
	// run outside the player's inventory lock.
	persist func(offers, traded []inventory.Offer)
	// tradeUpdated is told whether the result slot holds an offer after each
	// recompute; it takes the owner's lock.
	tradeUpdated func(valid bool)
	release      func()
	// refresh returns the owner's current offers (without special prices),
	// level and experience; it takes the owner's lock.
	refresh func() ([]inventory.Offer, int32, int32)

	// stillValid reports whether a player with eyes at (x, y, z) in w may keep
	// trading; nil keeps the window until closed.
	stillValid func(w *world.World, rng, x, y, z float64) bool
}

// openMerchant puts the merchant's window in front of the player.
func (c *client) openMerchant(m *merchant) error {
	if err := c.closeCurrentWindow(false); err != nil {
		return err
	}
	window := &openWindow{kind: windowMerchant, w: c.theWorld(), trader: m}
	window.Offers = m.offers
	c.player.Inventory(func(*player.Inventory) {
		window.id = c.container.click.OpenWindow()
	})
	c.setOpenWindow(window)

	view := windowView{kind: window.kind, id: window.id, w: window, srv: c.srv}
	if err := c.openScreen(view); err != nil {
		return err
	}
	var err error
	c.player.Inventory(func(inv *player.Inventory) {
		if err = c.sendContainerContent(view, inv); err != nil {
			return
		}
		if len(m.offers) == 0 {
			// vanilla: Merchant.openTradingScreen
			return
		}
		err = c.writePacket(protocol.IDPlayMerchantOffers, "merchant offers", merchantOffersPayload(window.id, m))
	})
	return err
}

// merchantOffersPayload is the Merchant Offers packet; the result is a slot.
// vanilla: ClientboundMerchantOffersPacket, MerchantOffer.writeToStream
func merchantOffersPayload(windowID int32, m *merchant) []byte {
	var b protocol.Buffer
	b.PutVarInt(windowID)
	b.PutVarInt(int32(len(m.offers)))
	for _, o := range m.offers {
		putItemCost(&b, o.CostA)
		b.PutSlot(o.Result)
		b.PutBool(o.HasCostB())
		if o.HasCostB() {
			putItemCost(&b, o.CostB)
		}
		b.PutBool(o.OutOfStock())
		b.PutI32(o.Uses)
		b.PutI32(o.MaxUses)
		b.PutI32(o.XP)
		b.PutI32(o.SpecialPrice)
		b.PutF32(o.PriceMultiplier)
		b.PutI32(o.Demand)
	}
	b.PutVarInt(m.level)
	b.PutVarInt(m.xp)
	b.PutBool(m.regular)
	b.PutBool(m.canRestock)
	return b.Bytes()
}

func putItemCost(b *protocol.Buffer, cost inventory.Cost) {
	b.PutVarInt(cost.ItemID)
	b.PutVarInt(cost.Count)
	cost.PutPredicate(b)
}

// handleSelectTrade fills the payment slots with the offer's costs from the
// player's inventory and tells the player what moved.
// vanilla: ServerGamePacketListenerImpl.handleSelectTrade
func (c *client) handleSelectTrade(r *protocol.Reader) error {
	index := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return c.decodeFailure("select_trade", err)
	}
	view := c.currentView()
	if view.kind != windowMerchant || view.w == nil {
		return nil
	}
	var outcome inventory.Outcome
	var resultValid bool
	slots := map[int32]protocol.ItemStack{}
	c.player.Inventory(func(inv *player.Inventory) {
		view.w.Hint = index
		click := view.clickView(inv)
		if inventory.RecomputeMerchant(click.Menu, click.Offers, click.Hint) {
			outcome.Announce = append(outcome.Announce, inventory.MerchantResultSlot)
		}
		inventory.SelectTrade(click, inv, index, &outcome)
		view.w.SlotsChanged(view.kind, inv, &outcome, false)
		resultValid = !view.w.Menu[inventory.MerchantResultSlot].Empty()
		for _, slot := range outcome.Announce {
			if slot >= 0 && slot < view.slots() {
				slots[slot] = view.stack(inv, slot)
			}
		}
	})
	ordered := slices.Sorted(maps.Keys(slots))
	var sendErr error
	c.player.Inventory(func(inv *player.Inventory) {
		for _, slot := range ordered {
			if sendErr = c.sendContainerSlot(inv, view, slot, slots[slot]); sendErr != nil {
				return
			}
		}
	})
	if sendErr != nil {
		return sendErr
	}
	if outcome.MerchantUpdated && view.w.trader.tradeUpdated != nil {
		view.w.trader.tradeUpdated(resultValid)
	}
	if outcome.Visible {
		c.broadcastEquipment()
	}
	return nil
}

// resendMerchantOffers sends a villager's trading player its offers again
// after a restock; the window's offers keep their special prices.
// vanilla: Villager.resendOffersToTradingPlayer
func (s *Server) resendMerchantOffers(ent *mobEntity) {
	s.mobsMu.Lock()
	resend := ent.resendOffers
	s.mobsMu.Unlock()
	if resend != nil {
		resend()
	}
}

// resendOffers replaces the window's offers with the owner's current ones,
// keeping the window's special prices.
func (c *client) resendOffers(mm *merchant) {
	c.replaceOffers(mm, false, func(fresh []inventory.Offer) {
		for i := range fresh {
			if i < len(mm.offers) {
				fresh[i].SpecialPrice = mm.offers[i].SpecialPrice
			}
		}
	})
}

// repriceOffers replaces the window's offers with the owner's current ones,
// priced by priceOffers, and recomputes the result slot.
func (c *client) repriceOffers(mm *merchant, reputation int) {
	hero, heroed := c.player.Effect(heroOfTheVillage)
	c.replaceOffers(mm, true, func(fresh []inventory.Offer) {
		priceOffers(fresh, reputation, hero, heroed)
	})
}

// priceOffers sets each offer's special price to the discount for the
// player's reputation and Hero of the Village (hero, when heroed), dropping
// any it had before.
// vanilla: Villager.updateSpecialPrices, Villager.resetSpecialPrices
func priceOffers(offers []inventory.Offer, reputation int, hero effect.Instance, heroed bool) {
	for i := range offers {
		offers[i].SpecialPrice = 0
	}
	if reputation != 0 {
		inventory.ReputationDiscount(offers, reputation)
	}
	if heroed {
		inventory.HeroDiscount(offers, hero.Amplifier)
	}
}

// replaceOffers sends the window the owner's current offers, priced by price
// under the player's inventory lock; updateSell recomputes the result slot.
func (c *client) replaceOffers(mm *merchant, updateSell bool, price func([]inventory.Offer)) {
	fresh, level, xp := mm.refresh()
	if len(fresh) == 0 {
		return
	}
	view := c.currentView()
	if view.kind != windowMerchant || view.w == nil || view.w.trader != mm {
		return
	}
	c.player.Inventory(func(inv *player.Inventory) {
		price(fresh)
		mm.offers, mm.level, mm.xp = fresh, level, xp
		view.w.Offers = fresh
		// A failed write surfaces on the player's next packet.
		_ = c.writePacket(protocol.IDPlayMerchantOffers, "merchant offers", merchantOffersPayload(view.id, mm))
		// vanilla: MerchantMenu.updateSellItem
		if click := view.clickView(inv); updateSell && inventory.RecomputeMerchant(click.Menu, click.Offers, click.Hint) {
			_ = c.sendContainerSlot(inv, view, inventory.MerchantResultSlot, view.stack(inv, inventory.MerchantResultSlot))
		}
	})
}
