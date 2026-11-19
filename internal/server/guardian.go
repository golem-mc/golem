package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// elderCurse is an elder guardian's curse come due: its place and whether
// it is silent.
type elderCurse struct {
	w       *world.World
	x, y, z float64
	silent  bool
}

// guardianBeam lands a finished beam's magic damage, difficulty-scaled like
// the blow after it. vanilla: Guardian.GuardianAttackGoal.tick
func (s *Server) guardianBeam(strike mobStrike, victim *client) {
	scaled := rules.ScaledWithDifficulty(strike.beam, s.world.Difficulty())
	landed, softened := victim.hurt(scaled, rules.DamageTypeIndirectMagic, &damageCause{
		entityID: strike.mob.ID,
		x:        strike.x, y: strike.y, z: strike.z,
		killer: strike.killer, weapon: strike.mob.MainHand,
	})
	if landed && !softened {
		victim.knockbackFromPoint(strike.w, strike.x, strike.y, strike.z, syncVelocityAfterTravel)
	}
}

// guardianBeamOnMob lands a guardian's beam on a mob target; the blow that
// follows it is landingMobBlow's.
// vanilla: Guardian.GuardianAttackGoal.tick
func (s *Server) guardianBeamOnMob(strike mobStrike) {
	s.mobsMu.Lock()
	var victim *entity.Mob
	for _, m := range s.herdOf(strike.w).herd {
		if m.ID == strike.victimID {
			victim = m
			break
		}
	}
	s.mobsMu.Unlock()
	if victim != nil {
		s.hurtMobBy(strike.w, victim, strike.mob, strike.beam, rules.DamageTypeIndirectMagic)
	}
}

// guardianThornsOnMob pricks a mob that struck a guardian with spikes out.
// vanilla: Guardian.hurtServer
func (s *Server) guardianThornsOnMob(w *world.World, attacker, guardian *entity.Mob) {
	s.hurtMobBy(w, attacker, guardian, entity.GuardianThornsDamage(), rules.DamageTypeThorns)
}

// hurtMobBy hurts m with source behind it, knocking it back and recording
// the attacker, and tells the damage event, motion, health and hurt sound.
func (s *Server) hurtMobBy(w *world.World, m, source *entity.Mob, amount float64, damageType registry.DamageType) {
	s.hurtMobByThen(w, m, source, amount, damageType, nil)
}

// hurtMobByThen applies a kind's additional push before collecting hurt packets.
func (s *Server) hurtMobByThen(w *world.World, m, source *entity.Mob, amount float64, damageType registry.DamageType, after func()) {
	s.mobsMu.Lock()
	ent := s.herdOf(w).book[m]
	if ent == nil || m.Dead {
		s.mobsMu.Unlock()
		return
	}
	landed := m.HurtByMobAs(amount, damageType, source)
	var health []byte
	var sends []mobSend
	if landed {
		if after != nil {
			after()
		}
		health = ent.syncHealth()
		cause := causeEntityID(&damageCause{entityID: source.ID})
		sends = mobHurtToldBy(w, m, damageType, cause, cause, nil)
		sends = append(sends, mobSend{w: w, id: protocol.IDPlaySetEntityMotion,
			data: entityVelocityPayload(m.ID, m.VX, m.VY, m.VZ)})
	}
	s.mobsMu.Unlock()
	if !landed {
		return
	}
	s.broadcastWorld(w, protocol.IDPlayEntityMetadata, health)
	s.sendMobSends(sends)
}

// guardianPrick is a guardian whose spikes were out when a sweep reached it.
type guardianPrick struct {
	id      int32
	x, y, z float64
	killer  *rules.Killer
}

// guardianThorns pricks the player who struck a guardian with its spikes out.
// vanilla: Guardian.hurtServer
func (s *Server) guardianThorns(c *client, id int32, x, y, z float64, killer *rules.Killer) {
	scaled := rules.ScaledWithDifficulty(entity.GuardianThornsDamage(), s.world.Difficulty())
	landed, softened := c.hurt(scaled, rules.DamageTypeThorns, &damageCause{
		entityID: id, x: x, y: y, z: z, killer: killer, shove: true,
	})
	if landed && !softened {
		c.knockbackFromPoint(c.theWorld(), x, y, z, syncVelocityNow)
		at := c.player.Position()
		s.broadcastNear(c.theWorld(), at.X, at.Y, at.Z, soundRange(1), protocol.IDPlaySoundEffect,
			soundEffectIDAt(thornsHitSound, protocol.SoundCategoryPlayer, at.X, at.Y, at.Z, 1, 1))
	}
}

// elderGuardianCurse gives mining fatigue to players around an elder
// guardian and shows each the apparition.
// vanilla: ElderGuardian.customServerAiStep, MobEffectUtil.addEffectToPlayersAround
func (s *Server) elderGuardianCurse(curse elderCurse, clients []*client) {
	const fatigue = registry.MobEffectMiningFatigue
	for _, c := range clients {
		if c.theWorld() != curse.w {
			continue
		}
		var have *effect.Instance
		for _, in := range c.player.Effects() {
			if in.ID == fatigue {
				have = &in
				break
			}
		}
		at := c.player.Position()
		if !rules.ElderCurseApplies(c.player.GameMode(), entity.Dist2(curse.x, curse.y, curse.z, at.X, at.Y, at.Z),
			entity.ElderCurseRadius, have, entity.ElderCurseAmp, entity.ElderCurseDisplay) {
			continue
		}
		c.AddEffect(blowEffect(registry.MobEffectMiningFatigue, entity.ElderCurseDuration, entity.ElderCurseAmp))
		value := float32(1)
		if curse.silent {
			value = 0
		}
		c.send(protocol.IDPlayGameEvent, gameEventPayload(protocol.GameEventGuardianElderEffect, value))
	}
}
