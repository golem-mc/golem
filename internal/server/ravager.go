package server

import (
	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// mobBlockBreak is a block a mob broke, applied outside the tick's locks.
type mobBlockBreak struct {
	w       *world.World
	mob     *entity.Mob
	x, y, z int32
	by      entity.MobType
	remove  bool
}

// mobBrokeBlock breaks the block with drops rolled for the breaking mob and
// reports whether the cell changed. A block the mob may not break, gone or
// turned into something else, is left alone.
// vanilla: Level.destroyBlock, Ravager.aiStep
func (s *Server) mobBrokeBlock(b mobBlockBreak) bool {
	old := b.w.BlockAt(b.x, b.y, b.z)
	if b.remove {
		s.setBlockAndTell(b.w, b.x, b.y, b.z, world.RemovalLeaves(old))
		return old != world.RemovalLeaves(old)
	}
	if !entity.MobMayBreak(b.by, old) || !s.mobMayChange(b.w, b.mob, b.x, b.y, b.z, world.RemovalLeaves(old)) {
		return false
	}
	if s.destroyBlockContentsBy(b.w, b.x, b.y, b.z, old, b.by.Name()) {
		s.setBlockAndTell(b.w, b.x, b.y, b.z, world.RemovalLeaves(old))
		s.gameEvent(b.w, rules.GameEventBlockDestroy, float64(b.x)+0.5, float64(b.y)+0.5, float64(b.z)+0.5,
			rules.GameEventContext{Affected: old})
		return true
	}
	return false
}

// mobAct is an entity_action game event a mob posts.
type mobAct struct {
	w       *world.World
	x, y, z float64
	mob     uuid.UUID
}

// mobActed posts the entity_action game event of a ravager's roar.
// vanilla: Ravager.roar
func (s *Server) mobActed(a mobAct) {
	s.gameEvent(a.w, rules.GameEventEntityAction, a.x, a.y, a.z, rules.GameEventContext{Mob: a.mob})
}

// ravagerShielded tells a ravager its blow met a shield: it is stunned or
// throws the defender.
// vanilla: Ravager.blockedByItem
func (s *Server) ravagerShielded(strike mobStrike, victim *client) {
	if strike.mob.Type != entity.TypeRavager {
		return
	}
	s.mobsMu.Lock()
	stunned, handled := strike.mob.RavagerShielded()
	s.mobsMu.Unlock()
	if !handled {
		return
	}
	at := victim.player.Position()
	dx, dz := at.X-strike.x, at.Z-strike.z
	vx, vy, vz := entity.RavagerThrow(dx, dz)
	if stunned {
		s.broadcastVoiceID(strike.w, strike.mob.Type, strike.mob.Silent, strike.x, strike.y, strike.z,
			registry.SoundEntityRavagerStunned, 1, 1)
		s.broadcastWorld(strike.w, protocol.IDPlayEntityEvent,
			entityEventPayload(strike.mob.ID, protocol.EntityStatusRavagerStun))
		vx, vz = entity.RavagerShove(dx, dz)
		vy = 0
	}
	victim.addVelocity(strike.w, vx, vy, vz)
}
