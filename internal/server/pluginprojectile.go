package server

import (
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

// launchArrow registers a freshly shot arrow or trident and puts it to
// ProjectileLaunchEvent listeners; false means a listener cancelled it and
// it is already discarded.
func (s *Server) launchArrow(it *arrowEntity) bool {
	s.registerArrow(it)
	return s.projectileLaunched(it.uuid) && s.bodySpawned(it.uuid, plugin.SpawnDefault)
}

// launchThrown is launchArrow for a thrown body, fireball or bullet.
func (s *Server) launchThrown(it *thrownEntity) bool {
	s.registerThrown(it)
	return s.projectileLaunched(it.uuid) && s.bodySpawned(it.uuid, plugin.SpawnDefault)
}

// projectileLaunched fires ProjectileLaunchEvent for a projectile just put in
// the world and discards it on a cancel. It is registered first so the
// handle resolves, so watchers in range see it appear and go in the same
// tick; a listener may change its shooter.
// vanilla: Projectile.spawnProjectile, ProjectileUtil
func (s *Server) projectileLaunched(id uuid.UUID) bool {
	r := s.plugins
	if r == nil || !r.Has(pluginruntime.ProjectileLaunch) {
		return true
	}
	e := &plugin.ProjectileLaunchEvent{Projectile: pendingProjectile{id: id}}
	s.firePluginEvent(e)
	if !e.Cancelled() {
		return true
	}
	bindPluginEntity(pluginEntity{id: id, host: s.pluginAdapter}, s.pluginAdapter).Remove()
	return false
}
