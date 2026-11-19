package server

import (
	"math"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// boatPusher is one pushable body's push inputs, frozen with its box.
type boatPusher struct {
	x, minY, z float64
	team       string
	rule       entity.CollisionRule
	// pushes is whether the body's own push step runs; takes whether a
	// mob's share reaches its velocity: a rider's is zeroed by its ride and
	// a vehicle takes none. vanilla: Entity.rideTick, Entity.push
	pushes, takes bool
}

// boatHullCell is the side of the hull grid's columns: hulls moored side
// by side would crowd a 16-block one.
const boatHullCell = 4

// boatStepSlack is how far, past its speed when filed, a hull moves in the
// rest of the tick: the paddles' 0.04 and 0.05 from each neighbour that
// pushes it before its step. A deliberate bound, not vanilla's: more than
// nineteen hulls pushing one would outrun it.
const boatStepSlack = 1.0

// fileHulls files each of w's live hulls by its centre, under the boat lock
// so that no driver's move lands between the filing and the hulls' pushes;
// hullSlack is the most any moves sideways in a tick.
func (wb *boatWorldBodies) fileHulls(w *world.World) {
	for i, it := range wb.hulls {
		b := it.boat
		if b.Dead || it.w != w {
			continue
		}
		wb.hullGrid.Add(int32(i), entity.NewAABB(b.X, b.Y, b.Z, b.X, b.Y, b.Z))
		wb.hullSlack = max(wb.hullSlack, math.Abs(b.VX), math.Abs(b.VZ))
	}
}

// holds reports whether the hull seats the mob or the client.
func (it *boatEntity) holds(m *mobEntity, c *client) bool {
	for _, p := range it.passengers {
		if m != nil && p.ent == m || c != nil && p.c == c {
			return true
		}
	}
	return false
}

// push runs, after the hull's step and boarding, its sweep's push of every
// body it did not seat and every other hull it meets, and the push steps of
// the bodies overlapping it, standing for those bodies' own ticks. A hull
// takes its share now unless it carries someone, a mob's share is queued and
// a player's is its client's. The sweep pushes a body standing no higher
// than the hull's floor and a hull whose floor is below this one's top.
// vanilla: AbstractBoat.tick, AbstractBoat.push, Entity.push, LivingEntity.pushEntities
func (fx *boatEffects) push(it *boatEntity, wb *boatWorldBodies, teams pushTeams) {
	if wb == nil {
		return
	}
	b := it.boat
	team, rule := teams.of(it.uuid)
	free := len(it.passengers) == 0
	box := entity.NewAABB(b.Box())
	// vanilla inflates by 0.2f sideways and shrinks by 0.01 upright.
	const grow = float64(float32(0.2))
	sweep := entity.NewAABB(b.X-entity.BoatHalfWidth-grow, b.Y+0.01, b.Z-entity.BoatHalfWidth-grow,
		b.X+entity.BoatHalfWidth+grow, b.Y+entity.BoatHeight-0.01, b.Z+entity.BoatHalfWidth+grow)
	if len(wb.boxes) > 0 {
		fx.pushBodies(it, wb, team, rule, free, box, sweep)
	}
	top := b.Y + entity.BoatHeight - 0.01
	// A hull filed by its centre has since moved by at most one tick's step.
	wb.hullGrid.QueryPoints(sweep.Inflate(entity.BoatHalfWidth+wb.hullSlack+boatStepSlack), func(i int32) {
		// The sweep meets a hull whose centre is within both half widths
		// and the growth on X and Z and whose floor is below the sweep's
		// top, which keeps AbstractBoat.push's rule too, and whose top is
		// above the sweep's floor.
		const reach = 2*entity.BoatHalfWidth + grow
		o := wb.hulls[i]
		ob := o.boat
		if o == it || ob.Dead || ob.Y >= top || ob.Y+entity.BoatHeight <= b.Y+0.01 ||
			math.Abs(ob.X-b.X) >= reach || math.Abs(ob.Z-b.Z) >= reach {
			return
		}
		if oTeam, oRule := teams.of(o.uuid); !entity.PushableBy(team, rule, oTeam, oRule) {
			return
		}
		px, pz := entity.PushApart(b.X, b.Z, ob.X, ob.Z)
		if free {
			b.Push(-px, 0, -pz)
		}
		if len(o.passengers) == 0 {
			ob.Push(px, 0, pz)
		}
	})
}

// pushBodies is push over the bodies: the hull's sweep and the bodies' own
// push steps onto its box.
func (fx *boatEffects) pushBodies(it *boatEntity, wb *boatWorldBodies, team string, rule entity.CollisionRule, free bool, box, sweep entity.AABB) {
	b := it.boat
	wb.grid.Query(sweep, func(i int32) {
		body, p := wb.boxes[i], &wb.pushers[i]
		if p.minY > b.Y || it.holds(wb.mobs[i], wb.clients[i]) {
			return
		}
		n := 0.0
		if body.Crosses(sweep) && entity.PushableBy(team, rule, p.team, p.rule) {
			n++
		}
		if p.pushes && body.Crosses(box) && entity.PushableBy(p.team, p.rule, team, rule) {
			n++
		}
		if n == 0 {
			return
		}
		px, pz := entity.PushApart(b.X, b.Z, p.x, p.z)
		if free {
			b.Push(-px*n, 0, -pz*n)
		}
		if m := wb.mobs[i]; m != nil && p.takes {
			fx.mobShares = append(fx.mobShares, mobPushShare{w: it.w, ent: m, dx: px * n, dz: pz * n})
		}
	})
}
