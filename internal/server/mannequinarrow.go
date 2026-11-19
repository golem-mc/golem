package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
)

type arrowMannequinHit struct {
	ent      *mannequinEntity
	arrow    *arrowEntity
	event    entity.ArrowEvent
	creative bool
}

// arrowMannequinOutcome lands the shaft before adding its tip's effects.
// vanilla: AbstractArrow.onHitEntity, Arrow.doPostHurtEffects
func (s *Server) arrowMannequinOutcome(hit arrowMannequinHit) arrowOutcome {
	it, ev := hit.arrow, hit.event
	s.mannequinsMu.Lock()
	ent := hit.ent
	if ent == nil || ent.w != it.w || ent.mannequin.Dead || ent.mannequin.Dying() {
		s.mannequinsMu.Unlock()
		return arrowFliesOn
	}
	m := ent.mannequin
	priorFire := m.FireTicks
	if ev.IgniteTicks > 0 {
		m.IgniteForTicks(int(ev.IgniteTicks))
	}
	typ, direct := rules.DamageTypeArrow, int32(protocol.EntityTypeArrow)
	if it.arrow.Trident {
		typ, direct = rules.DamageTypeTrident, protocol.EntityTypeTrident
	}
	cause := int32(0)
	if it.arrow.OwnerID != 0 {
		cause = it.arrow.OwnerID + 1
	}
	hurt, landed := s.hurtMannequinLocked(ent, ev.Damage, typ, cause, it.arrow.ID+1, nil, hit.creative, nil)
	if !landed {
		m.FireTicks = priorFire
		s.mannequinsMu.Unlock()
		return refusedOutcome(it)
	}
	punch := rules.EnchantedKnockback(it.arrow.Weapon, &rules.EnchContext{
		DamageType: typ, Victim: protocol.EntityTypeMannequin, DirectAttacker: direct,
	}, 0)
	m.ArrowKnockback(ev.VX, ev.VZ, punch, !hurt.softened, s.levelRng)
	if !it.arrow.Trident && !it.piercingHit {
		m.ArrowCount++
	}
	s.mannequinsMu.Unlock()
	s.tellMannequinHurt(hurt)
	attacker := attackBody{}
	ctx := &rules.EnchContext{DamageType: typ, Victim: protocol.EntityTypeMannequin, Attacker: -1, DirectAttacker: direct}
	if c := s.entityClient(it.arrow.OwnerID); c != nil {
		attacker.player, ctx.Attacker = c, protocol.EntityTypePlayer
	} else if mob := s.mobByEntityID(it.w, it.arrow.OwnerID); mob != nil {
		attacker.mob, ctx.Attacker = mob.mob, mobEntityType(mob.mob.Type)
	}
	weapon := it.arrow.Weapon
	if attacker.player == nil && attacker.mob == nil {
		weapon = protocol.ItemStack{}
	}
	s.postAttack(it.w, attacker, attackBody{mannequin: ent}, weapon, ctx, syncVelocityAfterTravel)
	s.mannequinsMu.Lock()
	for _, in := range arrowPostEffects(it) {
		m.AddEffect(in)
	}
	packets, watchers := mannequinEffectPackets(ent), trackedWatchers(ent.tracked)
	s.mannequinsMu.Unlock()
	s.tellWatchers(it.w, watchers, packets)
	s.arrowsMu.Lock()
	killed := it.killedByArrow(hurt.died, mannequinEntityID)
	s.arrowsMu.Unlock()
	s.arrowHitAdvancements(s.entityClient(it.arrow.OwnerID), arrowTypeName(it), killed, it.arrow.Weapon.ItemID)
	return landedOutcome(it)
}
