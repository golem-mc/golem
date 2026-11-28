package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

type collisionPass uint8

const (
	mobCollisions collisionPass = iota
	itemCollisions
	orbCollisions
	tntCollisions
	armorStandCollisions
	fallingCollisions
	collisionPassCount
)

// collidersByWorld reads solid bodies before a mover takes its own lock.
// Each pass owns its storage; queries and rebuilds run on the tick goroutine.
// vanilla: EntityGetter.getEntityCollisions, Entity.canCollideWith
func (s *Server) collidersByWorld(pass collisionPass) map[*world.World]entity.World {
	hulls := s.hullsByWorld()
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	out := s.colliderMaps[pass]
	if out == nil {
		out = make(map[*world.World]entity.World, len(s.allDims()))
		s.colliderMaps[pass] = out
	}
	clear(out)
	for _, dim := range s.allDims() {
		herd := dim.mobs.herd
		if dim.mobs.colliders == 0 {
			herd = nil
		}
		out[dim.w] = dim.colliders[pass].reset(dim.w, hulls[dim.w], herd, false)
	}
	return out
}

type mobCollisionWorld struct {
	entity.Bodies
	grid entity.BoxGrid
	mobs map[*entity.Mob]int32
	// shulkers marks the shulker hulls in Boxes.
	shulkers []bool
	skip     []int32
	riders   []*entity.Mob
	// view is ForMob's result, reused so a mover's view is not boxed anew.
	view entity.Bodies
}

// reset requires mobsMu; the hulls were read before taking it. markShulkers
// builds the mask ForMob drops for a shulker; only the mob pass reads it. The
// returned view is valid until this dimension's same pass rebuilds it next tick.
func (w *mobCollisionWorld) reset(level *world.World, hulls []entity.AABB, herd entity.Mobs, markShulkers bool) entity.World {
	w.World = level
	w.Boxes = append(w.Boxes[:0], hulls...)
	w.grid.Cell = 4
	w.grid.Reset()
	w.Grid = &w.grid
	clear(w.mobs)
	if markShulkers {
		w.shulkers = slices.Grow(w.shulkers[:0], len(hulls))[:len(hulls)]
		clear(w.shulkers)
	}
	for _, m := range herd {
		if m.CollidesWithAny() {
			if w.mobs == nil {
				w.mobs = make(map[*entity.Mob]int32)
			}
			w.mobs[m] = int32(len(w.Boxes))
			if markShulkers {
				w.shulkers = append(w.shulkers, m.IsShulker())
			}
			w.Boxes = append(w.Boxes, entity.NewAABB(m.BlastBox()))
		}
	}
	for i, box := range w.Boxes {
		w.grid.Add(int32(i), box)
	}
	if len(w.Boxes) == 0 {
		return level
	}
	return w
}

// ForMob excludes the mover and its current ride tree, and for a shulker
// every shulker hull: its tick reads those live from the herd, and a hull
// recorded before the tick would still block where a carried shulker left.
// Called under mobsMu; the view lasts until the next mover's synchronous
// tick starts.
// vanilla: Entity.isPassengerOfSameVehicle
func (w *mobCollisionWorld) ForMob(m *entity.Mob) entity.World {
	w.view = w.Bodies
	b := &w.view
	w.skip = w.skip[:0]
	if m.IsShulker() {
		b.Drop = w.shulkers
	}
	w.riders = append(w.riders, m.RootVehicle())
	for len(w.riders) > 0 {
		last := len(w.riders) - 1
		other := w.riders[last]
		w.riders[last] = nil
		w.riders = w.riders[:last]
		if index, ok := w.mobs[other]; ok {
			w.skip = append(w.skip, index)
		}
		w.riders = append(w.riders, other.Passengers...)
	}
	b.Skip = w.skip
	return b
}
