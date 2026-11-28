package server

import (
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// thrownMannequinHit is a flight's blow at a mannequin, applied after the
// tick's locks.
type thrownMannequinHit struct {
	it         *thrownEntity
	ent        *mannequinEntity
	damage     float64
	damageType registry.DamageType
	creative   bool
	heal       skullHeal
}

// thrownMannequinHitOf is the blow a body deals a mannequin: the damage and
// type the body gives a mob.
// vanilla: onHitEntity of each projectile
func thrownMannequinHitOf(tw *thrownWorld, it *thrownEntity, ent *mannequinEntity, ev entity.ThrownEvent, ownerMob bool) thrownMannequinHit {
	t := it.throwable
	owner := tw.bodies.clients[t.OwnerID]
	hit := thrownMannequinHit{it: it, ent: ent, damage: ev.Damage, damageType: rules.DamageTypeThrown, creative: creativeClient(owner)}
	switch {
	case t.Kind.IsFireball():
		hit.damage, hit.damageType = entity.FireballDamage(t.Kind, t.OwnerID != 0, ownerMob || owner != nil)
		hit.heal = skullHealOf(tw, it)
	case t.Kind == entity.ThrownLlamaSpit:
		hit.damage, hit.damageType = entity.LlamaSpitDamage, rules.DamageTypeSpit
	case t.Kind == entity.ThrownShulkerBullet:
		hit.damage, hit.damageType = entity.ShulkerBulletDamage, rules.DamageTypeMobProjectile
	case t.Kind.IsWindCharge():
		hit.damageType = rules.DamageTypeWindCharge
	}
	return hit
}

// thrownHitMannequin lands a flight's hurt on a mannequin: the fire or effect
// the body carries only if the hurt landed, then the shove against the flight.
// vanilla: SmallFireball.onHitEntity, WitherSkull.onHitEntity, ShulkerBullet.onHitEntity
func (s *Server) thrownHitMannequin(hit thrownMannequinHit) {
	it, t := hit.it, hit.it.throwable
	s.mannequinsMu.Lock()
	ent := hit.ent
	if ent.w != it.w || ent.mannequin.Dead || ent.mannequin.Dying() {
		s.mannequinsMu.Unlock()
		return
	}
	m := ent.mannequin
	priorFire := m.FireTicks
	if t.Kind == entity.ThrownSmallFireball {
		m.IgniteForTicksBy(entity.SmallFireballBurnTicks, entity.CombustSource{By: pluginEntity{id: it.uuid}})
	}
	cause := int32(0)
	if t.OwnerID != 0 {
		cause = t.OwnerID + 1
	}
	fx, landed := s.hurtMannequinLocked(ent, hit.damage, hit.damageType, cause, t.ID+1, nil, hit.creative, nil)
	if !landed {
		m.FireTicks = priorFire
		s.mannequinsMu.Unlock()
		return
	}
	if !hit.damageType.InTag(registry.DamageTypeTagIDNoKnockback) {
		m.ArrowKnockback(t.VX, t.VZ, 0, !fx.softened, s.levelRng)
	}
	switch t.Kind {
	case entity.ThrownShulkerBullet:
		m.AddEffect(effect.New(registry.MobEffectLevitation, entity.ShulkerBulletLevitation, 0, false, true, true))
	case entity.ThrownWitherSkull:
		if seconds := entity.WitherSkullEffectSeconds(it.w.Difficulty()); seconds > 0 {
			m.AddEffect(effect.New(registry.MobEffectWither, 20*seconds, 1, false, true, true))
		}
	}
	syncMannequinEffects(ent)
	s.mannequinsMu.Unlock()
	s.tellMannequinHurt(fx)
	if fx.died && t.Kind == entity.ThrownWitherSkull {
		s.healSkullOwner(hit.heal)
	}
}
