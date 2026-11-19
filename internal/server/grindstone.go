package server

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// The grindstone window's own part: the take's level event at the
// grindstone. The take, cleared inputs and orbs are
// inventory/grindstone.go's and containers.go's.
// vanilla: GrindstoneMenu$3.onTake

// grindstoneEventAt plays the take's event at the grindstone for the taker
// and everyone near.
func (c *client) grindstoneEventAt(view windowView) error {
	if view.w.virtual {
		return nil
	}
	payload := worldEvent(protocol.WorldEventGrindstoneUsed, view.w.x, view.w.y, view.w.z, 0)
	c.sendIn(view.w.w, protocol.IDPlayWorldEvent, payload)
	c.srv.broadcastNearExcept(view.w.w, c, float64(view.w.x), float64(view.w.y), float64(view.w.z), levelEventRange,
		protocol.IDPlayWorldEvent, payload)
	return nil
}
