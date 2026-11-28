package server

import (
	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// leashRef is a lead's holder as a save names it: a knot by the fence cell
// it stands on, any other holder by its UUID. The zero value names none.
// vanilla: Leashable.LeashData.CODEC
type leashRef struct {
	holder uuid.UUID
	knot   [3]int32
	atKnot bool
}

// set reports whether the reference names a holder.
func (r leashRef) set() bool { return r.atKnot || r.holder != uuid.Nil }

// leashKey is the compound key a lead is saved under.
// vanilla: Leashable.LEASH_TAG
const leashKey = "leash"

// putLeash writes the lead's holder under leashKey: a knot's cell as an int
// array, any other holder as a compound with its UUID. Nothing is written
// for an unset reference.
func putLeash(tag nbt.CompoundTag, r leashRef) {
	switch {
	case r.atKnot:
		tag.Set(leashKey, nbt.IntArray{r.knot[0], r.knot[1], r.knot[2]})
	case r.holder != uuid.Nil:
		tag.Set(leashKey, nbt.Compound{"UUID": nbt.UUIDToIntArray(r.holder)})
	}
}

// leashOfTag reads a lead's holder off the compound; an absent or malformed
// lead reads as unset.
func leashOfTag(compound nbt.Compound) leashRef {
	switch v := compound[leashKey].(type) {
	case nbt.IntArray:
		if len(v) == 3 {
			return leashRef{knot: [3]int32{v[0], v[1], v[2]}, atKnot: true}
		}
	case nbt.CompoundTag:
		if id, ok := nbt.UUIDField(v.Entries(), "UUID"); ok {
			return leashRef{holder: id}
		}
	}
	return leashRef{}
}

// leashWait is a body whose lead a save named, as its kind's first look
// found it: holder is the body's entity id the caller already found for
// the ref, or 0.
type leashWait struct {
	w      *world.World
	ref    leashRef
	holder int32
}

// standLeashHolders fills each wait's holder still unfound: the knot on the
// cell, stood up there if none stands, or a player of the wait's world by
// UUID, or a mob of that world by UUID. It returns the knots it stood up, by wait index. Called with no
// lock held.
// vanilla: Leashable.restoreLeashFromSave
func (s *Server) standLeashHolders(waits []leashWait) []*knotEntity {
	knots := make([]*knotEntity, len(waits))
	for i := range waits {
		wt := &waits[i]
		switch {
		case wt.ref.atKnot:
			k, _, created := s.tieKnot(wt.w, wt.ref.knot[0], wt.ref.knot[1], wt.ref.knot[2])
			wt.holder = k.id
			if created {
				knots[i] = k
			}
		case wt.holder == 0:
			if c := s.clientByUUID(wt.ref.holder); c != nil && c.player != nil && c.theWorld() == wt.w {
				wt.holder = c.player.EntityID
			} else {
				s.mobsMu.Lock()
				if m := s.herdOf(wt.w).byUUID[wt.ref.holder]; m != nil && m.Alive() {
					wt.holder = m.ID
				}
				s.mobsMu.Unlock()
				if wt.holder == 0 {
					wt.holder = s.vehicleIDByUUID(wt.w, wt.ref.holder)
				}
			}
		}
	}
	return knots
}

// vehicleIDByUUID is the entity ID of the live boat or minecart of w with
// the UUID, or 0.
func (s *Server) vehicleIDByUUID(w *world.World, id uuid.UUID) int32 {
	s.boatsMu.Lock()
	for _, it := range s.boats {
		if it.w == w && it.uuid == id && !it.boat.Dead {
			s.boatsMu.Unlock()
			return it.boat.ID
		}
	}
	s.boatsMu.Unlock()
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.w == w && it.uuid == id && !it.cart.Dead {
			return it.cart.ID
		}
	}
	return 0
}

// leashTell is a restored lead's news: a body tied to holder, or laying its
// lead at x, y, z.
type leashTell struct {
	w       *world.World
	id      int32
	holder  int32
	x, y, z float64
}

// tellLeashRestores tells the outcome of a restore after its lock section:
// it takes down the knots still set, stood up for a body that no longer
// waited, tells the tied links and lays the dropped leads. Called with no
// lock held.
func (s *Server) tellLeashRestores(knots []*knotEntity, tied, dropped []leashTell) {
	for _, k := range knots {
		if k != nil {
			s.discardEmptiedKnot(k)
		}
	}
	for _, t := range tied {
		s.broadcastWorld(t.w, protocol.IDPlaySetEntityLink, leashLinkPayload(t.id, t.holder))
	}
	// vanilla: Entity.spawnAtLocation, with no link told: none was.
	for _, t := range dropped {
		s.dropLeadItem(t.w, t.x, t.y, t.z)
	}
}
