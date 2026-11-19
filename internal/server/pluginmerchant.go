package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
)

// pluginTrade fires PlayerTradeEvent for a click that takes the merchant
// window's result, before the click runs, and reports whether a listener
// cancelled it (the click then does nothing and the window is resent).
// vanilla: MerchantResultSlot.onTake
func (c *client) pluginTrade(cv windowView, slot int32, mode int32) (cancelled bool) {
	r := c.srv.plugins
	if r == nil || cv.kind != windowMerchant || slot != inventory.MerchantResultSlot || !r.Has(pluginruntime.PlayerTrade) ||
		mode == protocol.ClickModeClone || mode == protocol.ClickModeCollect || cv.w == nil || cv.w.trader == nil || cv.w.trader.ent == nil {
		return false
	}
	trader := cv.w.trader
	var result protocol.ItemStack
	cv.inventory(c, func(inv *player.Inventory) {
		cv.use(func() { result = cv.stack(inv, slot) })
	})
	if result.Empty() {
		return false
	}
	var offer inventory.Offer
	found := false
	cv.inventory(c, func(*player.Inventory) {
		for _, o := range trader.offers {
			if !o.OutOfStock() && o.Result.Equal(result) {
				offer, found = o, true
				break
			}
		}
	})
	if !found {
		return false
	}
	r.Call(func() {
		e := &plugin.PlayerTradeEvent{
			Player:   pluginPlayer{host: c.srv.pluginAdapter, c: c},
			Merchant: pluginLivingHandle(trader.ent.uuid),
			Trade:    pluginTradeOf(offer),
		}
		r.Fire(e)
		cancelled = e.Cancelled()
	})
	return cancelled
}

// pluginTradeOf is an offer as the plugin API shows it.
func pluginTradeOf(o inventory.Offer) plugin.Trade {
	cost := func(c inventory.Cost, count int32) plugin.ItemStack {
		if c.Count <= 0 {
			return plugin.ItemStack{}
		}
		return pluginStack(protocol.ItemStack{ItemID: c.ItemID, Count: count})
	}
	return plugin.Trade{
		Cost: cost(o.CostA, o.PricedCountA()), Cost2: cost(o.CostB, o.CostB.Count), Result: pluginStack(o.Result),
		Uses: int(o.Uses), MaxUses: int(o.MaxUses), XP: int(o.XP), PriceMultiplier: o.PriceMultiplier,
		Demand: int(o.Demand), SpecialPrice: int(o.SpecialPrice), RewardsExperience: o.RewardExp,
	}
}
