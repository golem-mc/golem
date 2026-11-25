package server

import (
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// clientByEntityID finds an online player by protocol entity ID, nil for none.
func (s *Server) clientByEntityID(id int32) *client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.clients {
		if c.player.EntityID == id {
			return c
		}
	}
	return nil
}

// relatedByID is the player, mob or leash knot an entity ID names in w, nil
// for none. No lock may be held.
func (s *Server) relatedByID(w *world.World, id int32) command.Entity {
	if id == 0 {
		return nil
	}
	if c := s.clientByEntityID(id); c != nil {
		return c
	}
	s.mobsMu.Lock()
	ent := s.mobByEntityIDLocked(w, id)
	var snap *entitySnapshot
	if ent != nil {
		snap = s.mobSnapshot(ent, s.dimensionOfWorld(w).name, nil)
	}
	s.mobsMu.Unlock()
	if snap != nil {
		return snap
	}
	if k := s.knotByEntityID(w, id); k != nil {
		s.knotsMu.Lock()
		defer s.knotsMu.Unlock()
		return s.knotSnapshot(k, nil)
	}
	return nil
}

// relatedByUUID is the player or mob with this identity, nil for none. No
// lock may be held.
func (s *Server) relatedByUUID(identity uuid.UUID) command.Entity {
	if identity == uuid.Nil {
		return nil
	}
	if c := s.clientByUUID(identity); c != nil {
		return c
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		if ent := dim.mobs.book[dim.mobs.byUUID[identity]]; ent != nil {
			return s.mobSnapshot(ent, dim.name, nil)
		}
	}
	return nil
}

// relatedMobAnyDim is the mob with this ID in whichever dimension holds it, nil for none.
func (s *Server) relatedMobAnyDim(id int32) command.Entity {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	ent, dim := s.mobByEntityIDAnyDimLocked(id)
	if ent == nil {
		return nil
	}
	if snap := s.mobSnapshot(ent, dim.name, nil); snap != nil {
		return snap
	}
	return nil
}

// firstRelated is the single related entity as a result list.
func firstRelated(e command.Entity) []command.Entity {
	if e == nil {
		return nil
	}
	return []command.Entity{e}
}

// Relation is the entities the player stands in the named relation to.
// vanilla: ExecuteCommand.createRelationOperations
func (c *client) Relation(name string) []command.Entity {
	switch name {
	case "vehicle":
		if v, ok := c.Vehicle(); ok {
			return []command.Entity{v}
		}
	case "attacker":
		return firstRelated(c.srv.relatedByID(c.theWorld(), c.player.Vitals().LastHurtBy))
	}
	return nil
}

// Relation is the entities this body stands in the named relation to.
// vanilla: ExecuteCommand.createRelationOperations
func (e *entitySnapshot) Relation(name string) []command.Entity {
	s := e.srv
	switch ref := e.ref.(type) {
	case *mobEntity:
		return s.mobRelation(ref, e, name)
	case *arrowEntity:
		if name != "origin" {
			return nil
		}
		s.arrowsMu.Lock()
		id := ref.arrow.OwnerID
		s.arrowsMu.Unlock()
		return firstRelated(s.relatedByID(ref.w, id))
	case *thrownEntity:
		if name != "origin" {
			return nil
		}
		s.arrowsMu.Lock()
		id := ref.throwable.OwnerID
		s.arrowsMu.Unlock()
		return firstRelated(s.relatedByID(ref.w, id))
	case *fireworkEntity:
		if name != "origin" {
			return nil
		}
		s.fireworksMu.Lock()
		owner := ref.ownerUUID
		s.fireworksMu.Unlock()
		return firstRelated(s.relatedByUUID(owner))
	case *bobberEntity:
		if name != "origin" {
			return nil
		}
		s.bobbersMu.Lock()
		owner := ref.owner
		s.bobbersMu.Unlock()
		return firstRelated(s.relatedByUUID(owner))
	case *tntEntity:
		if name != "origin" {
			return nil
		}
		s.tntMu.Lock()
		id := ref.tnt.OwnerID
		s.tntMu.Unlock()
		return firstRelated(s.relatedByID(ref.w, id))
	case *cloudEntity:
		if name != "origin" {
			return nil
		}
		s.arrowsMu.Lock()
		owner := ref.ownerUUID
		s.arrowsMu.Unlock()
		return firstRelated(s.relatedByUUID(owner))
	case *boatEntity:
		return s.boatRelation(ref, name)
	case *minecartEntity:
		if name != "passengers" {
			return nil
		}
		s.minecartsMu.Lock()
		riders := append([]*client(nil), ref.passengers...)
		seated := append([]*mobEntity(nil), ref.mobs...)
		s.minecartsMu.Unlock()
		var out []command.Entity
		for _, c := range riders {
			out = append(out, c)
		}
		dim := s.dimensionOfWorld(ref.w).name
		for _, m := range seated {
			s.mobsMu.Lock()
			snap := s.mobSnapshot(m, dim, nil)
			s.mobsMu.Unlock()
			out = append(out, snap)
		}
		return out
	case *itemEntity:
		if name != "origin" {
			return nil
		}
		s.itemsMu.Lock()
		owner := ref.thrower
		s.itemsMu.Unlock()
		return firstRelated(s.relatedByUUID(owner))
	}
	return nil
}

func (s *Server) boatRelation(b *boatEntity, name string) []command.Entity {
	if name != "passengers" && name != "controller" {
		return nil
	}
	s.boatsMu.Lock()
	seats := append([]*boatPassenger(nil), b.passengers...)
	s.boatsMu.Unlock()
	var out []command.Entity
	for _, p := range seats {
		switch {
		case p.c != nil:
			out = append(out, p.c)
		case p.ent != nil:
			s.mobsMu.Lock()
			snap := s.mobSnapshot(p.ent, s.dimensionOfWorld(b.w).name, nil)
			s.mobsMu.Unlock()
			out = append(out, snap)
		}
	}
	if name == "controller" && len(out) > 1 {
		out = out[:1]
	}
	return out
}

// mobRelation answers for a mob. The ids are read under the mob lock and
// resolved after it, since a player or a boat has its own lock.
func (s *Server) mobRelation(ent *mobEntity, self *entitySnapshot, name string) []command.Entity {
	if name == "vehicle" {
		if v, ok := self.Vehicle(); ok {
			return []command.Entity{v}
		}
		return nil
	}
	s.mobsMu.Lock()
	m := ent.mob
	var (
		riders   []int32
		minded   []command.Entity
		targetID int32
		ownerID  uuid.UUID
	)
	switch name {
	case "passengers", "controller":
		if m.Horse.RiderID != 0 {
			riders = append(riders, m.Horse.RiderID)
		}
		if name == "passengers" {
			riders = append(riders, m.Horse.ExtraRiderIDs...)
			for _, p := range m.Passengers {
				if mount := s.mobByEntityIDLocked(ent.w, p.ID); mount != nil {
					minded = append(minded, s.mobSnapshot(mount, self.dim, nil))
				}
			}
		}
	case "target":
		targetID = m.TargetID
	case "attacker":
		targetID = m.LastAttackerID()
	case "leasher":
		targetID = m.LeashHolderID
	case "origin":
		targetID = m.VexOwnerID()
		if targetID == 0 {
			ownerID = m.VexOwnerUUID()
		}
	case "owner":
		if m.Tamable.Tame {
			ownerID = m.Tamable.Owner
		} else {
			ownerID = m.Horse.Owner
		}
	}
	s.mobsMu.Unlock()

	out := minded
	for _, id := range riders {
		if c := s.clientByEntityID(id); c != nil {
			out = append(out, c)
		}
	}
	if targetID != 0 {
		related := s.relatedByID(ent.w, targetID)
		if related == nil {
			// A mob keeps its attacker's ID across dimensions. vanilla: EntityReference.getEntity
			related = s.relatedMobAnyDim(targetID)
		}
		out = append(out, firstRelated(related)...)
	}
	if ownerID != uuid.Nil {
		out = append(out, firstRelated(s.relatedByUUID(ownerID))...)
	}
	return out
}
