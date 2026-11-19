package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
)

// The loom window: banner, dye and pattern inputs, the result, and one
// property, the selected pattern. Take and pick are package inventory's
// (inventory/loom.go); this sends them.
// vanilla: LoomMenu, LoomBlock

// LoomSelectedProperty is the loom's one property: the selected pattern's
// index among those offered, -1 for none.
// vanilla: LoomMenu.selectedBannerPatternIndex
const LoomSelectedProperty = 0

// handleLoomButton handles a pattern pick, sending the result slot if it
// changed and the selection if it moved. vanilla: LoomMenu.clickMenuButton
func (c *client) handleLoomButton(view windowView, button int32) error {
	var result protocol.ItemStack
	var changed, picked bool
	c.player.Inventory(func(*player.Inventory) {
		if view.w != nil {
			result, changed, picked = view.w.PickLoom(button)
		}
	})
	if !picked {
		return nil
	}
	if changed {
		var sendErr error
		c.player.Inventory(func(inv *player.Inventory) {
			sendErr = c.sendContainerSlot(inv, view, inventory.LoomResultSlot, result)
		})
		if sendErr != nil {
			return sendErr
		}
	}
	return c.sendLoomSelected(view)
}

// sendLoomSelected sends the selection when it changed.
func (c *client) sendLoomSelected(view windowView) error {
	if view.kind != windowLoom || view.w == nil {
		return nil
	}
	var selected int32
	var moved bool
	c.player.Inventory(func(*player.Inventory) { selected, moved = view.w.LoomSelectionMoved() })
	return c.sendWindowSelection(view, LoomSelectedProperty, selected, moved)
}
