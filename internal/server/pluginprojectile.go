package server

import (
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

// launchBody files a freshly shot projectile where plugin handles resolve it
// and puts it to ProjectileLaunchEvent and EntitySpawnEvent listeners; only
// an accepted one is published, so watchers never see a refused one. false
// means a listener cancelled it and it is already dropped.
// vanilla: Projectile.spawnProjectile
func launchBody[B any, S any](s *Server, k *trackedKind[B, S], it B, publish func()) bool {
	k.hold(s, it)
	id := k.uuid(it)
	if !s.projectileLaunched(id) || !s.bodySpawnAllowed(id, plugin.SpawnDefault) {
		k.drop(s, it)
		return false
	}
	publish()
	return true
}

// launchArrow launches a freshly shot arrow or trident.
func (s *Server) launchArrow(it *arrowEntity) bool {
	return launchBody(s, &arrowTracker, it, func() {
		arrowFirstTick(it)
		arrowTracker.publish(s, it)
	})
}

// launchThrown is launchArrow for a thrown body, fireball or bullet.
func (s *Server) launchThrown(it *thrownEntity) bool {
	s.resolveBulletTarget(it)
	return launchBody(s, &thrownTracker, it, func() {
		thrownTracker.publish(s, it)
		s.thrownRegistered(it)
	})
}

// launchFirework is launchArrow for a firework rocket.
func (s *Server) launchFirework(it *fireworkEntity) bool {
	return launchBody(s, &fireworkTracker, it, func() { fireworkTracker.publish(s, it) })
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
