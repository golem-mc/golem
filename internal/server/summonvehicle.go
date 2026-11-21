package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

// registerSummonedVehicle loads a boat's or cart's nested mob riders and shows
// every body before sending the links between them.
// vanilla: SummonCommand.createEntity, EntityType.loadPassengersRecursive
func (s *Server) registerSummonedVehicle(w *world.World, compound nbt.Compound, seat vehicleSeat, reason plugin.SpawnReason) error {
	var woken wakeResult
	var identity uuid.UUID
	var x, y, z float64
	if seat.boat != nil {
		it := seat.boat
		identity, x, y, z = it.uuid, it.boat.X, it.boat.Y, it.boat.Z
		woken.boats = append(woken.boats, it)
	} else {
		it := seat.cart
		identity, x, y, z = it.uuid, it.cart.X, it.cart.Y, it.cart.Z
		woken.minecarts = append(woken.minecarts, it)
	}
	if s.relatedByUUID(identity) != nil {
		return command.ErrDuplicateUUID
	}
	giveRidersUUIDs(compound)
	known := map[uuid.UUID]bool{identity: true}
	woken.collectTeams(compound, known)
	carried := s.herdOf(w).standVehicleRiders(s, compound, known, seat, &woken)
	for _, ent := range woken.mobs {
		if s.summonDuplicate(ent.uuid) || s.relatedByUUID(ent.uuid) != nil {
			return command.ErrDuplicateUUID
		}
		// The command's post-load processor applies to every nested body.
		ent.mob.SetPos(x, y, z)
	}
	if seat.boat != nil {
		seat.boat.carriedPassengers = carried
	} else {
		seat.cart.carriedPassengers = carried
	}
	// Keep mob links pending during registration: pairing a linked parent
	// would name its riders before their spawn packets reached the client.
	s.registerWake(woken)
	var packets []outbound
	s.mobsMu.Lock()
	linkMobRides(woken.mobs)
	for _, ent := range woken.mobs {
		if ids := ent.mob.PassengerIDs(); len(ids) > 0 {
			packets = append(packets, outbound{id: protocol.IDPlaySetPassengers,
				data: setPassengersPayload(ent.mob.ID, ids)})
		}
	}
	s.mobsMu.Unlock()
	for _, packet := range packets {
		s.broadcastWorld(w, packet.id, packet.data)
	}
	// The command's own summon asks only after the riders are in, as for a mob.
	s.bodySpawned(identity, reason)
	return nil
}
