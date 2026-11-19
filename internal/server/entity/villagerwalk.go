package entity

import (
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// villageCenters derives occupied village sections from the same loaded
// herd that owns the POIs. Unloaded owners do not contribute centers.
// vanilla: PoiManager.isVillageCenter
func (m *Mob) villageCenters(t *tickView) []Waypoint {
	var centers []Waypoint
	add := func(o *Mob) {
		if o.Type != TypeVillager || !o.Alive() || o.gone {
			return
		}
		for slot, p := range []*GlobalPos{o.vb.home, o.vb.jobSite, o.vb.potentialJobSite, o.vb.meetingPoint} {
			if !m.memHere(p) {
				continue
			}
			k := rules.POIKindOf(t.w.BlockAt(p.X, p.Y, p.Z))
			if k == rules.POINone {
				continue
			}
			if slot == 0 && k == rules.POIHome || slot == 1 && heldJobSite(o.Villager.Profession, k) ||
				slot == 2 && acquirableJobSite(o.Villager.Profession, k) || slot == 3 && k == rules.POIMeeting {
				centers = append(centers, sectionOf(p.at()))
			}
		}
	}
	add(m)
	for _, o := range t.others {
		if o != m {
			add(o)
		}
	}
	return centers
}

func sectionOf(p Waypoint) Waypoint { return Waypoint{p.X >> 4, p.Y >> 4, p.Z >> 4} }

// villageDistance is the section graph's Chebyshev distance, capped at seven.
// vanilla: PoiManager.DistanceTracker
func villageDistance(s Waypoint, centers []Waypoint) int32 {
	d := int32(7)
	for _, c := range centers {
		d = min(d, max(s.X-c.X, c.X-s.X, s.Y-c.Y, c.Y-s.Y, s.Z-c.Z, c.Z-s.Z))
	}
	return d
}

// closerVillageSection preserves SectionPos.cube's x, then y, then z tie order.
// vanilla: BehaviorUtils.findSectionClosestToVillage
func closerVillageSection(from Waypoint, centers []Waypoint) (Waypoint, bool) {
	best := villageDistance(from, centers)
	if best <= 1 {
		return from, false
	}
	target := from
	for z := from.Z - 2; z <= from.Z+2; z++ {
		for y := from.Y - 2; y <= from.Y+2; y++ {
			for x := from.X - 2; x <= from.X+2; x++ {
				p := Waypoint{x, y, z}
				if d := villageDistance(p, centers); d < best {
					best, target = d, p
				}
			}
		}
	}
	return target, target != from
}

// villageBoundPos strolls on land in a village, otherwise toward a closer section.
// vanilla: VillageBoundRandomStroll
func (m *Mob) villageBoundPos(t *tickView, xz, y int) (Waypoint, bool) {
	if s, ok := closerVillageSection(sectionOf(m.cell()), m.villageCenters(t)); ok {
		// Reflect the target so the shared directional search points toward it.
		return directedRandomPos(t.w, m, 2*m.X-float64(s.X*16+8)-0.5, 2*m.Z-float64(s.Z*16+8)-0.5, xz, y, false)
	}
	return landRandomPos(t.w, m, xz, y)
}

// closestVillagePos scores each batch of ten land picks by village distance.
// vanilla: GoToClosestVillage
func (m *Mob) closestVillagePos(t *tickView) (target Waypoint, found bool) {
	centers := m.villageCenters(t)
	d := villageDistance(sectionOf(m.cell()), centers)
	if d <= 1 {
		return target, false
	}
	for range 5 {
		p, ok := landRandomPosScored(func(_ World, p Waypoint) float64 {
			return -float64(villageDistance(sectionOf(p), centers))
		}, t.w, m, 15, 7)
		if !ok {
			continue
		}
		if next := villageDistance(sectionOf(p), centers); next <= d {
			target, found = p, true
			if next < d {
				break
			}
		}
	}
	return
}

// insidePos shuffles the surrounding 27 blocks, checking support at the
// target block and collision at the current body, not at a hypothetical body.
// vanilla: InsideBrownianWalk
func (m *Mob) insidePos(t *tickView) (Waypoint, bool) {
	if m.seesSkyAbove(t.w) || overlapsSolid(t.w, m.collideCtx(), m.mobBox()) {
		return Waypoint{}, false
	}
	c := m.cell()
	var picks [27]Waypoint
	i := 0
	for z := c.Z - 1; z <= c.Z+1; z++ {
		for y := c.Y - 1; y <= c.Y+1; y++ {
			for x := c.X - 1; x <= c.X+1; x++ {
				picks[i], i = Waypoint{x, y, z}, i+1
			}
		}
	}
	m.Rand.Shuffle(len(picks), func(i, j int) { picks[i], picks[j] = picks[j], picks[i] })
	for _, p := range picks {
		if !canSeeSkyAt(t.w, p.X, p.Y, p.Z) && registry.CollisionTopFull(t.w.BlockAt(p.X, p.Y, p.Z)) {
			return p, true
		}
	}
	return Waypoint{}, false
}

// restWanderGoal runs the homeless rest package's indoor walk, village walk
// and 20-40 tick pause; the higher-priority home search supplies its bed walk.
// vanilla: VillagerGoalPackages.getRestPackage
type restWanderGoal struct {
	m      *Mob
	target Waypoint
	reach  int32
	wait   int
}

func (g *restWanderGoal) Priority() int           { return priorityStroll }
func (g *restWanderGoal) Flags() GoalFlags        { return GoalMove }
func (g *restWanderGoal) RequiresEveryTick() bool { return true }
func (g *restWanderGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.vb.activity != actRest || m.vb.home != nil || m.Sleeping {
		return false
	}
	g.wait, g.reach = 0, 0
	var ok bool
	switch m.Rand.Intn(8) {
	case 0, 1, 2, 3:
		g.target, ok = m.insidePos(t)
	case 4, 5:
		g.reach = 4
		g.target, ok = m.closestVillagePos(t)
	default:
		g.wait = 20 + m.Rand.Intn(21)
		ok = true
	}
	return ok
}
func (g *restWanderGoal) Start(t *tickView) {
	if g.wait == 0 {
		p := g.target
		g.m.seekTo(t.w, float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5, villagerWalkPace, g.reach)
	}
}
func (g *restWanderGoal) ShouldContinue(*tickView) bool {
	return g.m.vb.activity == actRest && g.m.vb.home == nil && !g.m.Sleeping && (g.wait > 0 || !g.m.navDone())
}
func (g *restWanderGoal) Tick(*tickView) {
	if g.wait > 0 {
		g.wait--
	}
}
func (g *restWanderGoal) Stop(*tickView) { g.m.stopSeeking() }
