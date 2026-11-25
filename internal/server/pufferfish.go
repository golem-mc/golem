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

// pufferTouchOf is the touch a puffed living pufferfish makes on players; its
// killer is resolved on contact (touchPuffers).
func pufferTouchOf(w *world.World, m *entity.Mob, ent *mobEntity) (pufferTouch, bool) {
	puff := m.PuffState()
	if m.Type != entity.TypePufferfish || puff == 0 || !m.Alive() {
		return pufferTouch{}, false
	}
	return pufferTouch{
		box: entity.NewAABB(m.BlastBox()),
		strike: mobStrike{
			w: w, mob: m, uuidText: ent.uuidText,
			x: m.X, y: m.Y, z: m.Z, damage: float64(1 + puff), touch: true, puff: puff,
		},
	}, true
}

// applyPufferTouches applies the touches to the players of the frozen pass.
// vanilla: Player.aiStep, TickRateManager.isEntityFrozen
func (s *Server) applyPufferTouches(watchers []trackingWatcher, touches []pufferTouch) {
	if len(touches) == 0 {
		return
	}
	for _, watcher := range watchers {
		s.touchPuffers(watcher.c, touches)
	}
}

// applyLivePufferTouches applies the touches of the live mob pass to each
// touched world's clients; scheduleMovement appends one world's touches as a
// run, so each run goes to that world's clients alone.
func (s *Server) applyLivePufferTouches(touches []pufferTouch, inWorld map[*world.World][]*client) {
	for len(touches) > 0 {
		w, n := touches[0].strike.w, 1
		for n < len(touches) && touches[n].strike.w == w {
			n++
		}
		for _, c := range inWorld[w] {
			s.touchPuffers(c, touches[:n])
		}
		touches = touches[n:]
	}
}

// touchPuffers stings c with every touch whose fish box it reaches in its
// world and whose fish stands in a section the pickup query visits.
// vanilla: Player.aiStep, EntitySectionStorage.forEachAccessibleNonEmptySection
func (s *Server) touchPuffers(c *client, touches []pufferTouch) {
	if !canPickUp(c) || c.player.GameMode() == protocol.GameModeCreative {
		return
	}
	reach := c.pickupBox()
	window := entity.NewSectionWindowOf(reach)
	for i := range touches {
		touch := &touches[i]
		if c.theWorld() != touch.strike.w || !reach.Crosses(touch.box) ||
			!window.HoldsAt(touch.strike.x, touch.strike.y, touch.strike.z) {
			continue
		}
		strike := touch.strike
		strike.victimID = c.player.EntityID
		strike.killer, _ = s.mobKillerByID(strike.w, strike.mob.ID)
		s.landingBlow(strike, []*client{c})
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
