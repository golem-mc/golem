package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// boatTarget is a chest boat as a transfer target; its lock nests inside
// a block container's lock and outside a hopper cart's lock.
// vanilla: HopperBlockEntity.getEntityContainer, ContainerEntity.getChestVehicleItem
type boatTarget struct {
	s  *Server
	it *boatEntity
}

func (t boatTarget) use(_ *world.World, move containerMove) bool {
	t.s.boatsMu.Lock()
	defer t.s.boatsMu.Unlock()
	if t.it.boat.Dead {
		return false
	}
	t.s.unpackBoatLoot(t.it, lootOpener{})
	return cachedVehicleMove(move, boatCargo{it: t.it}, t.it.cargoVersion)
}

func (t boatTarget) entity() *world.BlockEntity { return nil }
func (t boatTarget) sync(s *Server, _ *world.World) {
	s.syncVehicleViewers(func(w *openWindow) bool { return w.boat == t.it }, func() []protocol.ItemStack {
		s.boatsMu.Lock()
		defer s.boatsMu.Unlock()
		return slices.Clone(t.it.cargo)
	})
}
func (t boatTarget) settle(*Server, *world.World) {}
