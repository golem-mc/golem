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

// knockbackEvent asks EntityKnockbackEvent for a push of victim by the body
// pusher (uuid.Nil for none) and returns the velocity to add, false when a
// listener cancelled it. No lock may be held.
func (s *Server) knockbackEvent(victim plugin.Entity, pusher uuid.UUID, kx, ky, kz float64) (x, y, z float64, ok bool) {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.EntityKnockback) {
		return kx, ky, kz, true
	}
	e := &plugin.EntityKnockbackEvent{Entity: victim, Knockback: plugin.Vec3{X: kx, Y: ky, Z: kz}}
	if pusher != uuid.Nil {
		e.Pusher = pluginEntity{host: s.pluginAdapter, id: pusher}
	}
	s.firePluginEvent(e)
	return e.Knockback.X, e.Knockback.Y, e.Knockback.Z, !e.Cancelled()
}
