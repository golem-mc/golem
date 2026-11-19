package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
)

// resultSlots is each window's result slot, the one a take fires
// CraftItemEvent for.
var resultSlots = map[windowKind]int32{
	windowPlayer: 0, windowCrafting: 0, windowAnvil: inventory.AnvilResultSlot,
	windowSmithing: inventory.SmithingResultSlot, windowGrindstone: inventory.GrindstoneResultSlot,
	windowStonecutter: inventory.StonecutterResultSlot, windowLoom: inventory.LoomResultSlot,
}

// pluginPrepare fires PrepareItemCraftEvent once a click or drag has changed
// a crafting grid's or an anvil's inputs, after the click ran and with no
// lock held. A listener's result replaces the shown one and the changed
// result slot is returned for the caller to announce. A grid's result
// holds for that grid's contents only, so a shift-click crafting more than
// once shows the listener's result for the first craft.
// vanilla: CraftingMenu.slotChangedCraftingGrid, AnvilMenu.createResult
func (c *client) pluginPrepare(cv windowView, announced []int32) (changed []int32) {
	r := c.srv.plugins
	if r == nil || !r.Has(pluginruntime.PrepareCraft) {
		return nil
	}
	switch cv.kind {
	case windowPlayer, windowCrafting:
		if len(announced) == 0 {
			return nil
		}
		return c.prepareGrid(r, cv)
	case windowAnvil:
		return c.prepareResult(r, cv, announced, inventory.AnvilResultSlot)
	case windowSmithing:
		return c.prepareResult(r, cv, announced, inventory.SmithingResultSlot)
	case windowGrindstone:
		return c.prepareResult(r, cv, announced, inventory.GrindstoneResultSlot)
	}
	return nil
}

func (c *client) prepareGrid(r *pluginruntime.Runtime, cv windowView) []int32 {
	var cells []protocol.ItemStack
	var width int32
	var id string
	var result protocol.ItemStack
	cv.inventory(c, func(inv *player.Inventory) {
		cv.use(func() {
			grid, w, ok := cv.clickView(inv).GridOf(inv)
			if !ok {
				return
			}
			cells, width = append([]protocol.ItemStack(nil), grid...), w
			inv.CraftOverride = nil
			id, result = inv.CraftMatch(cells, width)
		})
	})
	if cells == nil {
		return nil
	}
	changed := false
	r.Call(func() {
		e := &plugin.PrepareItemCraftEvent{Player: pluginPlayer{host: c.srv.pluginAdapter, c: c}, View: c.pluginViewOf(cv),
			Result: pluginStack(result)}
		if id != "" {
			e.Recipe, _ = registryRecipe(id)
		}
		r.Fire(e)
		if st, ok := pluginProtocolStack(e.Result); ok && !sameCount(st, result) {
			result, changed = st, true
		}
	})
	if !changed {
		return nil
	}
	cv.inventory(c, func(inv *player.Inventory) {
		inv.CraftOverride = &player.CraftOverride{Cells: cells, Width: width, Result: result}
	})
	return []int32{0}
}

// prepareResult fires the event for an anvil, smithing table or grindstone
// whose inputs (any slot up to resultSlot) were announced; only the anvil has
// a cost.
func (c *client) prepareResult(r *pluginruntime.Runtime, cv windowView, announced []int32, resultSlot int32) []int32 {
	if !slices.ContainsFunc(announced, func(slot int32) bool { return slot <= resultSlot }) {
		return nil
	}
	var result protocol.ItemStack
	var cost int
	cv.inventory(c, func(*player.Inventory) {
		result = cv.w.Menu[resultSlot]
		if cv.kind == windowAnvil {
			cost = int(cv.w.Cost)
		}
	})
	changed := false
	r.Call(func() {
		e := &plugin.PrepareItemCraftEvent{Player: pluginPlayer{host: c.srv.pluginAdapter, c: c}, View: c.pluginViewOf(cv),
			Result: pluginStack(result), Cost: cost}
		r.Fire(e)
		if st, ok := pluginProtocolStack(e.Result); ok && !sameCount(st, result) {
			result, changed = st, true
		}
		if cv.kind == windowAnvil && e.Cost != cost {
			cost, changed = e.Cost, true
		}
	})
	if !changed {
		return nil
	}
	cv.inventory(c, func(*player.Inventory) {
		cv.w.Menu[resultSlot] = result
		if cv.kind == windowAnvil {
			cv.w.Cost = int32(cost)
		}
	})
	return []int32{resultSlot}
}

// pluginCraft fires CraftItemEvent for a take of a window's result slot and
// reports whether a listener cancelled it.
// vanilla: ResultSlot.onTake
func (c *client) pluginCraft(cv windowView, result protocol.ItemStack, id string, shift bool) (cancelled bool) {
	r := c.srv.plugins
	if r == nil || !r.Has(pluginruntime.CraftItem) {
		return false
	}
	r.Call(func() {
		e := &plugin.CraftItemEvent{Player: pluginPlayer{host: c.srv.pluginAdapter, c: c}, View: c.pluginViewOf(cv),
			Result: pluginStack(result), Shift: shift}
		if id != "" {
			e.Recipe, _ = registryRecipe(id)
		}
		r.Fire(e)
		cancelled = e.Cancelled()
	})
	return cancelled
}

// pluginFurnaceExtract answers the hook that fires FurnaceExtractEvent for a
// furnace result take, nil when nobody listens so the payout keeps its
// per-recipe orbs. The hook returns the experience to pay.
// vanilla: AbstractFurnaceBlockEntity.awardUsedRecipesAndPopExperience
func (c *client) pluginFurnaceExtract(cv windowView) func(protocol.ItemStack, int) int {
	if !c.pluginListening(pluginruntime.FurnaceExtract) || cv.be == nil {
		return nil
	}
	return func(item protocol.ItemStack, xp int) int {
		e := &plugin.FurnaceExtractEvent{Player: c.pluginSelf(), Block: c.pluginBlockAt(cv.be.X, cv.be.Y, cv.be.Z),
			Item: pluginStack(item), Experience: xp}
		c.pluginFire(e, false)
		return max(e.Experience, 0)
	}
}
