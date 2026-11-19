package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func (c *client) enchantListening(id int) bool {
	return c.srv.plugins != nil && c.srv.plugins.Has(id)
}

// enchantTable is the table's block as a plugin sees it.
func (c *client) enchantTable(view windowView) plugin.Block {
	w := pluginWorld{srv: c.srv, dim: c.srv.dimensionOfWorld(view.w.w), host: c.srv.pluginAdapter}
	return plugin.Block{World: w, Pos: plugin.BlockPos{X: int(view.w.x), Y: int(view.w.y), Z: int(view.w.z)}}
}

// pluginPrepareEnchant remakes the offers, fires PrepareItemEnchantEvent
// with no lock held and stores what the listeners left as the window's
// offers. A cancelled event leaves the table with no offers.
func (c *client) pluginPrepareEnchant(view windowView) {
	var item protocol.ItemStack
	var shelves int
	var props [protocol.EnchantPropertyCount]int32
	c.player.Inventory(func(*player.Inventory) {
		item = view.w.Menu[0]
		shelves = rules.CountBookshelves(c.theWorld(), view.w.x, view.w.y, view.w.z)
		props = inventory.EnchantProps(c.xpSeed.Load(), shelves, item)
	})
	e := &plugin.PrepareItemEnchantEvent{Player: pluginPlayer{host: c.srv.pluginAdapter, c: c},
		Block: c.enchantTable(view), Item: pluginStack(item), Bookshelf: shelves}
	for i := range e.Offers {
		e.Offers[i] = plugin.EnchantOffer{Enchantment: registry.Enchantment(props[protocol.EnchantPropertyEnchant0+i]),
			Level: int(props[protocol.EnchantPropertyLevel0+i]), Cost: int(props[i])}
	}
	c.srv.plugins.Call(func() { c.srv.plugins.Fire(e) })
	if e.Cancelled() {
		props = inventory.EnchantProps(c.xpSeed.Load(), 0, protocol.ItemStack{})
	} else {
		for i, o := range e.Offers {
			props[i] = int32(o.Cost)
			props[protocol.EnchantPropertyEnchant0+i] = int32(o.Enchantment)
			props[protocol.EnchantPropertyLevel0+i] = int32(o.Level)
		}
	}
	c.player.Inventory(func(*player.Inventory) { view.w.Props = props })
}

// pluginEnchantPress fires EnchantItemEvent for a planned press with no lock
// held and returns the plan the listeners left; false when cancelled or when
// the edited cost is out of the player's reach.
func (c *client) pluginEnchantPress(view windowView, plan inventory.EnchantPlan, button, level int32, creative bool) (inventory.EnchantPlan, bool) {
	var item protocol.ItemStack
	c.player.Inventory(func(*player.Inventory) { item = view.w.Menu[0] })
	e := &plugin.EnchantItemEvent{Player: pluginPlayer{host: c.srv.pluginAdapter, c: c}, Block: c.enchantTable(view),
		Item: pluginStack(item), Enchantments: make(map[plugin.Enchantment]int, len(plan.Offers)),
		Cost: int(plan.Required), Button: int(button)}
	for _, o := range plan.Offers {
		e.Enchantments[registry.Enchantment(o.Enchantment)] = int(o.Level)
	}
	c.srv.plugins.Call(func() { c.srv.plugins.Fire(e) })
	if e.Cancelled() || (!creative && int(level) < e.Cost) {
		return plan, false
	}
	plan.Required = int32(e.Cost)
	plan.Offers = plan.Offers[:0:0]
	for id, lv := range e.Enchantments {
		plan.Offers = append(plan.Offers, rules.EnchOffer{Enchantment: int32(id), Level: int32(lv)})
	}
	slices.SortFunc(plan.Offers, func(a, b rules.EnchOffer) int { return int(a.Enchantment - b.Enchantment) })
	return plan, len(plan.Offers) != 0
}
