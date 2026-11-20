package server

import (
	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// pendingSplash is a body a splash potion reached whose instances wait for
// PotionSplashEvent: a mob by entity ID and UUID, or a player's client.
type pendingSplash struct {
	mob   int32
	id    uuid.UUID
	c     *client
	scale float64
	src   instantSource
}

// pluginHandle is the plugin's handle on the mob or player, nil when the
// body is gone.
func (p pendingSplash) pluginHandle(s *Server) plugin.LivingEntity {
	if p.c != nil {
		return pluginPlayer{host: s.pluginAdapter, c: p.c}
	}
	if body, ok := s.pluginAdapter.entityByUUID(p.id, ""); ok {
		l, _ := s.pluginAdapter.pluginEntityOf(body).(plugin.LivingEntity)
		return l
	}
	return nil
}

// resolveSplash fires PotionSplashEvent for an effect splash and lands the
// instances on the bodies it left in, at the intensity it set. No lock may
// be held.
// vanilla: ThrownSplashPotion.onHitAsPotion
func (s *Server) resolveSplash(br thrownBreak) {
	shl := br.potion
	if !shl.splash || !s.plugins.Has(pluginruntime.PotionSplash) {
		return
	}
	pending := shl.pending
	shl.pending = nil
	handles := make([]plugin.LivingEntity, len(pending))
	affected := make(map[plugin.LivingEntity]float64, len(pending))
	for i, p := range pending {
		if handles[i] = p.pluginHandle(s); handles[i] != nil {
			affected[handles[i]] = p.scale
		}
	}
	e := &plugin.PotionSplashEvent{Potion: potionHandle(br), Item: pluginStack(shl.stack), Affected: affected}
	s.firePluginEvent(e)
	if e.Cancelled() {
		return
	}
	dim := s.dimensionOfWorld(br.w)
	for i, p := range pending {
		scale, ok := e.Affected[handles[i]]
		if handles[i] == nil || !ok || scale <= 0 {
			continue
		}
		landing := splashLanding(shl.brewed, scale, shl.durationScale)
		if len(landing) == 0 {
			continue
		}
		if p.c != nil {
			if !p.c.player.Vitals().Dead {
				shl.players = append(shl.players, splashPlayerTell{c: p.c, effects: landing, scale: scale, src: p.src})
			}
			continue
		}
		s.mobsMu.Lock()
		if m := dim.mobs.byUUID[p.id]; m != nil && m.Alive() {
			hits, changes := landMobEffects(dim, br.w, m, landing, scale, br.x, br.z, p.src)
			shl.instantHurts = append(shl.instantHurts, hits...)
			if tell, ok := mobEffectTell(dim, m, changes); ok {
				shl.mobs = append(shl.mobs, tell)
			}
		}
		s.mobsMu.Unlock()
	}
}

// lingeringAllowed fires LingeringPotionSplashEvent and reports whether the
// cloud may be left.
func (s *Server) lingeringAllowed(br thrownBreak) bool {
	if !s.plugins.Has(pluginruntime.LingeringPotionSplash) {
		return true
	}
	e := &plugin.LingeringPotionSplashEvent{Potion: potionHandle(br), Item: pluginStack(br.potion.stack)}
	s.firePluginEvent(e)
	return !e.Cancelled()
}

// pluginPotion is the handle of a broken potion: the body is gone by the
// time its event fires, so it answers its type, and its Shooter is the
// thrower if one still stands in a world.
type pluginPotion struct {
	pluginRemoved
	owner uuid.UUID
}

func potionHandle(br thrownBreak) pluginPotion {
	typ := registry.EntityTypeSplashPotion
	if br.kind == entity.ThrownLingeringPotion {
		typ = registry.EntityTypeLingeringPotion
	}
	return pluginPotion{pluginRemoved: pluginRemoved{pluginEntity: pluginEntity{id: br.uuid}, typ: typ}, owner: br.owner}
}

func (p pluginPotion) Shooter() (plugin.Entity, bool) {
	if p.owner == uuid.Nil || p.host == nil {
		return nil, false
	}
	body, ok := p.host.entityByUUID(p.owner, "")
	if !ok {
		return nil, false
	}
	return p.host.pluginEntityOf(body), true
}

func (p pluginPotion) SetShooter(plugin.Entity) {}
