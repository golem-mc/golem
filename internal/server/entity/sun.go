package entity

import (
	"math"

	"github.com/golem-mc/golem/registry"
)

// The undead's sun goals from AbstractSkeleton.registerGoals. The wolf
// avoidance registered between them (AvoidEntityGoal(Wolf, 6.0f, 1.0, 1.2))
// is not here. The restriction holds no controls (it only sets a navigation
// mode); the flight holds MOVE only for its walk.

// restrictSunGoal is RestrictSunGoal at priority two: while bright outside
// the navigation avoids the sun, so every path is cut where the sky begins
// (trimForSun). It holds no controls and runs while canUse holds, which is
// also canContinueToUse's default.
type restrictSunGoal struct{ m *Mob }

func (g *restrictSunGoal) Priority() int    { return priorityRestrictSun }
func (g *restrictSunGoal) Flags() GoalFlags { return 0 }

// CanStart is RestrictSunGoal.canUse: bright outside, a bare head (a helmet
// blocks the sun), and ground navigation (which every mob here has).
func (g *restrictSunGoal) CanStart(t *tickView) bool {
	return t.w.BrightOutside() && g.m.Worn[wornHead].Empty()
}

func (g *restrictSunGoal) Start(*tickView) { g.m.navOf().nav.avoidSun = true }

func (g *restrictSunGoal) ShouldContinue(t *tickView) bool {
	return g.CanStart(t) // vanilla: Goal leaves canContinueToUse at canUse
}

func (g *restrictSunGoal) Tick(*tickView) {}

func (g *restrictSunGoal) Stop(*tickView) { g.m.navOf().nav.avoidSun = false }

// RequiresEveryTick is false; RestrictSunGoal does not override it.
func (g *restrictSunGoal) RequiresEveryTick() bool { return false }

// fleeSunGoal is FleeSunGoal(this, 1.0) at priority three: a burning
// skeleton under open sky walks once to shade. It holds MOVE, taking the
// legs from the stroll and ranged attack it outranks.
type fleeSunGoal struct {
	m *Mob
	// wanted is FleeSunGoal's wantedX/Y/Z, found by canUse.
	wantedX, wantedY, wantedZ float64
}

// fleeSunPace is FleeSunGoal's speed modifier, 1.0.
const fleeSunPace = 1.0

func (g *fleeSunGoal) Priority() int    { return priorityFleeSun }
func (g *fleeSunGoal) Flags() GoalFlags { return GoalMove }

// CanStart is FleeSunGoal.canUse in its order: no target; bright outside; on
// fire (Entity.isOnFire); sky visible over the feet; a bare head (a helmet
// shields it until it breaks); and a hide spot found.
func (g *fleeSunGoal) CanStart(t *tickView) bool {
	if g.m.TargetID != 0 {
		return false
	}
	if !t.w.BrightOutside() {
		return false
	}
	if g.m.FireTicks <= 0 {
		return false
	}
	if !g.m.seesSkyAbove(t.w) {
		return false
	}
	if !g.m.Worn[wornHead].Empty() {
		return false
	}
	return g.findHidePos(t.w)
}

func (g *fleeSunGoal) Start(t *tickView) {
	// vanilla: FleeSunGoal.start
	g.m.seekTo(t.w, g.wantedX, g.wantedY, g.wantedZ, fleeSunPace, navReach)
}

// ShouldContinue is FleeSunGoal.canContinueToUse: the navigation is not done
// (an empty path is done).
func (g *fleeSunGoal) ShouldContinue(*tickView) bool {
	return len(g.m.navOf().nav.path) > 0
}

// Tick does nothing; FleeSunGoal has no tick.
func (g *fleeSunGoal) Tick(*tickView) {}

// Stop does nothing; FleeSunGoal has no stop.
func (g *fleeSunGoal) Stop(*tickView) {}

// RequiresEveryTick is false; FleeSunGoal does not override it.
func (g *fleeSunGoal) RequiresEveryTick() bool { return false }

// findHidePos is FleeSunGoal.setWantedPos over getHidePos: ten random cells
// around the feet, kept when the cell does not see the sky and is not dark
// (a monster's walk-target value is under zero only where it is brighter
// than half the burn threshold). The spot is the cell's bottom centre.
func (g *fleeSunGoal) findHidePos(w World) bool {
	feet := g.m.cell()
	for range 10 {
		cand := Waypoint{
			X: feet.X + int32(g.m.Rand.Intn(20)) - 10,
			Y: feet.Y + int32(g.m.Rand.Intn(6)) - 3,
			Z: feet.Z + int32(g.m.Rand.Intn(20)) - 10,
		}
		if canSeeSkyAt(w, cand.X, cand.Y, cand.Z) {
			continue
		}
		if monsterWalkTargetValue(w, cand.X, cand.Y, cand.Z) >= 0 {
			continue
		}
		g.wantedX = float64(cand.X) + 0.5
		g.wantedY = float64(cand.Y)
		g.wantedZ = float64(cand.Z) + 0.5
		return true
	}
	return false
}

// canSeeSkyAt is BlockAndLightGetter's Level.canSeeSky: the stored sky light
// is 15. A cell just under a roof edge holds 14 and answers false, which is
// the shade the flight looks for and the trim cuts at.
func canSeeSkyAt(w World, x, y, z int32) bool {
	return w.SkyLightAt(x, y, z) == 15
}

// seesSkyAbove is canSeeSky at the feet cell (not the eyes), as
// FleeSunGoal.canUse asks.
func (m *Mob) seesSkyAbove(w World) bool {
	c := m.cell()
	return canSeeSkyAt(w, c.X, c.Y, c.Z)
}

// monsterWalkTargetValue is Monster.getWalkTargetValue, the negated
// getPathfindingCostFromLightLevels: below zero exactly where brightness is
// past half the burn threshold.
func monsterWalkTargetValue(w World, x, y, z int32) float64 {
	return 0.5 - cellLightDependentMagicValue(w, x, y, z)
}

// raiseCauldronNodes lifts every node standing in a cauldron one block, and
// the node after it to the same height unless that one is already higher.
// It edits the path in place.
// vanilla: PathNavigation.trimPath
func raiseCauldronNodes(w World, path []Waypoint) []Waypoint {
	for i, node := range path {
		if !registry.BlockOf(w.BlockAt(node.X, node.Y, node.Z)).InTag(registry.TagIDCauldrons) {
			continue
		}
		path[i].Y = node.Y + 1
		if i+1 < len(path) && node.Y >= path[i+1].Y {
			path[i+1].Y = node.Y + 1
		}
	}
	return path
}

// trimForSun is GroundPathNavigation.trimPath's sun half: while avoiding the
// sun, the path is cut at the first node that sees the sky
// (Path.truncateNodes), unless the mob already stands in the open.
func (m *Mob) trimForSun(w World, path []Waypoint) []Waypoint {
	o := m.navOf()
	if !o.nav.avoidSun {
		return path
	}
	if canSeeSkyAt(w, int32(math.Floor(o.X)), int32(math.Floor(o.Y+0.5)), int32(math.Floor(o.Z))) {
		return path
	}
	for i, node := range path {
		if canSeeSkyAt(w, node.X, node.Y, node.Z) {
			return path[:i]
		}
	}
	return path
}
