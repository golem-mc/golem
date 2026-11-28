package entity

import (
	"slices"
)

// villagerKids holds unsaved visible-child and interaction memories.
// vanilla: VillagerBabiesSensor, PlayTagWithOtherKids
type villagerKids struct {
	scan        int
	visible     []*Mob
	interaction *Mob
}

// villagerKidsStep samples children every sensor interval, nearest first.
func (m *Mob) villagerKidsStep(t *tickView) {
	k := &m.vb.kids
	if !m.IsBaby() {
		k.visible, k.interaction = nil, nil
		return
	}
	// Deliberately limit this play-only memory to PLAY: other activities use
	// goal-local targets rather than vanilla's shared INTERACTION_TARGET.
	if m.vb.activity != actPlay || k.interaction != nil && (!k.interaction.Alive() || !t.containsMob(k.interaction)) {
		k.interaction = nil
	}
	if k.scan--; k.scan > 0 {
		return
	}
	k.scan = villagerDangerScan
	clear(k.visible)
	k.visible = k.visible[:0]
	for _, o := range t.villagerMobs() {
		if o == m || !o.IsBaby() || !o.Alive() {
			continue
		}
		visibility := m.visibilityRange(&Player{Invisible: o.hasEffect(mobEffectInvisibility), Worn: o.Worn})
		if Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z) <= visibility*visibility && m.sees(t, o) {
			k.visible = append(k.visible, o)
		}
	}
	slices.SortStableFunc(k.visible, func(a, b *Mob) int {
		da, db := Dist2(m.X, m.Y, m.Z, a.X, a.Y, a.Z), Dist2(m.X, m.Y, m.Z, b.X, b.Y, b.Z)
		if da < db {
			return -1
		}
		if da > db {
			return 1
		}
		return 0
	})
}

// tagTarget first joins the least crowded chase (up to five chasers), then
// falls back to the nearest child. The herd order breaks tied counts.
// vanilla: PlayTagWithOtherKids.findSomeoneBeingChased
func (m *Mob) tagTarget() (target *Mob, fleeing bool) {
	kids := m.vb.kids.visible
	for _, o := range kids {
		if o.vb.kids.interaction == m {
			return nil, true
		}
	}
	best := 6
	for _, o := range kids {
		candidate := o.vb.kids.interaction
		if candidate == nil || !candidate.Alive() {
			continue
		}
		count := 0
		for _, chaser := range kids {
			if chaser.vb.kids.interaction == candidate {
				count++
			}
		}
		if count > 0 && count <= 5 && count < best {
			target, best = candidate, count
		}
	}
	if target == nil && len(kids) > 0 {
		target = kids[0]
	}
	return target, false
}

// kidPlayGoal adapts the PLAY package's walk target and RunOne to the goal
// selector. A walk keeps its target until arrival, failure or the sink timeout.
// vanilla: VillagerGoalPackages.getPlayPackage, MoveToTargetSink(80, 120)
type kidPlayGoal struct {
	m           *Mob
	target      *Mob
	look        villagerLookTarget
	destination Waypoint
	pace        float64
	reach       int32
	left        int
	jumping     *jumpOnBedGoal
	waiting     bool
}

func (g *kidPlayGoal) Priority() int           { return priorityPoiStrol }
func (g *kidPlayGoal) Flags() GoalFlags        { return GoalMove | GoalLook | GoalJump }
func (g *kidPlayGoal) RequiresEveryTick() bool { return true }
func (g *kidPlayGoal) active() bool {
	return g.m.IsBaby() && g.m.vb.activity == actPlay && !g.m.Sleeping
}

func (g *kidPlayGoal) CanStart(t *tickView) bool {
	if !g.active() || len(g.m.navOf().nav.path) != 0 {
		return false
	}
	g.target, g.jumping, g.waiting = nil, nil, false
	g.look = villagerLookTarget{}
	g.pace, g.reach = 0.6, 1
	if len(g.m.vb.kids.visible) > 0 {
		if g.m.Rand.Intn(10) != 0 {
			return false
		}
		target, flee := g.m.tagTarget()
		if !flee {
			g.target = target
			return target != nil
		}
		// A tagged child flees only to land inside an occupied village.
		centers := g.m.villageCenters(t)
		for range 10 {
			if p, ok := landRandomPos(t.w, g.m, 20, 8); ok && villageDistance(sectionOf(p), centers) <= 1 {
				g.destination, g.reach = p, 0
				return true
			}
		}
		return false
	}
	return g.fallback(t)
}

// fallback runs only without visible children: interact with a villager or
// cat, stroll, follow the look target, jump on a bed, or pause.
// vanilla: VillagerGoalPackages.getPlayPackage
func (g *kidPlayGoal) fallback(t *tickView) bool {
	m := g.m
	g.pace, g.reach = villagerWalkPace, 2
	for _, c := range weightedChoices(g.m, 2, 1, 1, 1, 2, 2) {
		switch c.kind {
		case 0, 1:
			typ := TypeVillager
			if c.kind == 1 {
				typ = TypeCat
			}
			g.target = m.nearestSeen(t, 64, func(o *Mob) bool { return o.Type == typ })
			if g.target != nil {
				return true
			}
		case 2:
			if p, ok := m.villageBoundPos(t, 10, 7); ok {
				g.destination, g.reach = p, 0
				return true
			}
		case 3:
			if m.vb.look.visible(m, t) {
				g.look = m.vb.look
				return true
			}
		case 4:
			if m.Village != nil && m.vb.nearestBed != nil {
				g.jumping = &jumpOnBedGoal{m: m, bed: *m.vb.nearestBed}
				return true
			}
		case 5:
			g.waiting = true
			return true
		}
	}
	return false
}

func (g *kidPlayGoal) Start(t *tickView) {
	g.left = 80 + g.m.Rand.Intn(41)
	if g.waiting {
		g.left = 20 + g.m.Rand.Intn(21)
		return
	}
	if g.jumping != nil {
		g.jumping.Start(t)
		return
	}
	if g.target != nil {
		g.m.vb.kids.interaction = g.target
		g.destination = g.target.cell()
	} else if p, ok := g.look.cell(t); ok {
		g.destination = p
	}

	g.walk(t)
}
func (g *kidPlayGoal) walk(t *tickView) {
	p := g.destination
	g.m.seekTo(t.w, float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5, g.pace, g.reach)
}
func (g *kidPlayGoal) ShouldContinue(t *tickView) bool {
	if !g.active() || g.left <= 0 {
		return false
	}
	if g.jumping != nil {
		return g.jumping.ShouldContinue(t)
	}
	if g.target != nil && (!g.target.Alive() || !t.containsMob(g.target)) {
		return false
	}

	if g.waiting {
		return true
	}
	p := g.destination
	if g.target != nil {
		p = g.target.cell()
	} else if g.look != (villagerLookTarget{}) {
		var ok bool
		p, ok = g.look.cell(t)
		if !ok {
			return false
		}
	}
	return len(g.m.navOf().nav.path) != 0 && manhattan3(g.m.cell(), p) > g.reach
}
func (g *kidPlayGoal) Tick(t *tickView) {
	if !g.active() || g.target != nil && (!g.target.Alive() || !t.containsMob(g.target)) {
		g.left = 0
		g.m.stopSeeking()
		return
	}
	g.left--
	if g.jumping != nil {
		g.jumping.Tick(t)
		return
	}
	target := g.look
	if g.target != nil {
		target = villagerLookTarget{mob: g.target}
	}
	if target == (villagerLookTarget{}) {
		return
	}
	p, ok := target.cell(t)
	if !ok {
		g.left = 0
		g.m.stopSeeking()
		return
	}
	target.look(g.m, t)
	dx, dy, dz := p.X-g.destination.X, p.Y-g.destination.Y, p.Z-g.destination.Z
	if dx*dx+dy*dy+dz*dz > 4 {
		g.destination = p
		g.walk(t)
	}
}

func (g *kidPlayGoal) Stop(*tickView) {
	g.target, g.jumping = nil, nil
	g.look = villagerLookTarget{}
	g.m.stopSeeking()
}
