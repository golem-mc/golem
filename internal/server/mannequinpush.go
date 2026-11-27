package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

type mannequinPushBody struct {
	mob       *mobEntity
	mannequin *mannequinEntity
	boat      *boatEntity
	cart      *minecartEntity
	dx, dz    float64
	takes     bool
}

type mannequinPushes struct {
	worlds map[*world.World]*mannequinPushWorld
	mobs   []mobPushShare
}

type mannequinPushWorld struct {
	entity.PushCrowd
	bodies []mannequinPushBody
	// reach holds each live mannequin's box inflated by mannequinPushReach:
	// a body outside all of them cannot overlap one after its move.
	reach     []entity.AABB
	reachGrid entity.BoxGrid
}

// mannequinPushReach is how far past a mannequin's box a body may still push
// it this tick; a deliberate bound, not vanilla's. Normal movement (walking,
// boats, minecarts) is well under it; a body moving further in one tick (a
// fall near terminal velocity, an elytra dash) can miss the push that tick.
const mannequinPushReach = 2.0

func (p *mannequinPushWorld) inReach(box entity.AABB) bool {
	found := false
	p.reachGrid.Query(box, func(i int32) {
		if !found && box.Crosses(p.reach[i]) {
			found = true
		}
	})
	return found
}

func (p *mannequinPushWorld) add(box entity.AABB, team string, rule entity.CollisionRule, body mannequinPushBody) {
	p.Add(entity.PushBody{Box: box, Team: team, Rule: rule})
	p.bodies = append(p.bodies, body)
}

// mannequinPushInputs snapshots other stores before the mannequin lock is taken.
// vanilla: LivingEntity.pushEntities, EntitySelector.pushableBy
func (s *Server) mannequinPushInputs(teams pushTeams) *mannequinPushes {
	pushes := s.mannequinPushScratch
	if pushes == nil {
		pushes = &mannequinPushes{worlds: make(map[*world.World]*mannequinPushWorld)}
		s.mannequinPushScratch = pushes
	}
	for _, p := range pushes.worlds {
		p.Reset()
		clear(p.bodies)
		p.bodies = p.bodies[:0]
		p.reach = p.reach[:0]
		p.reachGrid.Reset()
	}
	clear(pushes.mobs)
	pushes.mobs = pushes.mobs[:0]
	byWorld := pushes.worlds
	s.lockTick(&s.mannequinsMu)
	for _, ent := range s.mannequins {
		if ent.mannequin.Dead || ent.mannequin.Dying() {
			continue
		}
		p := byWorld[ent.w]
		if p == nil {
			p = &mannequinPushWorld{}
			p.Grid.Cell = cartPushCell
			p.reachGrid.Cell = cartPushCell
			byWorld[ent.w] = p
		}
		team, rule := teams.of(ent.uuid)
		box := entity.NewAABB(ent.mannequin.BlastBox())
		p.reachGrid.Add(int32(len(p.reach)), box.Inflate(mannequinPushReach))
		p.reach = append(p.reach, box.Inflate(mannequinPushReach))
		p.add(box, team, rule, mannequinPushBody{mannequin: ent, takes: true})
	}
	s.unlockTick(&s.mannequinsMu)
	add := func(w *world.World, box entity.AABB, id uuid.UUID, b mannequinPushBody) {
		if p := byWorld[w]; p != nil && len(p.bodies) > 0 && p.inReach(box) {
			team, rule := teams.of(id)
			p.add(box, team, rule, b)
		}
	}
	s.mu.Lock()
	clients := s.clientsLocked()
	s.mu.Unlock()
	for _, c := range clients {
		if c.player == nil || c.player.Vitals().Dead || c.player.GameMode() == protocol.GameModeSpectator {
			continue
		}
		pos := c.player.Position()
		dims := c.dims(c.player.Appearance().Pose)
		half := dims.Width / 2
		if p := byWorld[c.theWorld()]; p != nil && len(p.bodies) > 0 {
			team := teams.names[c.player.Name]
			p.add(entity.NewAABB(pos.X-half, pos.Y, pos.Z-half, pos.X+half, pos.Y+dims.Height, pos.Z+half), team, teams.rules[team], mannequinPushBody{})
		}
	}
	s.lockTick(&s.mobsMu)
	for _, dim := range s.allDims() {
		p := byWorld[dim.w]
		if p == nil || len(p.bodies) == 0 {
			continue
		}
		h := dim.mobs
		h.mobsNearBoxes(p.reach, func(m *entity.Mob) bool {
			if !m.Pushable() || m.Type == entity.TypeVex {
				return true
			}
			ent := h.record(m)
			// Herd entries can outlive their tracking records, e.g. on hive entry.
			if ent == nil {
				return true
			}
			add(dim.w, entity.NewAABB(m.BlastBox()), ent.uuid, mannequinPushBody{mob: ent, takes: !m.RidesVehicle})
			return true
		})
	}
	s.unlockTick(&s.mobsMu)
	s.lockTick(&s.boatsMu)
	for _, ent := range s.boats {
		if !ent.boat.Dead {
			add(ent.w, entity.NewAABB(ent.boat.Box()), ent.uuid, mannequinPushBody{boat: ent, takes: len(ent.passengers) == 0})
		}
	}
	s.unlockTick(&s.boatsMu)
	s.lockTick(&s.minecartsMu)
	for _, ent := range s.minecarts {
		if !ent.cart.Dead {
			add(ent.w, entity.NewAABB(ent.cart.Box()), ent.uuid, mannequinPushBody{cart: ent, takes: len(ent.passengers) == 0})
		}
	}
	s.unlockTick(&s.minecartsMu)
	return pushes
}

// push keeps both halves of the pair; another mannequin runs its own half in
// this pass, while the other kinds' ticks do not visit mannequins.
func (p *mannequinPushWorld) push(ent *mannequinEntity, teams pushTeams) {
	if p == nil || ent.mannequin.Dead || ent.mannequin.Dying() {
		return
	}
	m := ent.mannequin
	box := entity.NewAABB(m.BlastBox())
	team, rule := teams.of(ent.uuid)
	p.Grid.Query(box, func(i int32) {
		body, other := &p.bodies[i], p.Bodies[i]
		if body.mannequin == ent || !box.Crosses(other.Box) || !entity.PushableBy(team, rule, other.Team, other.Rule) {
			return
		}
		minX, _, minZ, maxX, _, maxZ := other.Box.Corners()
		px, pz := entity.PushApart(m.X, m.Z, (minX+maxX)/2, (minZ+maxZ)/2)
		n := 2.0
		if body.mannequin != nil {
			n = 1
		}
		m.Push(-px*n, 0, -pz*n)
		if body.takes {
			body.dx += px * n
			body.dz += pz * n
		}
	})
}

// apply delivers queued shares without nesting entity-store locks.
func (s *Server) applyMannequinPushes(pushes *mannequinPushes) {
	for w, p := range pushes.worlds {
		for _, b := range p.bodies {
			if b.dx == 0 && b.dz == 0 {
				continue
			}
			switch {
			case b.mob != nil:
				pushes.mobs = append(pushes.mobs, mobPushShare{w: w, ent: b.mob, dx: b.dx, dz: b.dz})
			case b.mannequin != nil:
				s.lockTick(&s.mannequinsMu)
				b.mannequin.mannequin.Push(b.dx, 0, b.dz)
				s.unlockTick(&s.mannequinsMu)
			case b.boat != nil:
				s.lockTick(&s.boatsMu)
				b.boat.boat.Push(b.dx, 0, b.dz)
				s.unlockTick(&s.boatsMu)
			case b.cart != nil:
				s.lockTick(&s.minecartsMu)
				b.cart.cart.Push(b.dx, 0, b.dz)
				s.unlockTick(&s.minecartsMu)
			}
		}
	}
	s.applyMobShares(pushes.mobs)
}
