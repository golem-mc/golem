package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// hoglinCharge tells a hoglin's or zoglin's blow: attack animation and
// sound. vanilla: Hoglin.doHurtTarget, Zoglin.doHurtTarget
func (s *Server) hoglinCharge(strike mobStrike) {
	s.broadcastWorld(strike.w, protocol.IDPlayEntityEvent,
		entityEventPayload(strike.mob.ID, protocol.EntityStatusHoglinAttack))
	name := registry.SoundEntityHoglinAttack
	if strike.hoglin.Zoglin {
		name = registry.SoundEntityZoglinAttack
	}
	s.broadcastVoiceID(strike.w, strike.mob.Type, strike.mob.Silent, strike.x, strike.y, strike.z,
		name, strike.mob.SoundVolume(), strike.mob.VoicePitch())
}

// hoglinThrowPlayer throws a player a landed blow hit.
// vanilla: HoglinBase.throwTarget
func (s *Server) hoglinThrowPlayer(strike mobStrike, c *client) {
	if strike.hoglin == nil || !strike.hoglin.Throw {
		return
	}
	var scale float64
	c.player.Attributes(func(a *player.Attributes) { scale = rules.KnockbackScale(a) })
	at := c.player.Position()
	b := strike.hoglin
	if vx, vy, vz, ok := rules.HoglinThrow(1-scale, at.X-strike.x, at.Z-strike.z, b.Angle, b.H, b.V); ok {
		c.addVelocity(strike.w, vx, vy, vz)
	}
}

// hoglinThrowMob throws a mob a landed blow hit; the caller holds the mob
// lock.
func hoglinThrowMob(strike mobStrike, victim *entity.Mob) {
	if strike.hoglin == nil || !strike.hoglin.Throw {
		return
	}
	b := strike.hoglin
	if vx, vy, vz, ok := rules.HoglinThrow(victim.KnockbackResistance(), victim.X-strike.x, victim.Z-strike.z, b.Angle, b.H, b.V); ok {
		victim.Push(vx, vy, vz)
	}
}

// hoglinConverted spawns the zoglin in the hoglin's place, nauseous for ten
// seconds, with the conversion sound. vanilla: Hoglin.finishConversion
func (s *Server) hoglinConverted(cv mobConversion, zoglin *entity.Mob) {
	ent := convertedEntity(cv.old, zoglin, cv.w, conversionParams{keepEquipment: true})
	zoglin.AddEffect(blowEffect(registry.MobEffectNausea, 200, 0))
	s.addRiddenConversion(cv.old, ent)
	old := cv.old.mob
	s.broadcastVoiceID(cv.w, old.Type, old.Silent, old.X, old.Y, old.Z,
		registry.SoundEntityHoglinConvertedToZombified, old.SoundVolume(), old.VoicePitch())
}
