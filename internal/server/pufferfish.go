package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

type pufferTouch struct {
	box    entity.AABB
	strike mobStrike
}

// pufferTouchOf is the touch a puffed living pufferfish makes on players.
func (s *Server) pufferTouchOf(w *world.World, m *entity.Mob, ent *mobEntity) (pufferTouch, bool) {
	puff := m.PuffState()
	if m.Type != entity.TypePufferfish || puff == 0 || !m.Alive() {
		return pufferTouch{}, false
	}
	return pufferTouch{
		box: entity.NewAABB(m.BlastBox()),
		strike: mobStrike{
			w: w, mob: m, uuidText: ent.uuidText, killer: s.mobKiller(ent),
			x: m.X, y: m.Y, z: m.Z, damage: float64(1 + puff), touch: true, puff: puff,
		},
	}, true
}

// vanilla: Player.aiStep, TickRateManager.isEntityFrozen
func (s *Server) applyPufferTouches(watchers []trackingWatcher, touches []pufferTouch) {
	if len(touches) == 0 {
		return
	}
	for _, watcher := range watchers {
		c := watcher.c
		if !canPickUp(c) || c.player.GameMode() == protocol.GameModeCreative {
			continue
		}
		reach := c.pickupBox()
		for i := range touches {
			touch := &touches[i]
			if c.theWorld() != touch.strike.w || !reach.Crosses(touch.box) {
				continue
			}
			strike := touch.strike
			strike.victimID = c.player.EntityID
			s.landingBlow(strike, []*client{c})
		}
	}
}

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
