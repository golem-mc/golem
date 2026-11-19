package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
)

// The stonecutter window: input and result slots and one property, the
// selection. Take, shift-click and pick are package inventory's
// (inventory/stonecutter.go, inventory/window.go); this sends them.
// vanilla: StonecutterMenu, StonecutterBlock

// StonecutterSelectedProperty is the stonecutter's one property: the
// selected recipe for its input, indexed in the join's Update Recipes
// order, -1 for none. vanilla: StonecutterMenu.selectedRecipeIndex
const StonecutterSelectedProperty = 0

// handleStonecutterButton handles a recipe pick, sending the result slot
// and selection when it took. vanilla: StonecutterMenu.clickMenuButton
func (c *client) handleStonecutterButton(view windowView, button int32) error {
	var result protocol.ItemStack
	picked := false
	c.player.Inventory(func(*player.Inventory) {
		if view.w != nil {
			result, picked = view.w.PickStonecutter(button)
		}
	})
	if !picked {
		return nil
	}
	// Result slot before property, vanilla's broadcast order.
	var sendErr error
	c.player.Inventory(func(inv *player.Inventory) {
		sendErr = c.sendContainerSlot(inv, view, inventory.StonecutterResultSlot, result)
	})
	if sendErr != nil {
		return sendErr
	}
	return c.sendStonecutterSelected(view)
}

// sendStonecutterSelected sends the selection when it changed.
func (c *client) sendStonecutterSelected(view windowView) error {
	if view.kind != windowStonecutter || view.w == nil {
		return nil
	}
	selected, moved := view.w.StonecutterSelectionMoved()
	return c.sendWindowSelection(view, StonecutterSelectedProperty, selected, moved)
}
