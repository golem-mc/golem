package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/rules"
)

// avoidGoal runs from the nearest visible threat within maxDist, at walk
// pace, or sprint pace within seven blocks. Threats are players not in
// creative or spectator, or live mobs of the named types.
// vanilla: AvoidEntityGoal
type avoidGoal struct {
	m            *Mob
	priority     int
	active       func() bool
	maxDist      float64
	walk, sprint float64
	mobs         []MobType
	// monsters adds every Monster to the threats.
	monsters bool
	// playerOK and mobOK narrow the threats where set.
	playerOK func(*Player) bool
	mobOK    func(*Mob) bool
	player   int32
	threat   *Mob
	// canFlee gates a planned escape, after selecting the nearest threat.
	canFlee func(*Mob) bool
	path    []Waypoint
	end     Waypoint
}

// felineAvoidDist, felineWalkPace and felineSprintPace are the 16, 0.8 and
// 1.33 the cat's and the ocelot's avoid goals are built with.
// vanilla: Cat.reassessTameGoals, Ocelot.reassessTrustingGoals
const (
	felineAvoidDist  = 16.0
	felineWalkPace   = 0.8
	felineSprintPace = 1.33
	// avoidSprintDistSq is the 7 blocks AvoidEntityGoal.tick sprints inside.
	avoidSprintDistSq = 49.0
	// avoidHeight is the vertical inflation of the search box.
	// vanilla: AvoidEntityGoal.canUse, inflate(maxDist, 3.0, maxDist)
	avoidHeight = 3.0
)

// newFelineAvoidGoal flees players while active.
func newFelineAvoidGoal(m *Mob, priority int, active func() bool) *avoidGoal {
	return &avoidGoal{m: m, priority: priority, active: active, maxDist: felineAvoidDist,
		walk: felineWalkPace, sprint: felineSprintPace}
}

// newCreeperAvoidGoal flees cats and ocelots within six blocks.
// vanilla: Creeper.registerGoals
func newCreeperAvoidGoal(m *Mob, priority int) *avoidGoal {
	return &avoidGoal{m: m, priority: priority, active: func() bool { return true }, maxDist: 6,
		walk: 1.0, sprint: 1.2, mobs: []MobType{TypeCat, TypeOcelot}}
}

// creakingAvoidDist is the 8 blocks the illagers' AvoidEntityGoal<Creaking>
// watches.
// vanilla: Pillager.registerGoals, Vindicator.registerGoals, Evoker.registerGoals
const creakingAvoidDist = 8.0

// newCreakingAvoidGoal flees creakings within eight blocks at the pace given.
func newCreakingAvoidGoal(m *Mob, priority int, walk, sprint float64) *avoidGoal {
	return &avoidGoal{m: m, priority: priority, active: func() bool { return true }, maxDist: creakingAvoidDist,
		walk: walk, sprint: sprint, mobs: []MobType{TypeCreaking}}
}

func (g *avoidGoal) Priority() int    { return g.priority }
func (g *avoidGoal) Flags() GoalFlags { return GoalMove }

// playersThreaten reports whether an avoid goal may flee players: in Peaceful
// no mob can attack one, so forCombat targeting never finds one.
// vanilla: LivingEntity.canAttack
func playersThreaten(t *tickView) bool { return t.w.Difficulty() != rules.DifficultyPeaceful }

// nearestThreat is the closest visible threat's position and its player
// entity ID or mob.
func (g *avoidGoal) nearestThreat(t *tickView) (x, y, z float64, player int32, mob *Mob, ok bool) {
	best := math.Inf(1)
	if g.mobs == nil && !g.monsters {
		if !playersThreaten(t) {
			return
		}
		for i := range t.players {
			p := &t.players[i]
			pw, ph, _ := p.box()
			if p.Creative || (g.playerOK != nil && !g.playerOK(p)) || !g.inBox(p.X, p.Y, p.Z, pw, ph) ||
				!g.m.noticesPlayer(t, p, g.maxDist, true) {
				continue
			}
			if d := Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z); d < best {
				best, x, y, z, player, ok = d, p.X, p.Y, p.Z, p.EntityID, true
			}
		}
		return
	}
	for _, o := range g.m.herdNear(t, max(g.maxDist, avoidHeight)) {
		if o == g.m || !o.Alive() || !g.threatens(o) {
			continue
		}
		d := Dist2(g.m.X, g.m.Y, g.m.Z, o.X, o.Y, o.Z)
		if d > g.maxDist*g.maxDist || d >= best || !g.inBox(o.X, o.Y, o.Z, o.width(), o.height()) ||
			!g.m.seesPlayer(t.w, o.X, o.Y+o.eyeHeight()-playerEyeHeight, o.Z) {
			continue
		}
		best, x, y, z, mob, ok = d, o.X, o.Y, o.Z, o, true
	}
	return
}

// inBox reports whether a body at (x, y, z) meets the mob's box grown by
// maxDist sideways and 3 vertically.
// vanilla: AvoidEntityGoal.canUse
func (g *avoidGoal) inBox(x, y, z, width, height float64) bool {
	m := g.m
	reach := g.maxDist + (m.width()+width)/2
	return math.Abs(x-m.X) < reach && math.Abs(z-m.Z) < reach &&
		y+height > m.Y-avoidHeight && y < m.Y+m.height()+avoidHeight
}

// herdNear is the mobs a box grown by inflate around m can reach, in herd
// order: the grid's candidates in a large herd, else the whole herd. The
// result is scratch valid until the next query.
func (m *Mob) herdNear(t *tickView, inflate float64) []*Mob {
	if m.push != nil && len(t.others) > villagerGridMinHerd {
		return m.push.candidates(m, inflate+1e-6)
	}
	return t.others
}

func (g *avoidGoal) threatens(o *Mob) bool {
	return (g.monsters && o.Hostile() || slices.Contains(g.mobs, o.Type)) && (g.mobOK == nil || g.mobOK(o))
}

// CanStart picks a threat and a cell away from it to run to.
// vanilla: AvoidEntityGoal.canUse
func (g *avoidGoal) CanStart(t *tickView) bool {
	if !g.active() {
		return false
	}
	x, y, z, player, mob, ok := g.nearestThreat(t)
	if !ok {
		return false
	}
	cell, ok := awayRandomPos(t.w, g.m, x, z, 16, 7)
	if !ok {
		return false
	}
	ax, ay, az := float64(cell.X)+0.5, float64(cell.Y), float64(cell.Z)+0.5
	if Dist2(x, y, z, ax, ay, az) < Dist2(x, y, z, g.m.X, g.m.Y, g.m.Z) {
		return false
	}
	path, end, ok := g.m.createPath(t.w, ax, ay, az)
	if !ok || len(path) == 0 {
		return false
	}
	if g.canFlee != nil && !g.canFlee(mob) {
		return false
	}
	g.player, g.threat = player, mob
	g.path, g.end = path, end
	return true
}

// Start installs the escape after the selector stops the previous mover,
// whose Stop may clear navigation.
// vanilla: AvoidEntityGoal.start
func (g *avoidGoal) Start(*tickView) { g.m.moveTo(g.path, g.end, g.walk) }

func (g *avoidGoal) ShouldContinue(*tickView) bool {
	return g.active() && len(g.m.navOf().nav.path) > 0
}

func (g *avoidGoal) Tick(t *tickView) {
	x, y, z := g.threatPos(t)
	pace := g.walk
	if Dist2(g.m.X, g.m.Y, g.m.Z, x, y, z) < avoidSprintDistSq {
		pace = g.sprint
	}
	g.m.navOf().nav.speedFactor = pace
}

// threatPos is the threat's current position, or the mob's own if a player
// left.
func (g *avoidGoal) threatPos(t *tickView) (x, y, z float64) {
	if g.threat != nil {
		return g.threat.X, g.threat.Y, g.threat.Z
	}
	for i := range t.players {
		if p := &t.players[i]; p.EntityID == g.player {
			return p.X, p.Y, p.Z
		}
	}
	return g.m.X, g.m.Y, g.m.Z
}

func (g *avoidGoal) Stop(*tickView)          { g.player, g.threat = 0, nil }
func (g *avoidGoal) RequiresEveryTick() bool { return false }

// awayRandomPos is the best-scoring stable, harmless cell of ten tries in
// the half-plane away from (ax, az).
// vanilla: DefaultRandomPos.getPosAway
func awayRandomPos(w World, m *Mob, ax, az float64, xzSpread, ySpread int) (Waypoint, bool) {
	return directedRandomPos(w, m, ax, az, xzSpread, ySpread, false)
}

// walkAwayFrom starts a walk at pace to the first of ten land picks away from
// (x, z) that a path reaches. It reports whether a walk started.
// vanilla: SetWalkTargetAwayFrom.create
func (m *Mob) walkAwayFrom(t *tickView, x, z, pace float64) bool {
	for range 10 {
		cell, ok := landRandomPosAway(t.w, m, x, z, 16, 7)
		if !ok {
			continue
		}
		if path, end, ok := m.createPath(t.w, float64(cell.X)+0.5, float64(cell.Y), float64(cell.Z)+0.5); ok && len(path) > 0 {
			m.moveTo(path, end, pace)
			return true
		}
	}
	return false
}

// landRandomPosAway is LandRandomPos.getPosAway: climb out of solids and
// reject water as well as harmful cells.
func landRandomPosAway(w World, m *Mob, ax, az float64, xzSpread, ySpread int) (Waypoint, bool) {
	return directedRandomPos(w, m, ax, az, xzSpread, ySpread, true)
}

func directedRandomPos(w World, m *Mob, ax, az float64, xzSpread, ySpread int, land bool) (Waypoint, bool) {
	dirX, dirZ := m.X-ax, m.Z-az
	if land && dirX == 0 && dirZ == 0 {
		dirX, dirZ = m.Rand.Float64()-0.5, m.Rand.Float64()-0.5
	}
	return randomPosTries(w, m, func() (Waypoint, bool) {
		// vanilla: RandomPos.generateRandomDirectionWithinRadians
		yaw := math.Atan2(dirZ, dirX) - math.Pi/2 + float64(2*m.Rand.Float32()-1)*math.Pi/2
		dist := math.Sqrt(m.Rand.Float64()) * float64(xzSpread) * math.Sqrt2
		xt, zt := -dist*math.Sin(yaw), dist*math.Cos(yaw)
		if math.Abs(xt) > float64(xzSpread) || math.Abs(zt) > float64(xzSpread) {
			return Waypoint{}, false
		}
		yt := m.Rand.Intn(2*ySpread+1) - ySpread
		spot := Waypoint{
			int32(math.Floor(math.Floor(xt) + m.X)),
			int32(math.Floor(float64(yt) + m.Y)),
			int32(math.Floor(math.Floor(zt) + m.Z)),
		}
		if land {
			return landSpot(w, m, spot)
		}
		return spot, spot.Y >= w.MinY() && spot.Y < w.MaxY() && isStableDestination(w, m, spot) && !hasMalus(w, m, spot)
	}, func(spot Waypoint) (Waypoint, bool) {
		if land {
			return resolveStrollSpot(w, m, spot)
		}
		return spot, true
	})
}
