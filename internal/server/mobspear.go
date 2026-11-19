package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// landingStab lands a mob's kinetic-weapon contact on a player or a mob: the
// damage when the weapon's damage condition held, then the extra knockback and
// the dismount. The mob swings nothing: the item is in use.
// A mob victim takes the damage as a plain mob blow, not as the spear's damage type.
// vanilla: LivingEntity.stabAttack
func (s *Server) landingStab(strike mobStrike, victim *client) {
	stab := strike.stab
	if victim == nil {
		s.landingMobStab(strike)
		return
	}
	affected := false
	if stab.DealsDamage {
		if scaled := rules.MobAttackAfterDifficulty(strike.damage, s.world.Difficulty()); scaled > 0 {
			affected, _ = victim.hurt(scaled, registry.DamageTypeSpear, &damageCause{
				entityID: strike.mob.ID, x: strike.x, y: strike.y, z: strike.z, killer: strike.killer, weapon: strike.mob.MainHand,
				shove: shovesOnBlock(strike.mob.Type),
			})
		}
	}
	if stab.Knockback {
		victim.knockbackFromPointExtraOnly(strike.w, float64(strike.mob.Yaw), rules.StabKnockback, syncVelocityAfterTravel)
	}
	if stab.Dismount {
		victim.StopRiding()
	}
	if affected {
		s.postAttack(strike.w, attackBody{mob: strike.mob}, attackBody{player: victim}, strike.mob.MainHand, &rules.EnchContext{
			DamageType: registry.DamageTypeSpear, Direct: true,
			Victim: protocol.EntityTypePlayer, Attacker: strike.mob.TypeID(), DirectAttacker: strike.mob.TypeID(),
		}, syncVelocityAfterTravel)
	}
	if affected || stab.Knockback || stab.Dismount {
		s.stabEvent(strike)
	}
}

// stabEvent tells the clients the attacker's contact affected something, which
// plays the spear's hit sound.
// vanilla: KineticWeapon.damageEntities, LivingEntity.onKineticHit
func (s *Server) stabEvent(strike mobStrike) {
	s.broadcastWorld(strike.w, protocol.IDPlayEntityEvent, entityEventPayload(strike.mob.ID, protocol.EntityStatusKineticHit))
}

func (s *Server) landingMobStab(strike mobStrike) {
	stab := strike.stab
	if stab.DealsDamage {
		s.landingMobBlow(strike)
	}
	if !stab.Knockback && !stab.Dismount {
		if stab.DealsDamage {
			s.stabEvent(strike)
		}
		return
	}
	s.mobsMu.Lock()
	h := s.herdOf(strike.w)
	var victim *entity.Mob
	for _, m := range h.herd {
		if m.ID == strike.victimID {
			victim = m
			break
		}
	}
	if victim == nil || victim.Dead {
		s.mobsMu.Unlock()
		return
	}
	defer s.stabEvent(strike)
	if stab.Knockback {
		victim.KnockbackAlong(float64(strike.mob.Yaw), rules.StabKnockback)
	}
	vx, vy, vz := victim.VX, victim.VY, victim.VZ
	var seat *seatNews
	if stab.Dismount {
		seat = dismountSeat(h.book[victim])
	}
	s.mobsMu.Unlock()
	if stab.Knockback {
		s.broadcastWorld(strike.w, protocol.IDPlaySetEntityMotion, entityVelocityPayload(strike.victimID, vx, vy, vz))
	}
	if seat != nil {
		s.sendSeatNews([]seatNews{*seat})
	}
}

// dismountSeat takes a mob off its vehicle, returning the vehicle's new
// passenger list to send, nil when nothing changed. Caller holds mobsMu.
func dismountSeat(ent *mobEntity) *seatNews {
	if ent == nil || ent.mob.Vehicle == nil {
		return nil
	}
	veh := ent.mob.Vehicle
	if !entity.DismountMob(ent.w, veh, ent.mob) {
		return nil
	}
	n := mobSeatNews(ent, veh, rules.GameEventEntityDismount)
	return &n
}

// dismountStabbed is a kinetic contact's dismount of a mob, reporting whether it
// left a vehicle.
func (s *Server) dismountStabbed(ent *mobEntity) bool {
	s.mobsMu.Lock()
	seat := dismountSeat(ent)
	s.mobsMu.Unlock()
	if seat != nil {
		s.sendSeatNews([]seatNews{*seat})
	}
	return seat != nil
}
