package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
)

// Vehicle is the player's boat, cart, mob or cushion as a command entity.
// The vehicles' locks are taken one after the other, never together.
func (c *client) Vehicle() (command.Entity, bool) {
	if it := c.vehicle.Load(); it != nil {
		c.srv.boatsMu.Lock()
		snap := c.srv.boatSnapshot(it, nil)
		c.srv.boatsMu.Unlock()
		if snap != nil {
			return snap, true
		}
	}
	if it := c.minecart.Load(); it != nil {
		c.srv.minecartsMu.Lock()
		snap := c.srv.minecartSnapshot(it, nil)
		c.srv.minecartsMu.Unlock()
		if snap != nil {
			return snap, true
		}
	}
	if ent := c.mount.Load(); ent != nil {
		c.srv.mobsMu.Lock()
		snap := c.srv.mobSnapshot(ent, ent.w.Name(), nil)
		c.srv.mobsMu.Unlock()
		return snap, true
	}
	if ent := c.cushion.Load(); ent != nil {
		c.srv.fixturesMu.Lock()
		snap := c.srv.fixtureSnapshot(ent, nil)
		c.srv.fixturesMu.Unlock()
		if snap != nil {
			return snap, true
		}
	}
	return nil, false
}

// HasIndirectPassenger reports whether the entity rides the player, however
// many seats up. vanilla: Entity.getSelfAndPassengers
func (c *client) HasIndirectPassenger(command.Entity) bool {
	return false
}

// StartRiding seats the player on a boat, cart, mob or cushion; other
// vehicles are refused. vanilla: Entity.startRiding
func (c *client) StartRiding(vehicle command.Entity) bool {
	snap, ok := vehicle.(*entitySnapshot)
	if !ok {
		return false
	}
	switch ref := snap.ref.(type) {
	case *boatEntity:
		return c.srv.mountBoat(c, ref, true)
	case *minecartEntity:
		return c.srv.mountMinecart(c, ref, true)
	case *mobEntity:
		return c.srv.seatPlayerOnMob(c, ref, true)
	case *fixtureEntity:
		if ref.kind == entity.FixtureCushion {
			return c.srv.mountCushion(c, ref, true)
		}
	}
	return false
}

// StopRiding takes the player off whatever they ride, stepping them out.
// vanilla: Entity.stopRiding
func (c *client) StopRiding() {
	c.dismountBoat(true)
	c.dismountMinecart(true)
	c.dismountMob(true)
	c.dismountCushion(true)
}

// Vehicle is the boat or mob a mob sits on, as a command entity.
func (e *entitySnapshot) Vehicle() (command.Entity, bool) {
	ent, ok := e.ref.(*mobEntity)
	if !ok {
		return nil, false
	}
	s := e.srv
	s.mobsMu.Lock()
	boat, cart, mount := ent.vehicle, ent.cart, ent.mob.Vehicle
	var mountSnap *entitySnapshot
	if mount != nil {
		if veh := s.mobByEntityIDLocked(ent.w, mount.ID); veh != nil {
			mountSnap = s.mobSnapshot(veh, e.dim, nil)
		}
	}
	s.mobsMu.Unlock()
	if mountSnap != nil {
		return mountSnap, true
	}
	if cart != nil {
		s.minecartsMu.Lock()
		snap := s.minecartSnapshot(cart, nil)
		s.minecartsMu.Unlock()
		if snap == nil {
			return nil, false
		}
		return snap, true
	}
	if boat == nil {
		return nil, false
	}
	s.boatsMu.Lock()
	snap := s.boatSnapshot(boat, nil)
	s.boatsMu.Unlock()
	if snap == nil {
		return nil, false
	}
	return snap, true
}

// HasIndirectPassenger reports whether the entity rides this mob, directly
// or via other mobs. vanilla: Entity.getSelfAndPassengers
func (e *entitySnapshot) HasIndirectPassenger(other command.Entity) bool {
	ent, ok := e.ref.(*mobEntity)
	if !ok {
		return false
	}
	s := e.srv
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	var walk func(m *entity.Mob) bool
	walk = func(m *entity.Mob) bool {
		if m.Horse.RiderID != 0 && m.Horse.RiderID == other.EntityID() ||
			slices.Contains(m.Horse.ExtraRiderIDs, other.EntityID()) {
			return true
		}
		for _, p := range m.Passengers {
			if p.ID == other.EntityID() || walk(p) {
				return true
			}
		}
		return false
	}
	return walk(ent.mob)
}

// StartRiding seats a mob on a boat, a minecart or another mob; other
// vehicles are refused. vanilla: Entity.startRiding
func (e *entitySnapshot) StartRiding(vehicle command.Entity) bool {
	ent, ok := e.ref.(*mobEntity)
	if !ok {
		return false
	}
	snap, ok := vehicle.(*entitySnapshot)
	if !ok {
		return false
	}
	switch ref := snap.ref.(type) {
	case *boatEntity:
		return e.srv.mountMobOnBoat(ent, ref)
	case *minecartEntity:
		return e.srv.mountMobOnMinecart(ent, ref, true)
	case *mobEntity:
		return e.srv.mountMobOnMob(ent, ref)
	}
	return false
}

// StopRiding sets a mob down beside the boat or cart it sits in.
// vanilla: Entity.stopRiding
func (e *entitySnapshot) StopRiding() {
	if ent, ok := e.ref.(*mobEntity); ok {
		e.srv.mobLeftItsVehicle(ent)
		e.srv.mobLeftItsMount(ent)
	}
}

// mountMobOnBoat seats a mob on a boat ignoring capacity and water, as
// /ride mount does, dropping its leash and leaving any mob vehicle first,
// as the boat tick's push does. The boat and mob locks are never held
// together. It reports whether the mob was seated.
// vanilla: Entity.startRiding, Mob.startRiding
func (s *Server) mountMobOnBoat(ent *mobEntity, it *boatEntity) bool {
	return s.seatMobOnBoat(ent, it, true)
}

// seatMobOnBoat suppresses the mount event when restoring a cross-dimension rider.
// vanilla: Entity.teleportCrossDimension
func (s *Server) seatMobOnBoat(ent *mobEntity, it *boatEntity, sendEvent bool) bool {
	s.mobsMu.Lock()
	if ent.vehicle != nil || !ent.mob.Alive() || !ent.mob.CanRide() {
		s.mobsMu.Unlock()
		return false
	}
	animal := entity.IsAnimal(ent.mob.Type)
	s.mobsMu.Unlock()

	s.boatsMu.Lock()
	if it.boat.Dead {
		s.boatsMu.Unlock()
		return false
	}
	it.passengers = append(it.passengers, &boatPassenger{ent: ent})
	x, y, z := it.seatOf(len(it.passengers), len(it.passengers)-1, animal)
	w, boatID, ids, riders := it.w, it.boat.ID, it.passengerIDs(), it.playersAboard()
	bx, by, bz := it.boat.X, it.boat.Y, it.boat.Z
	s.boatsMu.Unlock()

	var (
		drop    mobLeashDrop
		dropped bool
		oldNews *seatNews
	)
	s.mobsMu.Lock()
	m := ent.mob
	if old := m.Vehicle; old != nil {
		entity.Dismount(old, m)
		n := mobSeatNews(ent, old, rules.GameEventEntityDismount)
		oldNews = &n
	}
	ent.vehicle = it
	m.Carrier = it.uuid
	m.StartRiding()
	// A leashed mob mounts free of its lead, as Mob.startRiding drops it.
	if holder := m.RemoveLeash(); holder != 0 {
		drop = mobLeashDrop{w: ent.w, mobID: m.ID, holder: holder, x: m.X, y: m.Y, z: m.Z}
		dropped = true
	}
	m.SetPos(x, y, z)
	s.mobsMu.Unlock()

	if dropped {
		s.finishLeashDrop(drop, true)
	}
	if oldNews != nil {
		s.sendSeatNews([]seatNews{*oldNews})
	}
	if sendEvent {
		s.gameEvent(w, rules.GameEventEntityMount, bx, by, bz, rules.GameEventContext{Mob: ent.uuid})
	}
	s.broadcastWorld(w, protocol.IDPlaySetPassengers, setPassengersPayload(boatID, ids))
	startedRidingAdvancements(riders)
	return true
}

// mountMobOnMob seats a mob on another ignoring both's rules, as /ride
// mount does, dropping its leash first. It reports whether it was seated.
// vanilla: Entity.startRiding, Mob.startRiding
func (s *Server) mountMobOnMob(ent, mount *mobEntity) bool {
	s.mobsMu.Lock()
	m, veh := ent.mob, mount.mob
	if ent == mount || ent.vehicle != nil || m.Vehicle != nil || !m.Alive() || !veh.Alive() ||
		ent.rider != nil || len(ent.extra) > 0 || ent.w != mount.w || veh.RootVehicle() == m || !m.CanRide() {
		s.mobsMu.Unlock()
		return false
	}
	var (
		drop    mobLeashDrop
		dropped bool
	)
	// A leashed mob mounts free of its lead, as Mob.startRiding drops it.
	if holder := m.RemoveLeash(); holder != 0 {
		drop = mobLeashDrop{w: ent.w, mobID: m.ID, holder: holder, x: m.X, y: m.Y, z: m.Z}
		dropped = true
	}
	entity.Ride(veh, m)
	n, riders := mobSeatNews(ent, veh, rules.GameEventEntityMount), mount.playersAboard()
	s.mobsMu.Unlock()

	if dropped {
		s.finishLeashDrop(drop, true)
	}
	s.sendSeatNews([]seatNews{n})
	startedRidingAdvancements(riders)
	return true
}

// mobLeftItsMount sets a mob down beside the mob it rides, or on top of it.
// vanilla: Entity.stopRiding
func (s *Server) mobLeftItsMount(ent *mobEntity) {
	s.mobsMu.Lock()
	veh := ent.mob.Vehicle
	if veh == nil || !entity.DismountMob(ent.w, veh, ent.mob) {
		s.mobsMu.Unlock()
		return
	}
	n := mobSeatNews(ent, veh, rules.GameEventEntityDismount)
	s.mobsMu.Unlock()
	s.sendSeatNews([]seatNews{n})
}
