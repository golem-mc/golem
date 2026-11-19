package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/rules"
)

// The numbers of MoveThroughVillageGoal as Zombie.addBehaviourGoals builds it.
// vanilla: MoveThroughVillageGoal, Zombie.addBehaviourGoals
const (
	moveVillagePriority = 6
	moveVillagePoiDist  = 4  // distanceToPoi
	moveVillageFind     = 10 // PoiManager.find radius
	moveVillageVisited  = 15 // visited list cap
)

// moveThroughVillage walks a mob at night to a village point of interest it
// has not visited lately. CanStart plans the path into the navigation, so a
// mob with no way there never takes the goal; Start has nothing left to do.
// vanilla: MoveThroughVillageGoal
type moveThroughVillage struct {
	m       *Mob
	poi     Waypoint
	visited []Waypoint
}

func (g *moveThroughVillage) Priority() int           { return moveVillagePriority }
func (g *moveThroughVillage) Flags() GoalFlags        { return GoalMove }
func (g *moveThroughVillage) RequiresEveryTick() bool { return false }

// nearestFresh is PoiManager.find over unvisited, claimed village points of
// interest (Occupancy.IS_OCCUPIED).
func (g *moveThroughVillage) nearestFresh(x, y, z int32) (Waypoint, bool) {
	pois := g.m.Village.POIs(func(k rules.POIKind) bool { return k != rules.POINone }, x, y, z, moveVillageFind)
	i := slices.IndexFunc(pois, func(p Waypoint) bool {
		return !slices.Contains(g.visited, p) && g.m.Village.Claimed(p.X, p.Y, p.Z)
	})
	if i < 0 {
		return Waypoint{}, false
	}
	return pois[i], true
}

func (g *moveThroughVillage) CanStart(t *tickView) bool {
	m := g.m
	if len(g.visited) > moveVillageVisited {
		g.visited = g.visited[1:]
	}
	if m.Village == nil || t.w.BrightOutside() {
		return false
	}
	c := m.cell()
	if m.Village.SectionsToVillage(c.X, c.Y, c.Z, villageSections) > villageSections {
		return false
	}
	// found remembers each scored cell's point, so the pick needs no second find.
	type scored struct{ at, poi Waypoint }
	var found []scored
	score := func(_ World, p Waypoint) float64 {
		if m.Village.SectionsToVillage(p.X, p.Y, p.Z, 1) > 1 {
			return math.Inf(-1)
		}
		poi, ok := g.nearestFresh(p.X, p.Y, p.Z)
		if !ok {
			return math.Inf(-1)
		}
		found = append(found, scored{p, poi})
		return -float64(sq64(poi.X-c.X) + sq64(poi.Y-c.Y) + sq64(poi.Z-c.Z))
	}
	spot, ok := landRandomPosScored(score, t.w, m, villageStrollXZ, villageStrollY)
	if !ok {
		return false
	}
	i := slices.IndexFunc(found, func(f scored) bool { return f.at == spot })
	if i < 0 {
		var ok bool
		if g.poi, ok = g.nearestFresh(spot.X, spot.Y, spot.Z); !ok {
			return false
		}
	} else {
		g.poi = found[i].poi
	}
	return g.plan(t)
}

// plan installs the path to the point, or to a partial step towards it.
// vanilla: MoveThroughVillageGoal.canUse createPath and getPosTowards
func (g *moveThroughVillage) plan(t *tickView) bool {
	m := g.m
	if m.seekTo(t.w, float64(g.poi.X)+0.5, float64(g.poi.Y), float64(g.poi.Z)+0.5, 1, 0) {
		return true
	}
	target := [2]float64{float64(g.poi.X) + 0.5, float64(g.poi.Z) + 0.5}
	cell, ok := posTowardsWalkable(t.w, m, 10, 7, target, math.Pi/2)
	return ok && m.seekTo(t.w, float64(cell.X)+0.5, float64(cell.Y), float64(cell.Z)+0.5, 1, 0)
}

func sq64(v int32) int64 { return int64(v) * int64(v) }

// reached is BlockPos.closerToCenterThan the mob.
func (g *moveThroughVillage) reached(dist float64) bool {
	m := g.m
	dx, dy, dz := float64(g.poi.X)+0.5-m.X, float64(g.poi.Y)+0.5-m.Y, float64(g.poi.Z)+0.5-m.Z
	return dx*dx+dy*dy+dz*dz < dist*dist
}

func (g *moveThroughVillage) Start(*tickView) {}

func (g *moveThroughVillage) ShouldContinue(*tickView) bool {
	return !g.m.navDone() && !g.reached(g.m.width()+moveVillagePoiDist)
}

func (g *moveThroughVillage) Tick(*tickView) {}

func (g *moveThroughVillage) Stop(*tickView) {
	if g.m.navDone() || g.reached(moveVillagePoiDist) {
		g.visited = append(g.visited, g.poi)
	}
}
