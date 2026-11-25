package server

import (
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

// launchBody puts a freshly shot projectile to ProjectileLaunchEvent and
// EntitySpawnEvent listeners and publishes it; with neither listened to it
// is registered at once. A listener sees the body through plugin handles while
// watchers do not; only an accepted one is shown. after runs once the body is
// shown. false means a listener cancelled or removed it, and it is dropped.
// vanilla: Projectile.spawnProjectile
func launchBody[B any, S any](s *Server, k *trackedKind[B, S], it B, after func()) bool {
	if !s.launchObserved() {
		k.register(s, it)
		after()
		return true
	}
	k.hold(s, it)
	id := k.uuid(it)
	if !s.projectileLaunched(id) || !k.stillHeld(s, it) || !s.bodySpawnAllowed(id, plugin.SpawnDefault) || !k.publish(s, it) {
		k.discard(s, it)
		return false
	}
	after()
	return true
}

// launchObserved reports whether a plugin listens to a projectile's launch or
// spawn.
func (s *Server) launchObserved() bool {
	r := s.plugins
	return r != nil && (r.Has(pluginruntime.ProjectileLaunch) || r.Has(pluginruntime.EntitySpawn))
}

// launchArrow launches a freshly shot arrow or trident.
func (s *Server) launchArrow(it *arrowEntity) bool {
	arrowFirstTick(it)
	return launchBody(s, &arrowTracker, it, func() {})
}

// launchThrown is launchArrow for a thrown body, fireball or bullet.
func (s *Server) launchThrown(it *thrownEntity) bool {
	s.resolveBulletTarget(it)
	return launchBody(s, &thrownTracker, it, func() { s.thrownRegistered(it) })
}

// launchFirework is launchArrow for a firework rocket.
func (s *Server) launchFirework(it *fireworkEntity) bool {
	return launchBody(s, &fireworkTracker, it, func() {})
}

// projectileLaunched fires ProjectileLaunchEvent for a held projectile and
// reports whether it was left uncancelled; a listener may change its shooter.
// vanilla: Projectile.spawnProjectile, ProjectileUtil
func (s *Server) projectileLaunched(id uuid.UUID) bool {
	r := s.plugins
	if r == nil || !r.Has(pluginruntime.ProjectileLaunch) {
		return true
	}
	e := &plugin.ProjectileLaunchEvent{Projectile: pendingProjectile{id: id}}
	s.firePluginEvent(e)
	return !e.Cancelled()
}
