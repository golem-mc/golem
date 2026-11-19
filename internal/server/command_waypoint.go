package server

import (
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// WaypointIcon is the icon the player's waypoint is drawn with.
func (c *client) WaypointIcon() player.LocatorIcon { return c.player.LocatorIcon() }

// SetWaypointIcon changes the icon and re-tracks the waypoint for every
// viewer. vanilla: WaypointCommand.mutateIcon
func (c *client) SetWaypointIcon(icon player.LocatorIcon) {
	c.player.SetLocatorIcon(icon)
	c.retrackWaypoints()
}

// CarriesWaypoint is true: a player always has a waypoint.
func (c *client) CarriesWaypoint() bool { return true }

// TransmitsWaypoint reports whether the transmit range is above zero, which
// puts the waypoint on the world's list.
// vanilla: LivingEntity.isTransmittingWaypoint
func (c *client) TransmitsWaypoint() bool {
	var transmit float64
	c.player.Attributes(func(a *player.Attributes) {
		transmit = a.Value(registry.AttributeWaypointTransmitRange)
	})
	return transmit > 0
}

// CarriesWaypoint is true for a mob, the living body whose waypoint /waypoint
// modify edits.
func (e *entitySnapshot) CarriesWaypoint() bool {
	_, ok := e.ref.(*mobEntity)
	return ok
}

// WaypointIcon is the icon the mob's waypoint is drawn with.
func (e *entitySnapshot) WaypointIcon() player.LocatorIcon {
	ref, ok := e.ref.(*mobEntity)
	if !ok {
		return player.LocatorIcon{}
	}
	e.srv.mobsMu.Lock()
	defer e.srv.mobsMu.Unlock()
	return ref.locatorIcon
}

// SetWaypointIcon stores the icon; the next locator bar pass retracks the
// waypoint for the receivers holding it.
func (e *entitySnapshot) SetWaypointIcon(icon player.LocatorIcon) {
	if ref, ok := e.ref.(*mobEntity); ok {
		e.srv.mobsMu.Lock()
		ref.locatorIcon = icon
		e.srv.mobsMu.Unlock()
	}
}

// TransmitsWaypoint reports whether the mob's transmit range is above zero.
// vanilla: LivingEntity.isTransmittingWaypoint
func (e *entitySnapshot) TransmitsWaypoint() bool {
	ref, ok := e.ref.(*mobEntity)
	if !ok {
		return false
	}
	e.srv.mobsMu.Lock()
	defer e.srv.mobsMu.Unlock()
	a := ref.mob.Attributes()
	return a != nil && a.Value(registry.AttributeWaypointTransmitRange) > 0
}
