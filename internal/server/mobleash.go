package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
)

// tieLeash ties the mob's lead to the body id, which a save names by ref.
// The caller holds the mob lock.
func (ent *mobEntity) tieLeash(id int32, ref leashRef) {
	ent.mob.LeashHolderID = id
	ent.leash, ent.leashOf, ent.leashWait = ref, id, 0
}

// savedLeash is the lead as the mob's save names it: the holder's ref while
// the holder it was tied with still holds it, a lead read from a save still
// waiting for its holder, or none. The caller holds the mob lock.
func (ent *mobEntity) savedLeash() leashRef {
	held := ent.mob.LeashHolderID
	if held == ent.leashOf {
		return ent.leash
	}
	return leashRef{}
}

// waitingLeash reports whether the mob carries a lead read from a save that
// no holder has claimed yet. The caller holds the mob lock.
func (ent *mobEntity) waitingLeash() bool {
	return ent.mob.LeashHolderID == 0 && ent.leashOf == 0 && ent.leash.set()
}

// readLeash takes the lead a compound names: another lead than the one the
// mob saves replaces it, waiting for its holder from the next tick; none
// takes the lead off without its item.
// vanilla: Leashable.readLeashData
func (ent *mobEntity) readLeash(ref leashRef) {
	if ref == ent.savedLeash() {
		return
	}
	ent.mob.RemoveLeash()
	ent.leash, ent.leashOf, ent.leashWait = ref, 0, 0
}

// restoreMobLeashes re-ties each mob waiting with a saved lead to the holder
// it names: the knot on the cell, stood up there if none stands, or a body
// of the mob's world by UUID, a player or a mob. A mob whose holder is not
// there waits, and lays its lead once the grace runs out. Called from the
// mob tick with no lock held.
// vanilla: Leashable.restoreLeashFromSave
func (s *Server) restoreMobLeashes() {
	var ents []*mobEntity
	var waits []leashWait
	s.lockTick(&s.mobsMu)
	for _, dim := range s.allDims() {
		h := dim.mobs
		if h == nil || len(h.leashWaits) == 0 {
			continue
		}
		kept := h.leashWaits[:0]
		for _, ent := range h.leashWaits {
			if h.book[ent.mob] != ent || ent.mob.Dead || !ent.waitingLeash() {
				continue
			}
			kept = append(kept, ent)
			wt := leashWait{w: dim.w, ref: ent.leash}
			if !wt.ref.atKnot {
				if m := h.byUUID[wt.ref.holder]; m != nil && m.Alive() {
					wt.holder = m.ID
				}
			}
			ents = append(ents, ent)
			waits = append(waits, wt)
		}
		clear(h.leashWaits[len(kept):])
		h.leashWaits = kept
	}
	s.unlockTick(&s.mobsMu)
	if len(waits) == 0 {
		return
	}

	knots := s.standLeashHolders(waits)
	var tied, dropped []leashTell
	s.lockTick(&s.mobsMu)
	for i, wt := range waits {
		ent := ents[i]
		if ent.mob.Dead || !ent.waitingLeash() || ent.leash != wt.ref {
			continue // untied, re-tied or gone since the look
		}
		knots[i] = nil
		t := leashTell{w: wt.w, id: ent.mob.ID, holder: wt.holder, x: ent.mob.X, y: ent.mob.Y, z: ent.mob.Z}
		if wt.holder != 0 {
			ent.tieLeash(wt.holder, wt.ref)
			tied = append(tied, t)
			continue
		}
		// vanilla checks the body's tickCount > 100; a wait starts at its
		// load.
		if ent.leashWait++; ent.leashWait > entity.LeashRestoreTicks {
			ent.leash, ent.leashWait = leashRef{}, 0
			dropped = append(dropped, t)
		}
	}
	s.unlockTick(&s.mobsMu)
	s.tellLeashRestores(knots, tied, dropped)
}
