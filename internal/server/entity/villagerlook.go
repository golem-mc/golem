package entity

import (
	"github.com/golem-mc/golem/registry"

	"math"
	"slices"
)

type weightedChoice struct {
	kind  int
	score float64
}

// weightedChoices is a weighted permutation: an unavailable choice lets the
// next one try. vanilla: ShufflingList.WeightedEntry.setRandom
func weightedChoices(m *Mob, weights ...float64) []weightedChoice {
	choices := make([]weightedChoice, len(weights))
	for i, w := range weights {
		choices[i] = weightedChoice{i, -math.Pow(float64(m.Rand.Float32()), 1/w)}
	}
	slices.SortStableFunc(choices, func(a, b weightedChoice) int {
		if a.score < b.score {
			return -1
		}
		if a.score > b.score {
			return 1
		}
		return 0
	})
	return choices
}

// villagerLookTarget tracks a body without retaining a player tick snapshot.
// It is also the position tracker used by a walk copied from LOOK_TARGET.
// vanilla: EntityTracker
type villagerLookTarget struct {
	mob    *Mob
	player int32
}

func (l villagerLookTarget) position(t *tickView) (x, y, z float64, ok bool) {
	if l.mob != nil {
		if l.mob.Alive() && slices.Contains(t.others, l.mob) {
			return l.mob.X, l.mob.Y, l.mob.Z, true
		}
	} else if l.player != 0 {
		for i := range t.players {
			p := &t.players[i]
			if p.EntityID == l.player && !p.Dead && !p.Spectator {
				return p.X, p.Y, p.Z, true
			}
		}
	}
	return
}
func (l villagerLookTarget) cell(t *tickView) (Waypoint, bool) {
	x, y, z, ok := l.position(t)
	return Waypoint{int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))}, ok
}
func (l villagerLookTarget) visible(m *Mob, t *tickView) bool {
	if _, _, _, ok := l.position(t); !ok {
		return false
	}
	if l.mob != nil {
		r := m.visibilityRange(&Player{Invisible: l.mob.hasEffect(mobEffectInvisibility), Worn: l.mob.Worn})
		return Dist2(m.X, m.Y, m.Z, l.mob.X, l.mob.Y, l.mob.Z) <= r*r && m.sees(t, l.mob)
	}
	for i := range t.players {
		if p := &t.players[i]; p.EntityID == l.player {
			return m.noticesPlayer(t, p, m.followRange(), true)
		}
	}
	return false
}
func (l villagerLookTarget) look(m *Mob, t *tickView) {
	if l.mob != nil {
		m.lookAtMob(l.mob)
		return
	}
	for i := range t.players {
		if p := &t.players[i]; p.EntityID == l.player {
			m.lookAtTarget(p)
			return
		}
	}
}

// villagerLookGoal selects the activity's weighted look target.
// A pause holds no look memory, so play cannot turn it into a walk.
// vanilla: VillagerGoalPackages.getFullLookBehavior/getMinimalLookBehavior
type villagerLookGoal struct {
	m       *Mob
	left    int
	act     villagerActivity
	waiting bool
}

func (g *villagerLookGoal) Priority() int           { return priorityLookAt }
func (g *villagerLookGoal) Flags() GoalFlags        { return 0 }
func (g *villagerLookGoal) RequiresEveryTick() bool { return true }
func (g *villagerLookGoal) CanStart(t *tickView) bool {
	if g.m.Sleeping || g.m.Trading {
		return false
	}
	g.act, g.waiting = g.m.vb.activity, false
	weights := []float64{8, 2, 2, 1, 1, 1, 1, 1, 1, 2}
	full := g.act == actIdle || g.act == actMeet || g.act == actPlay
	if !full {
		weights = []float64{2, 2, 8}
	}
	for _, choice := range weightedChoices(g.m, weights...) {
		k := choice.kind
		if !full {
			k = [...]int{1, 2, 9}[k]
		}
		if k == 9 {
			g.waiting = true
			return true
		}
		if target := g.choose(t, k); target.mob != nil || target.player != 0 {
			g.m.vb.look = target
			return true
		}
	}
	return false
}
func (g *villagerLookGoal) choose(t *tickView, k int) villagerLookTarget {
	m := g.m
	if k == 2 {
		var target villagerLookTarget
		best := 64.0 // vanilla: SetEntityLookTarget's raw eight-block reach
		for i := range t.players {
			p := &t.players[i]
			if d := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z); d <= best && m.noticesPlayer(t, p, m.followRange(), true) {
				target, best = villagerLookTarget{player: p.EntityID}, d
			}
		}
		return target
	}
	var target *Mob
	best := 64.0
	for _, o := range t.others {
		if o == m || slices.Contains(m.Passengers, o) {
			continue
		}
		match := false
		switch k {
		case 0:
			match = o.Type == TypeCat
		case 1:
			match = o.Type == TypeVillager
		case 3:
			match = CreatureKind(o.Type)
		case 4:
			match = WaterCreatureKind(o.Type)
		case 5:
			match = o.Type == registry.EntityTypeAxolotl
		case 6:
			match = UndergroundWaterKind(o.Type)
		case 7:
			match = WaterAmbientKind(o.Type)
		case 8:
			match = MonsterKind(o.Type)
		}
		if d := Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z); match && d <= best && (villagerLookTarget{mob: o}).visible(m, t) {
			target, best = o, d
		}
	}
	return villagerLookTarget{mob: target}
}
func (g *villagerLookGoal) Start(*tickView) {
	g.left = 45 + g.m.Rand.Intn(46)
	if g.waiting {
		g.left = 30 + g.m.Rand.Intn(31)
	}
}
func (g *villagerLookGoal) ShouldContinue(t *tickView) bool {
	return g.left > 0 && !g.m.Sleeping && !g.m.Trading && g.act == g.m.vb.activity && (g.waiting || g.m.vb.look.visible(g.m, t))
}
func (g *villagerLookGoal) Tick(*tickView) { g.left-- }
func (g *villagerLookGoal) Stop(*tickView) { g.m.vb.look = villagerLookTarget{} }

// villagerLookSink applies the remembered gaze when no interaction owns the
// head. Selection runs independently so a child's walk cannot starve it.
// vanilla: LookAtTargetSink
type villagerLookSink struct{ m *Mob }

func (g *villagerLookSink) Priority() int           { return priorityLookAt }
func (g *villagerLookSink) Flags() GoalFlags        { return GoalLook }
func (g *villagerLookSink) RequiresEveryTick() bool { return true }
func (g *villagerLookSink) CanStart(*tickView) bool {
	// The preceding look goal clears memory when its visibility check fails.
	return !g.m.Sleeping && !g.m.Trading && g.m.vb.look != (villagerLookTarget{})
}
func (g *villagerLookSink) Start(*tickView)                 {}
func (g *villagerLookSink) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *villagerLookSink) Tick(t *tickView)                { g.m.vb.look.look(g.m, t) }
func (g *villagerLookSink) Stop(*tickView)                  {}

// lookWalkGoal is the IDLE RunOne's one-in-eight walk from LOOK_TARGET.
// vanilla: SetWalkTargetFromLookTarget, MoveToTargetSink
type lookWalkGoal struct {
	m           *Mob
	target      villagerLookTarget
	destination Waypoint
	left        int
}

func (g *lookWalkGoal) Priority() int           { return priorityPoiStrol }
func (g *lookWalkGoal) Flags() GoalFlags        { return GoalMove }
func (g *lookWalkGoal) RequiresEveryTick() bool { return true }
func (g *lookWalkGoal) CanStart(t *tickView) bool {
	if g.m.vb.activity != actIdle || g.m.Sleeping || g.m.Trading || len(g.m.navOf().nav.path) != 0 || g.m.Rand.Intn(8) != 0 {
		return false
	}
	g.target = g.m.vb.look
	return g.target.visible(g.m, t)
}
func (g *lookWalkGoal) Start(t *tickView) { g.left = 150 + g.m.Rand.Intn(101); g.walk(t) }
func (g *lookWalkGoal) walk(t *tickView) {
	x, y, z, ok := g.target.position(t)
	if !ok {
		return
	}
	g.destination = Waypoint{int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))}
	g.m.seekTo(t.w, x, y, z, villagerWalkPace, 2)
}
func (g *lookWalkGoal) ShouldContinue(t *tickView) bool {
	p, ok := g.target.cell(t)
	return ok && g.left > 0 && g.m.vb.activity == actIdle && !g.m.Sleeping && !g.m.Trading && len(g.m.navOf().nav.path) != 0 && manhattan3(g.m.cell(), p) > 2
}
func (g *lookWalkGoal) Tick(t *tickView) {
	g.left--
	p, ok := g.target.cell(t)
	if !ok {
		g.left = 0
		g.m.stopSeeking()
		return
	}
	dx, dy, dz := p.X-g.destination.X, p.Y-g.destination.Y, p.Z-g.destination.Z
	if dx*dx+dy*dy+dz*dz > 4 {
		g.walk(t)
	}
}
func (g *lookWalkGoal) Stop(*tickView) { g.target = villagerLookTarget{}; g.m.stopSeeking() }
