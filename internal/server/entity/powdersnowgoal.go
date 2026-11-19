package entity

import (
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// walksOnPowderSnowByKind reports whether the kind is on the
// powder_snow_walkable_mobs tag.
func (m *Mob) walksOnPowderSnowByKind() bool {
	return m.inTag(registry.EntityTypeTagIDPowderSnowWalkableMobs)
}

// climbPowderSnowGoal is ClimbOnTopOfPowderSnowGoal: a sunken powder-snow
// walker keeps jumping while the cell above is powder snow or empty, which
// lifts it back on top.
// vanilla: ClimbOnTopOfPowderSnowGoal
type climbPowderSnowGoal struct{ m *Mob }

func (g *climbPowderSnowGoal) Priority() int { return priorityPanic }
func (g *climbPowderSnowGoal) Flags() GoalFlags {
	return GoalJump
}

// CanStart is ClimbOnTopOfPowderSnowGoal.canUse.
func (g *climbPowderSnowGoal) CanStart(t *tickView) bool {
	m := g.m
	// freezeStep has already moved this tick's read into lastInPowderSnow,
	// so wasInPowderSnow is the previous tick's isInPowderSnow.
	if !m.wasInPowderSnow || !m.walksOnPowderSnowByKind() {
		return false
	}
	cell := m.cell()
	above := t.w.BlockAt(cell.X, cell.Y+1, cell.Z)
	return rules.IsPowderSnow(above) || len(world.CollisionBoxes(above)) == 0
}

func (g *climbPowderSnowGoal) Start(*tickView) {}

// ShouldContinue is the canContinueToUse the base Goal supplies.
func (g *climbPowderSnowGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }

func (g *climbPowderSnowGoal) Tick(*tickView) { g.m.jump = true }
func (g *climbPowderSnowGoal) Stop(*tickView) {}

func (g *climbPowderSnowGoal) RequiresEveryTick() bool { return true }
