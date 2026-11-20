package server

import (
	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
)

// leashAllowed fires PlayerLeashEntityEvent for the entity (a mob or boat)
// being tied to holderID (the player, a knot or a mob), or taken off with
// unleash, and reports whether a listener left it alone. No mob, boat or knot
// lock may be held.
func (c *client) leashAllowed(entityID, holderID uuid.UUID, unleash bool) bool {
	if !c.pluginListening(pluginruntime.PlayerLeashEntity) {
		return true
	}
	var holder plugin.Entity = c.pluginSelf()
	if holderID != c.player.UUID {
		holder = pluginEntity{id: holderID}
	}
	e := &plugin.PlayerLeashEntityEvent{Player: c.pluginSelf(), Entity: pluginEntity{id: entityID}, Holder: holder, Unleash: unleash}
	return c.pluginFire(e, false) && !e.Cancelled()
}

func (s *Server) listeningKnockback() bool {
	return s.plugins != nil && s.plugins.Has(pluginruntime.EntityKnockback)
}

// knockbackEvent asks EntityKnockbackEvent for a push of victim by the body
// pusher (uuid.Nil for none) and returns the velocity to add, false when a
// listener cancelled it. No lock may be held.
func (s *Server) knockbackEvent(victim plugin.Entity, pusher uuid.UUID, kx, ky, kz float64) (x, y, z float64, ok bool) {
	if !s.listeningKnockback() {
		return kx, ky, kz, true
	}
	e := &plugin.EntityKnockbackEvent{Entity: victim, Knockback: plugin.Vec3{X: kx, Y: ky, Z: kz}}
	if pusher != uuid.Nil {
		e.Pusher = pluginEntity{host: s.pluginAdapter, id: pusher}
	}
	s.firePluginEvent(e)
	return e.Knockback.X, e.Knockback.Y, e.Knockback.Z, !e.Cancelled()
}

// mobKnockback is a mob's push held back while mobsMu is held so
// EntityKnockbackEvent can fire outside the lock.
type mobKnockback struct {
	ent                 *mobEntity
	pusher              uuid.UUID
	oldVX, oldVY, oldVZ float64
	dx, dy, dz          float64
}

// holdMobKnockback captures the velocity change a blow or blast just wrote to
// ent's mob and restores the old velocity until applyMobKnockback fires
// EntityKnockbackEvent outside mobsMu. Returns nil when nobody listens or
// nothing moved. The caller holds mobsMu.
func (s *Server) holdMobKnockback(ent *mobEntity, pusher uuid.UUID, oldVX, oldVY, oldVZ float64, oldImpulse bool) *mobKnockback {
	if !s.listeningKnockback() || ent == nil {
		return nil
	}
	m := ent.mob
	dx, dy, dz := m.VX-oldVX, m.VY-oldVY, m.VZ-oldVZ
	if dx == 0 && dy == 0 && dz == 0 {
		return nil
	}
	m.VX, m.VY, m.VZ = oldVX, oldVY, oldVZ
	m.Impulse = oldImpulse
	return &mobKnockback{ent: ent, pusher: pusher, oldVX: oldVX, oldVY: oldVY, oldVZ: oldVZ, dx: dx, dy: dy, dz: dz}
}

// applyMobKnockback fires EntityKnockbackEvent for a held mob push and lands
// the surviving velocity, returning the mob's resulting velocity. No lock may
// be held.
func (s *Server) applyMobKnockback(kb *mobKnockback) (vx, vy, vz float64) {
	kx, ky, kz, ok := s.knockbackEvent(pluginEntity{id: kb.ent.uuid}, kb.pusher, kb.dx, kb.dy, kb.dz)
	if !ok {
		return kb.oldVX, kb.oldVY, kb.oldVZ
	}
	s.mobsMu.Lock()
	m := kb.ent.mob
	m.VX += kx
	m.VY += ky
	m.VZ += kz
	m.Impulse = true
	vx, vy, vz = m.VX, m.VY, m.VZ
	s.mobsMu.Unlock()
	return vx, vy, vz
}
