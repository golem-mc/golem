package entity

import "math"

// The village stroll's numbers.
// vanilla: StrollThroughVillageGoal, Fox.registerGoals
const (
	foxVillageInterval = 200 // FoxStrollThroughVillageGoal(this, 32, 200)
	villageStrollNear  = 6   // isCloseToVillage's sectionDistance
	villageStrollXZ    = 15  // LandRandomPos.getPos(mob, 15, 7, ...)
	villageStrollY     = 7
	villageStrollLeg   = 10.0 // the leg's length and the distance counted as arrived
	villageSections    = 6    // ServerLevel.isCloseToVillage's cap
)

// inVillage is ServerLevel.isVillage at the feet: a village point of interest
// within one section.
func (m *Mob) inVillage() bool {
	if m.Village == nil {
		return false
	}
	c := m.cell()
	return m.Village.SectionsToVillage(c.X, c.Y, c.Z, 1) <= 1
}

// foxCanMove is Fox.FoxStrollThroughVillageGoal.canFoxMove.
func (m *Mob) foxCanMove() bool {
	return !m.Fox.Sleeping && !m.Fox.Sitting && !m.Fox.Defending && m.TargetID == 0
}

// strollThroughVillage is StrollThroughVillageGoal: by night near a village,
// pick a spot that is closer to one and walk a leg toward it.
// vanilla: StrollThroughVillageGoal
type strollThroughVillage struct {
	m        *Mob
	interval int
	wanted   Waypoint
	has      bool
}

func (g *strollThroughVillage) Priority() int           { return foxPriorityVillage }
func (g *strollThroughVillage) Flags() GoalFlags        { return GoalMove }
func (g *strollThroughVillage) RequiresEveryTick() bool { return false }

func (g *strollThroughVillage) CanStart(t *tickView) bool {
	m := g.m
	if m.Village == nil || t.w.BrightOutside() || m.Rand.Intn(g.interval) != 0 {
		return false
	}
	c := m.cell()
	if m.Village.SectionsToVillage(c.X, c.Y, c.Z, villageStrollNear) > villageStrollNear {
		return false
	}
	score := func(_ World, p Waypoint) float64 {
		return -float64(m.Village.SectionsToVillage(p.X, p.Y, p.Z, villageSections))
	}
	g.wanted, g.has = landRandomPosScored(score, t.w, m, villageStrollXZ, villageStrollY)
	return g.has
}

func (g *strollThroughVillage) Start(*tickView) {}

func (g *strollThroughVillage) ShouldContinue(*tickView) bool {
	o := g.m.navOf()
	return g.has && !g.m.navDone() && o.nav.end == g.wanted
}

// Tick walks a leg of ten blocks toward the spot, on the line through it that
// runs 0.4 of the distance past it. The heightmap ignores leaves in vanilla
// and counts them here.
func (g *strollThroughVillage) Tick(t *tickView) {
	m := g.m
	if !g.has || !m.navDone() {
		return
	}
	cx, cy, cz := float64(g.wanted.X)+0.5, float64(g.wanted.Y)+0.5, float64(g.wanted.Z)+0.5
	if sq(m.X-cx)+sq(m.Y-cy)+sq(m.Z-cz) < villageStrollLeg*villageStrollLeg {
		return
	}
	bx, by, bz := cx, float64(g.wanted.Y), cz
	dx, dy, dz := (m.X-bx)*0.4+bx-m.X, (m.Y-by)*0.4+by-m.Y, (m.Z-bz)*0.4+bz-m.Z
	n := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if n == 0 {
		return
	}
	tx := cellAt(m.X+dx/n*villageStrollLeg, 0, m.Z+dz/n*villageStrollLeg)
	if !m.seekTo(t.w, float64(tx.X)+0.5, float64(t.w.MotionBlockingHeight(tx.X, tx.Z)+1), float64(tx.Z)+0.5, 1, navReach) {
		g.moveRandomly(t)
	}
}

func (g *strollThroughVillage) moveRandomly(t *tickView) {
	m := g.m
	x := m.cell().X - 8 + int32(m.Rand.Intn(16))
	z := m.cell().Z - 8 + int32(m.Rand.Intn(16))
	m.seekTo(t.w, float64(x)+0.5, float64(t.w.MotionBlockingHeight(x, z)+1), float64(z)+0.5, 1, navReach)
}

func (g *strollThroughVillage) Stop(*tickView) {}

func sq(v float64) float64 { return v * v }
