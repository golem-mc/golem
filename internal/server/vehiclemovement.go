package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// vehicleMovement carries a vibration past the vehicle lock: listeners can
// acquire entity locks, and the source is the vehicle rather than its riders.
type vehicleMovement struct {
	w      *world.World
	source uuid.UUID
	event  entity.MovementEvent
}

func (s *Server) emitVehicleMovement(v vehicleMovement) {
	if v.event.Kind == registry.GameEventNone {
		return
	}
	s.gameEvent(v.w, v.event.Kind, v.event.X, v.event.Y, v.event.Z, rules.GameEventContext{Mob: v.source, Affected: v.event.BlockState})
}

type vehicleMovements []vehicleMovement

func (fx *vehicleMovements) add(w *world.World, source uuid.UUID, event entity.MovementEvent) {
	if event.Kind != registry.GameEventNone {
		*fx = append(*fx, vehicleMovement{w: w, source: source, event: event})
	}
}

func (fx vehicleMovements) apply(s *Server) {
	applyEach(s, fx, (*Server).emitVehicleMovement)
}
