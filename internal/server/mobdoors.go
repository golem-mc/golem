package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
)

// The door level events a breaking zombie plays.
// vanilla: LevelEvent.SOUND_ZOMBIE_WOODEN_DOOR (1019), SOUND_ZOMBIE_BREAKS_WOODEN_DOOR (1021),
// PARTICLES_DESTROY_BLOCK (2001)
const (
	levelEventZombieAttackDoor = 1019
	levelEventZombieBreakDoor  = 1021
	levelEventDestroyBlock     = 2001
)

// mobDoorOp is a door change a goal queued, with the world it is in.
type mobDoorOp struct {
	w  *world.World
	op entity.DoorOp
}

// mobDoorWork applies a door change outside the tick's locks, skipping a
// door changed since the goal queued it.
// vanilla: DoorInteractGoal.setOpen, BreakDoorGoal.tick
func (s *Server) mobDoorWork(d mobDoorOp) {
	w, op := d.w, d.op
	switch op.Kind {
	case entity.DoorOpOpen, entity.DoorOpClose:
		state := w.BlockAt(op.X, op.Y, op.Z)
		if !entity.DoorIsWooden(state) || entity.DoorIsOpen(state) == (op.Kind == entity.DoorOpOpen) {
			return
		}
		// UseBlock toggles both halves and plays the sound and game event.
		_, changes, effects, _ := s.physicsFor(w).UseBlock(op.X, op.Y, op.Z, "")
		for _, e := range effects {
			s.tellEffect(w, e)
		}
		s.tellBlockChanges(w, changes)
	case entity.DoorOpCrack:
		s.broadcastLevelEvent(w, levelEventZombieAttackDoor, op.X, op.Y, op.Z, 0)
		if op.Swing {
			s.broadcastNear(w, float64(op.X), float64(op.Y), float64(op.Z), levelEventRange,
				protocol.IDPlaySwingAnimation, mobSwingArm(op.Mob.ID))
		}
	case entity.DoorOpProgress:
		var b protocol.Buffer
		b.PutVarInt(op.Mob.ID)
		b.PutPosition(op.X, op.Y, op.Z)
		b.PutU8(uint8(op.Stage))
		s.broadcastNear(w, float64(op.X), float64(op.Y), float64(op.Z),
			rules.DestroyStageRange, protocol.IDPlayBlockDestruction, b.Bytes())
	case entity.DoorOpBreak:
		old := w.BlockAt(op.X, op.Y, op.Z)
		if !entity.DoorIsWooden(old) {
			return
		}
		if !s.mobMayBreakDoor(w, op) {
			return
		}
		s.setBlockAndTell(w, op.X, op.Y, op.Z, 0)
		s.broadcastLevelEvent(w, levelEventZombieBreakDoor, op.X, op.Y, op.Z, 0)
		// vanilla reads the state after the removal, so the event names air.
		s.broadcastLevelEvent(w, levelEventDestroyBlock, op.X, op.Y, op.Z, 0)
	}
}

// mobBedOp is a bed's occupancy a sleeping or waking villager changed.
type mobBedOp struct {
	w  *world.World
	op entity.BedOp
}

// mobBedWork applies a bed change outside the tick's locks.
// vanilla: LivingEntity.startSleeping, stopSleeping
func (s *Server) mobBedWork(b mobBedOp) {
	s.setBedOccupied(b.w, b.op.X, b.op.Y, b.op.Z, b.op.Occupied)
}

// mobMayBreakDoor puts a zombie's finished door break to EntityBreakDoorEvent
// listeners and reports whether the door may go.
// vanilla: BreakDoorGoal.tick
func (s *Server) mobMayBreakDoor(w *world.World, op entity.DoorOp) bool {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.EntityBreakDoor) {
		return true
	}
	ent := s.mobEntityOf(op.Mob)
	if ent == nil {
		return true
	}
	e := &plugin.EntityBreakDoorEvent{
		Entity: pendingMob{id: ent.uuid},
		Block:  plugin.Block{World: pluginWorld{srv: s, dim: s.dimensionOfWorld(w), host: s.pluginAdapter}, Pos: plugin.BlockPos{X: int(op.X), Y: int(op.Y), Z: int(op.Z)}},
	}
	s.firePluginEvent(e)
	return !e.Cancelled()
}
