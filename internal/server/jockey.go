package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Jockeys: finalizeSpawn's rolls land on the mob as an entity.JockeyKind and
// are resolved here. The second body spawns like any arrival, the pair links
// under the mob lock and Set Passengers goes to the dimension; later viewers
// get the link in the pairing.

// resolveJockey spawns what the finalize rolled for ent: a spider's
// skeleton rider, a baby zombie's chicken or a husk's camel husk. The chicken gets
// setChickenJockey's mark: no eggs, and monster despawn rules.
// vanilla: Spider.finalizeSpawn, Zombie.finalizeSpawn
func (s *Server) resolveJockey(dim *dimension, ent *mobEntity) {
	m := ent.mob
	jockey := m.Jockey
	m.Jockey = entity.JockeyNone
	switch jockey {
	case entity.JockeyRider:
		s.linkMobPassengers(ent, s.standMobArriving(dim, m.RiderKind, m.X, m.Y, m.Z,
			0, nil, false, "", true, s.jockeyPrep(m.RiderKind)))
	case entity.JockeyBuiltRider:
		rider := m.Rider
		m.Rider = nil
		rider.ID = s.nextMobEntity(rider.Type)
		rider.OnGround = spawnStandsOnGround(dim.w, rider.X, rider.Y, rider.Z)
		riderEnt := &mobEntity{mob: rider, w: dim.w, uuid: uuid.New()}
		s.addMob(riderEnt)
		s.linkMobPassengers(ent, riderEnt)
	case entity.JockeyZombieNautilus, entity.JockeyStructureNautilus:
		vehicle := s.standMobArriving(dim, entity.TypeZombieNautilus, m.X, m.Y, m.Z,
			0, nil, false, "", true, func(e *mobEntity) {
				e.mob.Yaw = m.Yaw
				e.mob.Persistence = jockey == entity.JockeyStructureNautilus
			})
		s.linkMobPassengers(vehicle, ent)
	case entity.JockeyFreshChicken:
		// The chicken is finalized as a lone arrival and marked after, since its
		// rolls ignore the flag. vanilla: Zombie.finalizeSpawn's setChickenJockey
		vehicle := s.standMobArriving(dim, entity.TypeChicken, m.X, m.Y, m.Z,
			0, nil, false, "", true, nil)
		vehicle.mob.ChickenJockey = true
		s.linkMobPassengers(vehicle, ent)
	case entity.JockeyCamelHusk:
		vehicle := s.standMobArriving(dim, entity.TypeCamelHusk, m.X, m.Y, m.Z,
			0, nil, false, "", true, nil)
		s.linkMobPassengers(vehicle, ent)
		parched := s.standMobArriving(dim, entity.TypeParched, m.X, m.Y, m.Z,
			0, nil, false, "", true, func(e *mobEntity) { e.mob.Yaw, e.mob.HeadYaw = m.Yaw, m.Yaw })
		s.linkMobPassengers(vehicle, parched)
	case entity.JockeyNearbyChicken:
		s.mobsMu.Lock()
		vehicle := entity.NearbyJockeyChicken(m, s.herdOf(dim.w).herd)
		if vehicle == nil {
			s.mobsMu.Unlock()
			return
		}
		// No eggs while ridden, and monster despawn rules.
		vehicle.ChickenJockey = true
		vehEnt := s.herdOf(dim.w).book[vehicle]
		s.mobsMu.Unlock()
		if vehEnt != nil {
			s.linkMobPassengers(vehEnt, ent)
		}
	}
}

// linkMobPassengers seats passenger on the mob vehicle and tells the whole
// dimension, like boat seats; later pairings carry it too.
func (s *Server) linkMobPassengers(vehicle, passenger *mobEntity) {
	s.mobsMu.Lock()
	entity.Ride(vehicle.mob, passenger.mob)
	n := mobSeatNews(passenger, vehicle.mob, rules.GameEventEntityMount)
	s.mobsMu.Unlock()
	s.sendSeatNews([]seatNews{n})
}

// addRiddenConversion transfers seats before pairing the replacement, so its
// spawn carries both its passengers and its mob vehicle's updated list.
func (s *Server) addRiddenConversion(old, next *mobEntity) {
	s.mobsMu.Lock()
	n := seatNews{w: old.w}
	if veh := old.mob.Vehicle; veh != nil {
		n.event(rules.GameEventEntityDismount, veh.X, veh.Y, veh.Z, rules.GameEventContext{Mob: old.uuid})
	}
	for i, p := range old.mob.Passengers {
		if ent := s.herdOf(old.w).book[p]; ent != nil {
			n.event(rules.GameEventEntityDismount, old.mob.X, old.mob.Y, old.mob.Z, rules.GameEventContext{Mob: ent.uuid})
			if i == 0 && old.mob.Horse.Rider == uuid.Nil {
				n.event(rules.GameEventEntityMount, next.mob.X, next.mob.Y, next.mob.Z, rules.GameEventContext{Mob: ent.uuid})
			}
		}
	}
	entity.ConvertMobRides(old.mob, next.mob)
	firstPlayer := old.rider
	players := old.playersAboard()
	s.mobsMu.Unlock()
	for _, ev := range n.events {
		s.gameEvent(ev.w, ev.event, ev.x, ev.y, ev.z, ev.context)
	}
	for _, c := range players {
		c.dismountMob(false)
	}
	boat, seats := s.mobTookOverBoat(old, next)
	s.addMob(next)
	s.mobTookOverMinecart(old, next)
	if firstPlayer != nil {
		s.seatPlayerOnMob(firstPlayer, next, true)
	}
	if boat != nil {
		s.broadcastWorld(next.w, protocol.IDPlaySetPassengers, setPassengersPayload(boat.boat.ID, seats))
	}
}

// dismountMob takes a mob off its mob vehicle and tells the vehicle's
// viewers. A player's lead on a rider ends the ride (vanilla's setLeashedTo),
// and a boarded hull takes the rider. Boat seats are mobLeftItsBoat's.
func (s *Server) dismountMob(ent *mobEntity) {
	s.mobsMu.Lock()
	vehicle := ent.mob.Vehicle
	if vehicle == nil {
		s.mobsMu.Unlock()
		return
	}
	entity.DismountMob(ent.w, vehicle, ent.mob)
	n := mobSeatNews(ent, vehicle, rules.GameEventEntityDismount)
	s.mobsMu.Unlock()
	s.sendSeatNews([]seatNews{n})
}

// seatNews is a vehicle whose passenger list changed under the mob lock,
// told after it.
type seatNews struct {
	w       *world.World
	vehicle int32
	ids     []int32
	events  []mobGameEvent
}

// sendSeatNews sends each vehicle's Set Passengers to its dimension.
func (s *Server) sendSeatNews(news []seatNews) {
	for _, n := range news {
		for _, ev := range n.events {
			s.gameEvent(ev.w, ev.event, ev.x, ev.y, ev.z, ev.context)
		}
		s.broadcastWorld(n.w, protocol.IDPlaySetPassengers, setPassengersPayload(n.vehicle, n.ids))
	}
}

// jockeyPrep equips a finalize-spawned rider: a zombie horse's zombie holds
// an iron spear and rolls no chicken.
// vanilla: ZombieHorse.finalizeSpawn
func (s *Server) jockeyPrep(kind entity.MobType) func(*mobEntity) {
	if kind != entity.TypeZombie {
		return nil
	}
	return func(ent *mobEntity) {
		ent.mob.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: int32(registry.ItemIronSpear), Count: 1})
		ent.mob.Jockey = entity.JockeyNone
	}
}

// event records a passenger's event at the vehicle's position while its lock is held.
func (n *seatNews) event(ev rules.GameEvent, x, y, z float64, ctx rules.GameEventContext) {
	n.events = append(n.events, mobGameEvent{w: n.w, event: ev, x: x, y: y, z: z, context: ctx})
}

// mobSeatNews snapshots a mob vehicle's passengers and the rider's game event.
// Caller holds mobsMu. vanilla: Entity.startRiding, Entity.removeVehicle
func mobSeatNews(ent *mobEntity, veh *entity.Mob, ev rules.GameEvent) seatNews {
	n := seatNews{w: ent.w, vehicle: veh.ID, ids: veh.PassengerIDs()}
	n.event(ev, veh.X, veh.Y, veh.Z, rules.GameEventContext{Mob: ent.uuid})
	return n
}
