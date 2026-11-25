package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

type villagerSignal struct {
	w     *world.World
	event entity.MobEvent
	// mob is the signalling villager's identity, read under the herd lock.
	mob uuid.UUID
}

// villagersHearBell runs after block effects, including redstone rings.
// vanilla: BellBlockEntity.updateEntities
func (s *Server) villagersHearBell(w *world.World, x, y, z int32) {
	gameTime, _ := s.world.Time()
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, m := range s.dimensionOfWorld(w).mobs.herd {
		m.HearBell(x, y, z, gameTime)
	}
}

// applyVillagerSignal runs with the mob lock released: rings can wake nearby
// villagers, and rockets take the firework lock.
func (s *Server) applyVillagerSignal(signal villagerSignal) {
	e := signal.event
	if e.Kind == entity.MobRangBell {
		x, y, z := int32(e.X), int32(e.Y), int32(e.Z)
		state := signal.w.BlockAt(x, y, z)
		if registry.BlockOf(state) != registry.BlockBell {
			return
		}
		s.ringBell(signal.w, x, y, z, uint8(registry.StateFacing(state)), rules.GameEventContext{}, nil)
	} else {
		s.spawnFirework(signal.w, e.Stack, e.X, e.Y, e.Z, e.Mob.ID, signal.mob, 0)
	}
}
