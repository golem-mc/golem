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

// markState is a moveMark plus the pass that last saw the body, so a pass
// prunes stale marks without a second map, and whether a plugin put the
// body back.
type markState struct {
	moveMark
	gen  uint32
	held bool
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
// last pass. A mob with another teleport still to be told is not a move;
// one a plugin put back is, so a cancel holds on every tick.
// vanilla: ServerEntity.sendChanges
func (t *mobDimTick) noteMoves(fx *mobEffects) {
	h := t.h
	if t.s.plugins == nil || !t.s.plugins.Has(pluginruntime.EntityMove) {
		h.moveMarks = nil
		return
	}
	if h.moveMarks == nil {
		h.moveMarks = make(map[*entity.Mob]markState, len(h.herd))
	}
	h.moveGen++
	for _, m := range h.herd {
		now := moveMark{m.X, m.Y, m.Z, m.Yaw, m.Pitch}
		st, ok := h.moveMarks[m]
		next := markState{moveMark: now, gen: h.moveGen}
		if !ok || st.moveMark == now {
			if ok {
				next.held = st.held
			}
			h.moveMarks[m] = next
			continue
		}
		ent := h.record(m)
		if ent != nil {
			next.held = st.held && ent.teleported
		}
		h.moveMarks[m] = next
		if ent == nil || ent.teleported && !st.held {
			continue
		}
		fx.moves = append(fx.moves, mobMove{w: t.w, h: h, m: m, id: h.MobUUID(m), from: st.moveMark, to: now})
	}
	if len(h.moveMarks) > len(h.herd) {
		for m, st := range h.moveMarks {
			if st.gen != h.moveGen {
				delete(h.moveMarks, m)
			}
		}
	}
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
		s.teleportMob(ent, player.Position{X: mv.from.x, Y: mv.from.y, Z: mv.from.z}, mv.from.yaw, mv.from.pitch, "plugin", nil)
		s.mobsMu.Lock()
		if mv.h.moveMarks != nil {
			mv.h.moveMarks[mv.m] = markState{moveMark: mv.from, gen: mv.h.moveGen, held: true}
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
	if s.vehicleMarks == nil {
		s.vehicleMarks = make(map[uuid.UUID]markState)
	}
	s.vehicleGen++
	var moves []vehicleMove
	note := func(w *world.World, id uuid.UUID, now moveMark) {
		if was, ok := s.vehicleMarks[id]; ok && was.moveMark != now {
			moves = append(moves, vehicleMove{w: w, id: id, from: was.moveMark, to: now})
		}
		s.vehicleMarks[id] = markState{moveMark: now, gen: s.vehicleGen}
	}
	n := 0
	s.lockTick(&s.boatsMu)
	for _, it := range s.boats {
		if !it.boat.Dead {
			n++
			note(it.w, it.uuid, moveMark{it.boat.X, it.boat.Y, it.boat.Z, it.boat.Yaw, 0})
		}
	}
	s.unlockTick(&s.boatsMu)
	s.lockTick(&s.minecartsMu)
	for _, it := range s.minecarts {
		if !it.cart.Dead {
			n++
			note(it.w, it.uuid, moveMark{it.cart.X, it.cart.Y, it.cart.Z, it.cart.Yaw, 0})
		}
	}
	s.unlockTick(&s.minecartsMu)
	if len(s.vehicleMarks) > n {
		for id, st := range s.vehicleMarks {
			if st.gen != s.vehicleGen {
				delete(s.vehicleMarks, id)
			}
		}
	}
	for _, mv := range moves {
		s.plugins.Fire(&plugin.VehicleMoveEvent{
			Vehicle: pendingVehicle{id: mv.id},
			From:    mv.from.location(s, mv.w), To: mv.to.location(s, mv.w),
		})
	}
}
