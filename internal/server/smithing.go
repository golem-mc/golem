package server

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// The smithing table window: template, base and addition inputs and the
// result a recipe makes, kept in the window's menu field like the anvil's.
// Clicks, takes and recomputes with the error state are package
// inventory's (inventory/smithing.go, inventory/window.go); this sends the
// error state and the take event.
// vanilla: SmithingMenu, SmithingTableBlock

// SmithingRecipeErrorProperty is the smithing window's one property:
// whether the inputs form a combination no recipe takes, drawn as a cross
// over the empty result. vanilla: SmithingMenu.hasRecipeError
const SmithingRecipeErrorProperty = 0

// sendSmithingError sends the error state when it changed.
// vanilla: AbstractContainerMenu.broadcastChanges' data-slot send
func (c *client) sendSmithingError(view windowView) error {
	if view.kind != windowSmithing || view.w == nil {
		return nil
	}
	state, moved := view.w.SmithingErrorMoved()
	if !moved {
		return nil
	}
	return c.sendContainerProperty(view, SmithingRecipeErrorProperty, state)
}

// smithingTakeEvent plays the table's use event at the table once per click
// however many takes, for the taker and everyone near.
// vanilla: SmithingMenu.onTake, level.levelEvent(1044, pos, 0)
func (c *client) smithingTakeEvent(view windowView) error {
	if view.w.virtual {
		return nil
	}
	payload := worldEvent(protocol.WorldEventSmithingUse, view.w.x, view.w.y, view.w.z, 0)
	c.sendIn(view.w.w, protocol.IDPlayWorldEvent, payload)
	c.srv.broadcastNearExcept(view.w.w, c, float64(view.w.x), float64(view.w.y), float64(view.w.z), levelEventRange,
		protocol.IDPlayWorldEvent, payload)
	return nil
}
