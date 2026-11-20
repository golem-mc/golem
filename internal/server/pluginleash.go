package server

import (
	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
)

// leashAllowed fires PlayerLeashEntityEvent for the mob mobID being tied to
// holderID (the player itself, or a knot), or taken off with unleash, and
// reports whether a listener left it alone. No mob or knot lock may be held.
func (c *client) leashAllowed(mobID, holderID uuid.UUID, unleash bool) bool {
	if !c.pluginListening(pluginruntime.PlayerLeashEntity) {
		return true
	}
	var holder plugin.Entity = c.pluginSelf()
	if holderID != c.player.UUID {
		holder = pluginEntity{id: holderID}
	}
	e := &plugin.PlayerLeashEntityEvent{Player: c.pluginSelf(), Entity: pluginEntity{id: mobID}, Holder: holder, Unleash: unleash}
	return c.pluginFire(e, false) && !e.Cancelled()
}
