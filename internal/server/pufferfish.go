package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// pufferStingPlayer is Pufferfish.playerTouch for a hurt player: the damage
// scales with difficulty (none on Peaceful, so no poison), a landed sting
// tells that player alone (unless the fish is silent) and poisons for 60
// ticks a puff state. The shove is sent after the sting and the poison, whole.
// vanilla: Pufferfish.playerTouch, ServerEntity.sendChanges
func (s *Server) pufferStingPlayer(strike mobStrike, victim *client) {
	cause := &damageCause{entityID: strike.mob.ID, x: strike.x, y: strike.y, z: strike.z, killer: strike.killer, shove: true}
	damage := rules.MobAttackAfterDifficulty(strike.damage, strike.w.Difficulty())
	landed, softened := victim.hurt(damage, rules.DamageTypeMobAttack, cause)
	if !landed {
		return
	}
	if !softened {
		victim.knockbackFromPoint(strike.w, strike.x, strike.y, strike.z, syncVelocityBeforeTravel)
	}
	if !strike.mob.Silent {
		victim.send(protocol.IDPlayGameEvent, gameEventPayload(protocol.GameEventPufferFishSting, 0))
	}
	victim.AddEffect(blowEffect(registry.MobEffectPoison, int32(60*strike.puff), 0))
}
