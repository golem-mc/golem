package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// mobDisk is one replace_disk entry a mob's or stand's location_changed run
// wrote, to apply after the entity locks.
type mobDisk struct {
	w     *world.World
	r     *registry.EnchReplace
	level int32
	t     enchantTarget
}

// diskOf splits a replace_disk off an effect copy for the batch, so
// applyPackEffects does not run it a second time.
func diskOf(w *world.World, e *registry.EnchEntityEffect, level int32, t enchantTarget) (mobDisk, bool) {
	if e.Replace == nil || e.Replace.Radius == nil {
		return mobDisk{}, false
	}
	d := mobDisk{w: w, r: e.Replace, level: level, t: t}
	e.Replace = nil
	return d, true
}

// applyDisks writes one batch's disks, gathering each world's bodies once and
// filing them in the body grid so a disk's unobstructed test sees only bodies
// whose boxes can reach its cells.
func (s *Server) applyDisks(disks []mobDisk) {
	var (
		w        *world.World
		bodies   []rules.BodyBox
		grid     *bodyGrid
		near     []rules.BodyBox
		indices  []int
		maxReach float64
	)
	obstructed := func(x, y, z int32) bool { return rules.BodyBoxesReach(near, x, y, z) }
	for _, d := range disks {
		if d.w != w {
			if grid != nil {
				grid.release()
			}
			w = d.w
			bodies = s.diskBodies(w)
			maxReach = 0
			grid = takeBodyGrid()
			for i, body := range bodies {
				grid.add(i, body.X, body.Z)
				maxReach = max(maxReach, body.Width/2)
			}
		}
		// The disk selects block centres, but obstruction tests whole blocks.
		radius := float64(rules.EnchLevelValueAt(d.r.Radius, d.level)) + 0.5 + maxReach
		indices = grid.near(d.t.x-radius, d.t.z-radius, d.t.x+radius, d.t.z+radius, indices[:0])
		near = near[:0]
		for _, i := range indices {
			body := bodies[i]
			if body.X+body.Width/2 > d.t.x-radius &&
				body.X-body.Width/2 < d.t.x+radius &&
				body.Z+body.Width/2 > d.t.z-radius &&
				body.Z-body.Width/2 < d.t.z+radius {
				near = append(near, body)
			}
		}
		for _, c := range s.diskCells(w, d.r, d.level, d.t, obstructed) {
			s.writeEnchantBlock(w, c[0], c[1], c[2], s.replaceState(d.r, c[0], c[1], c[2]), d.r.GameEvent, d.t)
		}
	}
	if grid != nil {
		grid.release()
	}
}

// diskBodies gathers the live bodies Server.obstructed walks, each list under
// its own lock, skipping spectators and markers.
// vanilla: EntityGetter.isUnobstructed
func (s *Server) diskBodies(w *world.World) []rules.BodyBox {
	var bodies []rules.BodyBox
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	for _, c := range clients {
		if c.theWorld() != w || c.player.GameMode() == protocol.GameModeSpectator {
			continue
		}
		pos := c.player.Position()
		d := c.dims(c.player.Appearance().Pose)
		bodies = append(bodies, rules.BodyBox{X: pos.X, Y: pos.Y, Z: pos.Z, Width: d.Width, Height: d.Height})
	}
	mobBox := func(m *entity.Mob) rules.BodyBox {
		return rules.BodyBox{X: m.X, Y: m.Y, Z: m.Z, Width: m.BbWidth(), Height: m.BbHeight()}
	}
	s.mobsMu.Lock()
	herd := s.herdOf(w)
	bodies = slices.Grow(bodies, len(herd.herd)+len(herd.dying))
	for _, m := range herd.herd {
		bodies = append(bodies, mobBox(m))
	}
	for _, corpse := range herd.dying {
		bodies = append(bodies, mobBox(corpse.ent.mob))
	}
	s.mobsMu.Unlock()
	s.armorStandsMu.Lock()
	for _, ent := range s.armorStands {
		if ent.w != w || ent.stand.Dead || ent.stand.Marker {
			continue
		}
		bodies = append(bodies, rules.BodyBoxOfBox(ent.stand.BlastBox()))
	}
	s.armorStandsMu.Unlock()
	s.boatsMu.Lock()
	for _, it := range s.boats {
		if it.w == w && !it.boat.Dead {
			bodies = append(bodies, rules.BodyBoxOfBox(it.boat.BlastBox()))
		}
	}
	s.boatsMu.Unlock()
	s.fallingMu.Lock()
	for _, it := range s.fallings {
		if it.w == w && !it.fb.Dead {
			bodies = append(bodies, rules.BodyBoxOfBox(it.fb.BlastBox()))
		}
	}
	s.fallingMu.Unlock()
	s.tntMu.Lock()
	for _, it := range s.tnts {
		if it.w == w && !it.tnt.Dead {
			bodies = append(bodies, rules.BodyBoxOfBox(it.tnt.BlastBox()))
		}
	}
	s.tntMu.Unlock()
	s.minecartsMu.Lock()
	for _, it := range s.minecarts {
		if it.w == w && !it.cart.Dead {
			bodies = append(bodies, rules.BodyBoxOfBox(it.cart.Box()))
		}
	}
	s.minecartsMu.Unlock()
	s.endCrystalsMu.Lock()
	for _, ent := range s.endCrystals {
		if ent.w == w && !ent.crystal.Dead {
			bodies = append(bodies, rules.BodyBoxOfBox(ent.crystal.BlastBox()))
		}
	}
	s.endCrystalsMu.Unlock()
	s.mannequinsMu.Lock()
	for _, ent := range s.mannequins {
		if ent.w == w && !ent.mannequin.Dead {
			bodies = append(bodies, rules.BodyBoxOfBox(ent.mannequin.BlastBox()))
		}
	}
	s.mannequinsMu.Unlock()
	return bodies
}
