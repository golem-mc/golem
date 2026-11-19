package entity

import "math"

// homeState is a mob's home position and radius, the area its random walks
// are kept in; the zero value is no home (vanilla's radius -1).
// vanilla: Mob.homePosition, Mob.homeRadius
type homeState struct {
	center Waypoint
	radius int32
	set    bool
}

// RestrictTo sets the mob's home to the block (x, y, z) and the radius, in
// blocks.
// vanilla: Mob.setHomeTo
func (m *Mob) RestrictTo(x, y, z, radius int32) {
	m.home = homeState{center: Waypoint{x, y, z}, radius: radius, set: true}
}

// HomeRestriction is the home block and radius RestrictTo set, with false
// when the mob has none.
// vanilla: Mob.hasHome
func (m *Mob) HomeRestriction() (x, y, z, radius int32, ok bool) {
	h := &m.home
	return h.center.X, h.center.Y, h.center.Z, h.radius, h.set
}

// withinHome is Mob.isWithinHome for a cell: the block distance to the home
// is under the radius.
func (m *Mob) withinHome(c Waypoint) bool {
	h := &m.home
	if !h.set {
		return true
	}
	dx, dy, dz := float64(c.X-h.center.X), float64(c.Y-h.center.Y), float64(c.Z-h.center.Z)
	return dx*dx+dy*dy+dz*dz < float64(h.radius)*float64(h.radius)
}

// withinHomeAt is Mob.isWithinHome for a drawn position: the distance from
// the centre of the home block to the position is under the radius.
// vanilla: Mob.isWithinHome(Vec3), Vec3i.distToCenterSqr
func (m *Mob) withinHomeAt(x, y, z float64) bool {
	h := &m.home
	if !h.set {
		return true
	}
	dx := float64(h.center.X) + 0.5 - x
	dy := float64(h.center.Y) + 0.5 - y
	dz := float64(h.center.Z) + 0.5 - z
	r := float64(h.radius)
	return dx*dx+dy*dy+dz*dz < r*r
}

// homeRestricted is GoalUtils.mobRestricted: whether the mob is close enough
// to home for its picked walk to be kept inside.
func (m *Mob) homeRestricted(horizontalDist int) bool {
	h := &m.home
	if !h.set {
		return false
	}
	reach := float64(h.radius) + float64(horizontalDist) + 1
	dx, dy, dz := float64(h.center.X)+0.5-m.X, float64(h.center.Y)+0.5-m.Y, float64(h.center.Z)+0.5-m.Z
	return dx*dx+dy*dy+dz*dz < reach*reach
}

// offsetTowardHome is RandomPos.generateRandomPosTowardDirection: the offset
// cell, shifted toward home by up to half the horizontal reach when a home
// is set.
func (m *Mob) offsetTowardHome(dx, dy, dz, horizontalDist int) Waypoint {
	xt, zt := float64(dx), float64(dz)
	if h := &m.home; h.set && horizontalDist > 1 {
		half := float64(horizontalDist) / 2
		if m.X > float64(h.center.X) {
			xt -= m.Rand.Float64() * half
		} else {
			xt += m.Rand.Float64() * half
		}
		if m.Z > float64(h.center.Z) {
			zt -= m.Rand.Float64() * half
		} else {
			zt += m.Rand.Float64() * half
		}
	}
	return Waypoint{
		int32(math.Floor(xt + m.X)), int32(math.Floor(float64(dy) + m.Y)), int32(math.Floor(zt + m.Z)),
	}
}

// homeGoal is MoveTowardsRestrictionGoal: outside its home, the mob walks at
// pace to a cell on the way back to it.
// vanilla: MoveTowardsRestrictionGoal
type homeGoal struct {
	m        *Mob
	priority int
	pace     float64
	target   Waypoint
}

// The reach of MoveTowardsRestrictionGoal's DefaultRandomPos.getPosTowards.
const (
	homeGoalSpread  = 16
	homeGoalSpreadY = 7
)

func (g *homeGoal) Priority() int    { return g.priority }
func (g *homeGoal) Flags() GoalFlags { return GoalMove }

func (g *homeGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.withinHome(m.cell()) {
		return false
	}
	home := [2]float64{float64(m.home.center.X) + 0.5, float64(m.home.center.Z) + 0.5}
	var ok bool
	g.target, ok = posTowardsWalkable(t.w, m, homeGoalSpread, homeGoalSpreadY, home, math.Pi/2)
	return ok
}

func (g *homeGoal) Start(t *tickView) {
	g.m.seekTo(t.w, float64(g.target.X)+0.5, float64(g.target.Y), float64(g.target.Z)+0.5, g.pace, navReach)
}

func (g *homeGoal) ShouldContinue(*tickView) bool { return len(g.m.navOf().nav.path) > 0 }
func (g *homeGoal) Tick(*tickView)                {}
func (g *homeGoal) Stop(*tickView)                { g.m.stopSeeking() }
func (g *homeGoal) RequiresEveryTick() bool       { return false }
