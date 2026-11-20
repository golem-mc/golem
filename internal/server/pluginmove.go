package server

import (
	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
)

// moveMark is where a body stood when its move event last fired or was
// skipped; the next pass fires when the body stands elsewhere.
type moveMark struct {
	x, y, z    float64
	yaw, pitch float32
}

func (m moveMark) location(s *Server, w *world.World) plugin.Location {
	return plugin.Location{
		World: pluginWorld{srv: s, dim: s.dimensionOfWorld(w), host: s.pluginAdapter},
		Pos:   plugin.Vec3{X: m.x, Y: m.y, Z: m.z}, Yaw: m.yaw, Pitch: m.pitch,
	}
}

// mobMove is a mob that moved over the tick, noted under the mob lock and
// fired by mobEffects.apply.
type mobMove struct {
	w        *world.World
	h        *mobHerd
	m        *entity.Mob
	id       uuid.UUID
	from, to moveMark
}

// noteMoves records the mobs whose position or rotation changed since the
// last pass. A mob with a teleport still to be told is not a move.
// vanilla: ServerEntity.sendChanges
func (t *mobDimTick) noteMoves(fx *mobEffects) {
	if t.s.plugins == nil || !t.s.plugins.Has(pluginruntime.EntityMove) {
		t.h.moveMarks = nil
		return
	}
	next := make(map[*entity.Mob]moveMark, len(t.h.herd))
	for _, m := range t.h.herd {
		now := moveMark{m.X, m.Y, m.Z, m.Yaw, m.Pitch}
		next[m] = now
		was, ok := t.h.moveMarks[m]
		if !ok || was == now {
			continue
		}
		if ent := t.h.record(m); ent == nil || ent.teleported {
			continue
		}
		fx.moves = append(fx.moves, mobMove{w: t.w, h: t.h, m: m, id: t.h.MobUUID(m), from: was, to: now})
	}
	t.h.moveMarks = next
}

// fireMobMoves fires EntityMoveEvent for each noted move; a cancelled one
// puts the mob back at its start.
func (s *Server) fireMobMoves(moves []mobMove) {
	for _, mv := range moves {
		e := &plugin.EntityMoveEvent{
			Entity: pluginLivingHandle(mv.id),
			From:   mv.from.location(s, mv.w), To: mv.to.location(s, mv.w),
		}
		s.plugins.Fire(e)
		if !e.Cancelled() {
			continue
		}
		s.mobsMu.Lock()
		ent := mv.h.record(mv.m)
		s.mobsMu.Unlock()
		if ent == nil {
			continue
		}
		s.teleportMob(ent, player.Position{X: mv.from.x, Y: mv.from.y, Z: mv.from.z}, mv.from.yaw, mv.from.pitch, "plugin")
		s.mobsMu.Lock()
		if mv.h.moveMarks != nil {
			mv.h.moveMarks[mv.m] = mv.from
		}
		s.mobsMu.Unlock()
	}
}

type vehicleMove struct {
	w        *world.World
	id       uuid.UUID
	from, to moveMark
}

// fireVehicleMoves fires VehicleMoveEvent for each boat and minecart that
// stands elsewhere than at the last pass, outside the vehicle locks.
func (s *Server) fireVehicleMoves() {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.VehicleMove) {
		s.vehicleMarks = nil
		return
	}
	next := make(map[uuid.UUID]moveMark, len(s.vehicleMarks))
	var moves []vehicleMove
	note := func(w *world.World, id uuid.UUID, now moveMark) {
		next[id] = now
		if was, ok := s.vehicleMarks[id]; ok && was != now {
			moves = append(moves, vehicleMove{w: w, id: id, from: was, to: now})
		}
	}
	s.lockTick(&s.boatsMu)
	for _, it := range s.boats {
		if !it.boat.Dead {
			note(it.w, it.uuid, moveMark{it.boat.X, it.boat.Y, it.boat.Z, it.boat.Yaw, 0})
		}
	}
	s.unlockTick(&s.boatsMu)
	s.lockTick(&s.minecartsMu)
	for _, it := range s.minecarts {
		if !it.cart.Dead {
			note(it.w, it.uuid, moveMark{it.cart.X, it.cart.Y, it.cart.Z, it.cart.Yaw, 0})
		}
	}
	s.unlockTick(&s.minecartsMu)
	s.vehicleMarks = next
	for _, mv := range moves {
		s.plugins.Fire(&plugin.VehicleMoveEvent{
			Vehicle: pendingVehicle{id: mv.id},
			From:    mv.from.location(s, mv.w), To: mv.to.location(s, mv.w),
		})
	}
}
