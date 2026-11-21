package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// A mob rides a minecart in the cart's seat: the cart's tick carries it, as
// a boat's does its mobs.

// minecartMobSeat is the seat the cart tick leaves its mob in, x, y, z being
// the cart's place; a dismount sets the mob off beside it, toward dx, dz.
type minecartMobSeat struct {
	ent      *mobEntity
	it       *minecartEntity
	w        *world.World
	x, y, z  float64
	dismount bool
	dx, dz   int32
}

// seatMinecartMobs writes the cart tick's mob seats under the mob lock, after
// the cart lock is down. A mob that left the seat since is skipped.
func (s *Server) seatMinecartMobs(seats []minecartMobSeat) {
	if len(seats) == 0 {
		return
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, st := range seats {
		ent, m := st.ent, st.ent.mob
		if ent.cart != st.it {
			continue
		}
		if st.dismount {
			ent.cart = nil
			m.RidesVehicle = false
			m.BoardingCooldown = boardingCooldown
			m.SetPos(minecartMobDismountSpot(m, st.w, st.x, st.y, st.z, st.dx, st.dz))
			continue
		}
		m.SetPos(entity.MinecartMobSeat(m, st.x, st.y, st.z))
		m.VX, m.VY, m.VZ = 0, 0, 0
		m.FallDistance = 0
	}
}

// minecartMobDismountSpot is where a mob leaving a cart at (x, y, z) stands.
// vanilla: AbstractMinecart.getDismountLocationForPassenger
func minecartMobDismountSpot(m *entity.Mob, w *world.World, x, y, z float64, dx, dz int32) (float64, float64, float64) {
	return rules.MinecartDismountSpot(w, x, y, z, dx, dz,
		[]rules.DismountPose{{Dims: rules.PlayerDimensions{Width: m.BbWidth(), Height: m.BbHeight()}}})
}

// mountMobOnMinecart seats a mob in a cart, dropping its leash; force skips
// the cart's one-passenger rule, as /ride mount does. The cart and mob locks
// are never held together. It reports whether the mob was seated.
// vanilla: Entity.startRiding, Mob.startRiding
func (s *Server) mountMobOnMinecart(ent *mobEntity, it *minecartEntity, force bool) bool {
	return s.seatMobOnMinecart(ent, it, force, true)
}

// seatMobOnMinecart suppresses the mount event when restoring a cross-dimension rider.
// vanilla: Entity.teleportCrossDimension
func (s *Server) seatMobOnMinecart(ent *mobEntity, it *minecartEntity, force, sendEvent bool) bool {
	s.mobsMu.Lock()
	ok := !ent.seated() && ent.mob.Alive() && ent.mob.CanRide()
	s.mobsMu.Unlock()
	if !ok {
		return false
	}
	s.minecartsMu.Lock()
	if it.cart.Dead || slices.Contains(it.mobs, ent) || it.w != ent.w ||
		(!force && (len(it.passengers) > 0 || len(it.mobs) > 0)) {
		s.minecartsMu.Unlock()
		return false
	}
	it.mobs = append(it.mobs, ent)
	w, id, ids := it.w, it.cart.ID, it.passengerIDs()
	x, y, z := it.cart.X, it.cart.Y, it.cart.Z
	s.minecartsMu.Unlock()

	var (
		drop    mobLeashDrop
		dropped bool
	)
	s.mobsMu.Lock()
	m := ent.mob
	ent.cart = it
	m.StartRiding()
	// A leashed mob mounts free of its lead, as Mob.startRiding drops it.
	if holder := m.RemoveLeash(); holder != 0 {
		drop = mobLeashDrop{w: ent.w, mobID: m.ID, holder: holder, x: m.X, y: m.Y, z: m.Z}
		dropped = true
	}
	m.SetPos(entity.MinecartMobSeat(m, x, y, z))
	s.mobsMu.Unlock()

	if dropped {
		s.finishLeashDrop(drop, true)
	}
	if sendEvent {
		s.gameEvent(w, rules.GameEventEntityMount, x, y, z, rules.GameEventContext{Mob: ent.uuid})
	}
	s.broadcastWorld(w, protocol.IDPlaySetPassengers, setPassengersPayload(id, ids))
	return true
}

// mobLeftItsMinecart sets a mob down beside the cart it sits in, if any.
// vanilla: Entity.stopRiding
func (s *Server) mobLeftItsMinecart(ent *mobEntity) {
	s.removeMobFromMinecart(ent, true)
}

// removeMobFromMinecart can retain a teleport destination while ending the seat.
func (s *Server) removeMobFromMinecart(ent *mobEntity, reposition bool) {
	var (
		w          *world.World
		id         int32
		ids        []int32
		cx, cy, cz float64
		dx, dz     int32
		found      bool
	)
	s.minecartsMu.Lock()
	for _, it := range s.minecarts {
		i := slices.Index(it.mobs, ent)
		if i < 0 {
			continue
		}
		it.mobs = slices.Delete(it.mobs, i, i+1)
		w, id, ids = it.w, it.cart.ID, it.passengerIDs()
		cx, cy, cz = it.cart.X, it.cart.Y, it.cart.Z
		dx, dz = it.cart.MotionDir()
		found = true
		break
	}
	s.minecartsMu.Unlock()
	s.mobsMu.Lock()
	if ent.cart != nil {
		ent.cart = nil
		ent.mob.RidesVehicle = false
		ent.mob.BoardingCooldown = boardingCooldown
	}
	if found && reposition {
		m := ent.mob
		m.SetPos(minecartMobDismountSpot(m, w, cx, cy, cz, dx, dz))
	}
	s.mobsMu.Unlock()
	if found {
		s.gameEvent(w, rules.GameEventEntityDismount, cx, cy, cz, rules.GameEventContext{Mob: ent.uuid})
		s.broadcastWorld(w, protocol.IDPlaySetPassengers, setPassengersPayload(id, ids))
	}
}

// mobLeftItsVehicle takes a mob out of the boat or cart it sits in.
func (s *Server) mobLeftItsVehicle(ent *mobEntity) {
	s.mobLeftItsBoat(ent)
	s.mobLeftItsMinecart(ent)
}

// mobTookOverMinecart moves a converted body's seat after the replacement's
// spawn, so Set Passengers can name the new entity ID.
// vanilla: ConversionType.SINGLE.convert
func (s *Server) mobTookOverMinecart(old, next *mobEntity) {
	s.mobsMu.Lock()
	it := old.cart
	if it != nil {
		old.cart = nil
		old.mob.RidesVehicle = false
	}
	s.mobsMu.Unlock()
	if it == nil {
		return
	}
	s.minecartsMu.Lock()
	it.mobs = slices.DeleteFunc(it.mobs, func(e *mobEntity) bool { return e == old })
	w, x, y, z := it.w, it.cart.X, it.cart.Y, it.cart.Z
	s.minecartsMu.Unlock()
	s.gameEvent(w, rules.GameEventEntityDismount, x, y, z, rules.GameEventContext{Mob: old.uuid})
	// SINGLE's startRiding(vehicle, false, false) suppresses the mount event.
	s.seatMobOnMinecart(next, it, false, false)
}

// carryMinecartMobs seats on to, in to's world, twins of the mobs aboard from
// (mobsAcross).
// vanilla: Entity.teleportCrossDimension re-seating its newPassengers
func (s *Server) carryMinecartMobs(from, to *minecartEntity, turn float32) {
	s.minecartsMu.Lock()
	mobs := from.mobs
	from.mobs = nil
	s.minecartsMu.Unlock()
	s.reseatMinecartMobs(mobs, from, to, turn)
}

// reseatMinecartMobs seats on to twins of mobs, taken off from (mobsAcross).
func (s *Server) reseatMinecartMobs(mobs []*mobEntity, from, to *minecartEntity, turn float32) {
	s.minecartsMu.Lock()
	w, x, y, z := to.w, to.cart.X, to.cart.Y, to.cart.Z
	s.minecartsMu.Unlock()
	aboard := func(e *mobEntity) bool { return e.cart == from }
	for _, twin := range s.mobsAcross(mobs, aboard, w, x, y, z, turn) {
		s.seatMobOnMinecart(twin, to, true, false)
	}
}

// boardingCooldown is the ticks a mob that left a vehicle waits before a cart
// or boat takes it aboard again. vanilla: Entity.removePassenger
const boardingCooldown = 60

// minecartPickup is a moving, empty rideable cart and the box it takes a
// body aboard from this tick.
type minecartPickup struct {
	it  *minecartEntity
	box entity.AABB
}

// pickupCandidate is a boardable mob and its box, as one tick saw it.
type pickupCandidate struct {
	ent *mobEntity
	id  int32
	box entity.AABB
}

// pickupCands is one world's candidate list and the grid over it; both keep
// their storage between ticks.
type pickupCands struct {
	list []pickupCandidate
	grid entity.BoxGrid
}

// pickUpMobs seats in each cart the touching mob with the lowest entity ID
// (vanilla takes the first its entity search returns). Players are never
// taken aboard this way. Each world's herd is walked once, under one hold
// of the mob lock, for all its carts; a mob one cart took is nobody the
// next takes.
// vanilla: OldMinecartBehavior.pushAndPickupEntities
func (s *Server) pickUpMobs(pickups []minecartPickup) {
	if len(pickups) == 0 {
		return
	}
	cands := s.pickupCands
	if cands == nil {
		cands = make(map[*world.World]*pickupCands)
		s.pickupCands = cands
	}
	defer func() {
		for _, wc := range cands {
			clear(wc.list)
			wc.list = wc.list[:0]
		}
	}()
	// reach is the box around a world's pickup carts; a mob outside it is
	// nobody's candidate.
	reach := make(map[*world.World]entity.AABB, 1)
	for _, p := range pickups {
		if r, ok := reach[p.it.w]; ok {
			reach[p.it.w] = r.Union(p.box)
		} else {
			reach[p.it.w] = p.box
		}
	}
	s.mobsMu.Lock()
	for _, p := range pickups {
		r, ok := reach[p.it.w]
		if !ok {
			continue
		}
		delete(reach, p.it.w)
		wc := cands[p.it.w]
		if wc == nil {
			wc = &pickupCands{}
			cands[p.it.w] = wc
		}
		wc.grid.Reset()
		for m, ent := range s.herdOf(p.it.w).book {
			if ent.seated() || !entity.MinecartBoardable(m) {
				continue
			}
			if box := entity.NewAABB(m.BlastBox()); r.Crosses(box) {
				wc.list = append(wc.list, pickupCandidate{ent: ent, id: m.ID, box: box})
			}
		}
		for i, c := range wc.list {
			wc.grid.Add(int32(i), c.box)
		}
	}
	s.mobsMu.Unlock()
	var taken map[*mobEntity]bool
	for _, p := range pickups {
		var took *pickupCandidate
		wc := cands[p.it.w]
		wc.grid.Query(p.box, func(i int32) {
			c := &wc.list[i]
			if taken[c.ent] || !p.box.Crosses(c.box) {
				return
			}
			if took == nil || c.id < took.id {
				took = c
			}
		})
		if took != nil && s.mountMobOnMinecart(took.ent, p.it, false) {
			if taken == nil {
				taken = make(map[*mobEntity]bool)
			}
			taken[took.ent] = true
		}
	}
}
