package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Turtle's numbers: the land pace its move control settles at, the 9-block
// egg-laying radius around home, the distance from which a grown turtle
// heads home, and the radius that counts as home.
const (
	turtleLandFloor   = 0.06
	turtleLerp        = 0.125
	turtleLayRange    = 9.0
	turtleHomeFar     = 64.0
	turtleHomeNear    = 7.0
	turtleHomeTryNear = 16.0
	turtleGoHomeRoll  = 700
	turtleHomeGiveUp  = 600
	// turtleLayTicks is adjustedTickDelay(200) of a goal that runs every tick.
	turtleLayTicks    = 200
	turtleStrollEvery = 100
	turtleWaterSearch = 24
	turtleLaySearch   = 16
	turtleWaterGiveUp = 1200
	turtlePanicWater  = 7
	turtleInLoveAfter = 600
	turtleBreedAge    = 6000
	turtleHomeAngle   = 0.3141592741012573
	// turtleTravelXZ and turtleTravelY are the spreads of the far point a
	// travelling turtle picks, turtleTravelLoaded the block radius that must
	// be loaded around each step.
	turtleTravelXZ     = 512
	turtleTravelY      = 4
	turtleTravelLoaded = 34
	turtleWideAngle    = 1.5707963705062866
)

// turtleGoals wires vanilla's priorities: water-seeking panic, breeding into
// an egg, laying, the seagrass tempt, going to water, going home, glance and
// stroll. The travel goal swims a turtle towards a far point.
// vanilla: Turtle.registerGoals
func turtleGoals(m *Mob) []Goal {
	return []Goal{
		&panicGoal{m: m, turtle: true},
		&breedGoal{m: m, priority: 1, turtle: true},
		newTurtleLayEggGoal(m),
		&temptGoal{m: m, priority: 2},
		newTurtleGoToWaterGoal(m),
		&turtleGoHomeGoal{m: m},
		&turtleTravelGoal{m: m},
		&lookAtPlayer{m: m, priority: 8},
		&randomStroll{m: m, priority: 9, interval: turtleStrollEvery, blocked: func() bool {
			return m.InWater() || m.goingHome || m.HasEgg
		}},
	}
}

// homeCloserThan is BlockPos.closerToCenterThan over the home block and the
// mob's position.
func (m *Mob) homeCloserThan(dist float64) bool {
	return Dist2(float64(m.Home.X)+0.5, float64(m.Home.Y)+0.5, float64(m.Home.Z)+0.5, m.X, m.Y, m.Z) < dist*dist
}

// setLayingEgg is Turtle.setLayingEgg: the counter restarts with the flag.
func (m *Mob) setLayingEgg(on bool) {
	m.SetMetadataLayingEgg(on)
	m.layEggCounter = 0
	if on {
		m.layEggCounter = 1
	}
}

// sandBlocks marks the blocks of the sand tag.
var sandBlocks = registry.BlockTagOf("minecraft:sand")

// isSandAt is TurtleEggBlock.isSand over the block at the cell.
func isSandAt(w World, x, y, z int32) bool {
	return registry.BlockOf(w.BlockAt(x, y, z)).InTag(sandBlocks)
}

// turtleSpawnOk is Turtle.checkTurtleSpawnRules: low enough, on sand and
// bright enough.
func turtleSpawnOk(w SpawnWorld, x, y, z int32) bool {
	return y < w.SeaLevel()+4 && isSandAt(w, x, y-1, z) && rawBrightness(w, x, y, z) > 8
}

// moveToBlock is MoveToBlockGoal: the search for a valid block within a
// range, the walk to the air above it, and the give-up counters.
// vanilla: MoveToBlockGoal
type moveToBlock struct {
	m           *Mob
	pace        float64
	searchRange int
	vertStart   int
	// vertRange is the highest layer the search climbs to; zero is the
	// default 1.
	vertRange int
	// onBlock aims the walk at the block itself rather than the cell over
	// it.
	onBlock bool
	// exactReach walks to the walkable cell itself (reach 0) rather than
	// within navReach of it.
	exactReach bool
	valid      func(w World, x, y, z int32) bool
	// anyIn, when set, reports whether the inclusive box may hold a valid
	// cell; false skips the walk, which would find nothing.
	anyIn func(w World, x0, y0, z0, x1, y1, z1 int32) bool
	// recalcEvery is the ticks between path replans.
	recalcEvery int
	// acceptedSq is the squared distance to the cell above the block that
	// counts as reached; zero is MoveToBlockGoal.acceptedDistance's 1.
	acceptedSq float64
	// startDelay replaces the 200 to 399 ticks between searches where
	// nonzero.
	// vanilla: MoveToBlockGoal.nextStartTick
	startDelay int

	nextStart int
	tryTicks  int
	maxStay   int
	target    Waypoint
	reached   bool
}

func newMoveToBlock(m *Mob, pace float64, searchRange int, valid func(World, int32, int32, int32) bool) *moveToBlock {
	return &moveToBlock{m: m, pace: pace, searchRange: searchRange, valid: valid, recalcEvery: 40}
}

// canUse is MoveToBlockGoal.canUse: a countdown between searches.
func (g *moveToBlock) canUse(t *tickView) bool {
	if g.nextStart > 0 {
		g.nextStart--
		return false
	}
	g.nextStart = g.startDelay
	if g.startDelay == 0 {
		g.nextStart = reducedTickDelay(200 + g.m.Rand.Intn(200))
	}
	return g.find(t.w)
}

// find is MoveToBlockGoal.findNearestBlock: the cells in growing squares
// around the mob, the layers alternating up and down from vertStart.
func (g *moveToBlock) find(w World) bool {
	at := g.m.cell()
	vr, r := int32(max(g.vertRange, 1)), int32(g.searchRange-1)
	if g.anyIn != nil && !g.anyIn(w, at.X-r, at.Y-1-vr, at.Z-r, at.X+r, at.Y-1+vr, at.Z+r) {
		return false
	}
	for y := g.vertStart; y <= max(g.vertRange, 1); {
		for r := 0; r < g.searchRange; r++ {
			for x := 0; x <= r; {
				z := 0
				if x < r && x > -r {
					z = r
				}
				for z <= r {
					cell := Waypoint{at.X + int32(x), at.Y + int32(y) - 1, at.Z + int32(z)}
					if g.valid(w, cell.X, cell.Y, cell.Z) {
						g.target = cell
						return true
					}
					if z > 0 {
						z = -z
					} else {
						z = 1 - z
					}
				}
				if x > 0 {
					x = -x
				} else {
					x = 1 - x
				}
			}
		}
		if y > 0 {
			y = -y
		} else {
			y = 1 - y
		}
	}
	return false
}

func (g *moveToBlock) walk(t *tickView, x, y, z int32) {
	reach := int32(navReach)
	if g.exactReach {
		reach = 0
	}
	g.m.seekTo(t.w, float64(x)+0.5, float64(y), float64(z)+0.5, g.pace, reach)
}

// start is MoveToBlockGoal.start.
func (g *moveToBlock) start(t *tickView) {
	g.walk(t, g.target.X, g.target.Y+1, g.target.Z)
	g.tryTicks = 0
	g.maxStay = g.m.Rand.Intn(g.m.Rand.Intn(1200)+1200) + 1200
}

// cont is MoveToBlockGoal.canContinueToUse.
func (g *moveToBlock) cont(t *tickView) bool {
	return g.tryTicks >= -g.maxStay && g.tryTicks <= 1200 && g.valid(t.w, g.target.X, g.target.Y, g.target.Z)
}

// tick is MoveToBlockGoal.tick: the walk to the cell above the block, the
// try counter running up on the way and down once there.
func (g *moveToBlock) tick(t *tickView) {
	above := Waypoint{g.target.X, g.target.Y + 1, g.target.Z}
	if g.onBlock {
		above.Y = g.target.Y
	}
	accepted := g.acceptedSq
	if accepted == 0 {
		accepted = 1
	}
	if Dist2(float64(above.X)+0.5, float64(above.Y)+0.5, float64(above.Z)+0.5, g.m.X, g.m.Y, g.m.Z) >= accepted {
		g.reached = false
		g.tryTicks++
		if g.tryTicks%g.recalcEvery == 0 {
			g.walk(t, above.X, above.Y, above.Z)
		}
		return
	}
	g.reached = true
	g.tryTicks--
}

func (g *moveToBlock) Flags() GoalFlags        { return GoalMove | GoalJump }
func (g *moveToBlock) RequiresEveryTick() bool { return true }
func (g *moveToBlock) Stop(*tickView)          {}

// turtleLayEggGoal is TurtleLayEggGoal: with an egg and home near, it walks
// to sand with air above and digs there until the egg is laid.
// vanilla: Turtle.TurtleLayEggGoal
type turtleLayEggGoal struct{ *moveToBlock }

func newTurtleLayEggGoal(m *Mob) *turtleLayEggGoal {
	return &turtleLayEggGoal{newMoveToBlock(m, 1.0, turtleLaySearch, func(w World, x, y, z int32) bool {
		return rules.IsAir(w.BlockAt(x, y+1, z)) && isSandAt(w, x, y, z)
	})}
}

func (g *turtleLayEggGoal) Priority() int { return 1 }

func (g *turtleLayEggGoal) CanStart(t *tickView) bool {
	return g.m.HasEgg && g.m.homeCloserThan(turtleLayRange) && g.canUse(t)
}

func (g *turtleLayEggGoal) Start(t *tickView) { g.start(t) }

func (g *turtleLayEggGoal) ShouldContinue(t *tickView) bool {
	return g.cont(t) && g.m.HasEgg && g.m.homeCloserThan(turtleLayRange)
}

func (g *turtleLayEggGoal) Tick(t *tickView) {
	g.tick(t)
	m := g.m
	if m.InWater() || !g.reached {
		return
	}
	if m.layEggCounter < 1 {
		m.setLayingEgg(true)
	} else if m.layEggCounter > turtleLayTicks {
		t.events = append(t.events, MobEvent{
			Mob: m, Kind: MobLaidTurtleEgg,
			X:      float64(g.target.X),
			Y:      float64(g.target.Y + 1),
			Z:      float64(g.target.Z),
			Amount: m.Rand.Intn(4) + 1,
			Pitch:  0.9 + m.Rand.Float32()*0.2,
		})
		m.SetMetadataHasEgg(false)
		m.setLayingEgg(false)
		m.InLove = turtleInLoveAfter
	}
	if m.LayingEgg {
		m.layEggCounter++
	}
}

// zombieAttackTurtleEggGoal is Zombie.ZombieAttackTurtleEggGoal, a
// RemoveBlockGoal for turtle eggs: the zombie walks onto the egg and stomps it
// for 60 ticks.
// vanilla: Zombie.ZombieAttackTurtleEggGoal, RemoveBlockGoal
type zombieAttackTurtleEggGoal struct {
	*moveToBlock
	stompTicks int
}

func newZombieAttackTurtleEggGoal(m *Mob) *zombieAttackTurtleEggGoal {
	mb := newMoveToBlock(m, 1.0, 24, func(w World, x, y, z int32) bool {
		return world.IsTurtleEgg(w.BlockAt(x, y, z)) && rules.IsAir(w.BlockAt(x, y+1, z)) && rules.IsAir(w.BlockAt(x, y+2, z))
	})
	mb.vertRange = 3
	mb.anyIn = World.TurtleEggsIn
	mb.exactReach = true
	// vanilla: ZombieAttackTurtleEggGoal.acceptedDistance 1.14, squared
	mb.acceptedSq = 1.14 * 1.14
	return &zombieAttackTurtleEggGoal{moveToBlock: mb}
}

func (g *zombieAttackTurtleEggGoal) Priority() int { return 4 }

// CanStart is RemoveBlockGoal.canUse: nothing happens with mob_griefing off,
// and a find waits 20 ticks before the next search.
func (g *zombieAttackTurtleEggGoal) CanStart(t *tickView) bool {
	if !t.w.MobGriefing() {
		return false
	}
	if g.nextStart > 0 {
		g.nextStart--
		return false
	}
	if g.find(t.w) {
		g.nextStart = reducedTickDelay(20)
		return true
	}
	g.nextStart = reducedTickDelay(200 + g.m.Rand.Intn(200))
	return false
}

func (g *zombieAttackTurtleEggGoal) Start(t *tickView) {
	g.start(t)
	g.stompTicks = 0
}

func (g *zombieAttackTurtleEggGoal) ShouldContinue(t *tickView) bool { return g.cont(t) }

// Stop is RemoveBlockGoal.stop.
func (g *zombieAttackTurtleEggGoal) Stop(*tickView) { g.m.FallDistance = 1.0 }

// eggAt is RemoveBlockGoal.getPosWithBlock: the egg at the mob's cell, else
// below it, beside it or two below.
func (g *zombieAttackTurtleEggGoal) eggAt(w World) (Waypoint, bool) {
	at := g.m.cell()
	for _, d := range [...]Waypoint{{0, 0, 0}, {0, -1, 0}, {-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}, {0, -2, 0}} {
		if world.IsTurtleEgg(w.BlockAt(at.X+d.X, at.Y+d.Y, at.Z+d.Z)) {
			return Waypoint{at.X + d.X, at.Y + d.Y, at.Z + d.Z}, true
		}
	}
	return Waypoint{}, false
}

// Tick is RemoveBlockGoal.tick.
func (g *zombieAttackTurtleEggGoal) Tick(t *tickView) {
	g.tick(t)
	egg, ok := g.eggAt(t.w)
	if !g.reached || !ok {
		return
	}
	m := g.m
	at := func(kind MobEventKind) MobEvent {
		return MobEvent{Mob: m, Kind: kind, X: float64(egg.X), Y: float64(egg.Y), Z: float64(egg.Z)}
	}
	if g.stompTicks > 0 {
		m.VY = 0.3
		ev := at(MobStompingEgg)
		ev.Amount = 1
		t.events = append(t.events, ev)
	}
	if g.stompTicks%2 == 0 {
		m.VY = -0.3
		if g.stompTicks%6 == 0 {
			ev := at(MobStompingEgg)
			ev.Pitch = 0.9 + m.Rand.Float32()*0.2
			t.events = append(t.events, ev)
		}
	}
	if g.stompTicks > 60 {
		ev := at(MobStompedEgg)
		ev.Pitch = 0.9 + m.Rand.Float32()*0.2
		t.events = append(t.events, ev)
	}
	g.stompTicks++
}

// turtleGoToWaterGoal is TurtleGoToWaterGoal: a turtle out of the water
// with no errand walks to the nearest water block, a hatchling always.
// vanilla: Turtle.TurtleGoToWaterGoal
type turtleGoToWaterGoal struct{ *moveToBlock }

func newTurtleGoToWaterGoal(m *Mob) *turtleGoToWaterGoal {
	g := &turtleGoToWaterGoal{newMoveToBlock(m, 1.0, turtleWaterSearch, func(w World, x, y, z int32) bool {
		return rules.IsWaterBlock(w.BlockAt(x, y, z))
	})}
	g.vertStart = -1
	g.recalcEvery = 160
	return g
}

func (g *turtleGoToWaterGoal) Priority() int { return 3 }

func (g *turtleGoToWaterGoal) CanStart(t *tickView) bool {
	m := g.m
	if m.IsBaby() && !m.InWater() {
		return g.canUse(t)
	}
	if !m.goingHome && !m.InWater() && !m.HasEgg {
		return g.canUse(t)
	}
	return false
}

func (g *turtleGoToWaterGoal) Start(t *tickView) { g.start(t) }

func (g *turtleGoToWaterGoal) ShouldContinue(t *tickView) bool {
	return !g.m.InWater() && g.tryTicks <= turtleWaterGiveUp &&
		g.valid(t.w, g.target.X, g.target.Y, g.target.Z)
}

func (g *turtleGoToWaterGoal) Tick(t *tickView) { g.tick(t) }

// turtleGoHomeGoal is TurtleGoHomeGoal: a grown turtle with an egg, or one
// far from home now and then, walks back towards the home block.
// vanilla: Turtle.TurtleGoHomeGoal
type turtleGoHomeGoal struct {
	m         *Mob
	stuck     bool
	closeTick int
}

func (g *turtleGoHomeGoal) Priority() int    { return 4 }
func (g *turtleGoHomeGoal) Flags() GoalFlags { return 0 }

func (g *turtleGoHomeGoal) CanStart(*tickView) bool {
	m := g.m
	if m.IsBaby() {
		return false
	}
	if m.HasEgg {
		return true
	}
	if m.Rand.Intn(reducedTickDelay(turtleGoHomeRoll)) != 0 {
		return false
	}
	return !m.homeCloserThan(turtleHomeFar)
}

func (g *turtleGoHomeGoal) Start(*tickView) {
	g.m.goingHome = true
	g.stuck = false
	g.closeTick = 0
}

func (g *turtleGoHomeGoal) Stop(*tickView) { g.m.goingHome = false }

func (g *turtleGoHomeGoal) ShouldContinue(*tickView) bool {
	return !g.m.homeCloserThan(turtleHomeNear) && !g.stuck && g.closeTick <= reducedTickDelay(turtleHomeGiveUp)
}

func (g *turtleGoHomeGoal) Tick(t *tickView) {
	m := g.m
	close := m.homeCloserThan(turtleHomeTryNear)
	if close {
		g.closeTick++
	}
	if len(m.navOf().nav.path) > 0 {
		return
	}
	home := [2]float64{float64(m.Home.X) + 0.5, float64(m.Home.Z) + 0.5}
	cell, ok := posTowards(t.w, m, 16, 3, home, turtleHomeAngle)
	if !ok {
		cell, ok = posTowards(t.w, m, 8, 7, home, turtleWideAngle)
	}
	if ok && !close && !rules.IsWaterBlock(t.w.BlockAt(cell.X, cell.Y, cell.Z)) {
		cell, ok = posTowards(t.w, m, 16, 5, home, turtleWideAngle)
	}
	if !ok {
		g.stuck = true
		return
	}
	m.seekTo(t.w, float64(cell.X)+0.5, float64(cell.Y), float64(cell.Z)+0.5, 1.0, navReach)
}

func (g *turtleGoHomeGoal) RequiresEveryTick() bool { return false }

// turtleTravelGoal is TurtleTravelGoal: a turtle in water with no errand
// picks a far point and swims towards it in steps.
// vanilla: Turtle.TurtleTravelGoal
type turtleTravelGoal struct {
	m     *Mob
	stuck bool
	// target is the far point, kept for the goal's run only; vanilla's
	// travelPos is never saved.
	target [2]float64
}

func (g *turtleTravelGoal) Priority() int    { return 7 }
func (g *turtleTravelGoal) Flags() GoalFlags { return 0 }

func (g *turtleTravelGoal) CanStart(*tickView) bool {
	return !g.m.goingHome && !g.m.HasEgg && g.m.InWater()
}

func (g *turtleTravelGoal) Start(*tickView) {
	m := g.m
	dx := m.Rand.Intn(2*turtleTravelXZ+1) - turtleTravelXZ
	// the height roll only feeds travelPos.y, which no step reads
	m.Rand.Intn(2*turtleTravelY + 1)
	dz := m.Rand.Intn(2*turtleTravelXZ+1) - turtleTravelXZ
	// BlockPos.containing, then atBottomCenterOf
	g.target = [2]float64{math.Floor(float64(dx)+m.X) + 0.5, math.Floor(float64(dz)+m.Z) + 0.5}
	g.stuck = false
}

func (g *turtleTravelGoal) Tick(t *tickView) {
	m := g.m
	if !m.navDone() {
		return
	}
	cell, ok := turtleTravelPos(t.w, m, 16, 3, g.target, turtleHomeAngle)
	if !ok {
		cell, ok = turtleTravelPos(t.w, m, 8, 7, g.target, turtleWideAngle)
	}
	if ok {
		ok = chunksLoadedAround(t.w, cell.X, cell.Z, turtleTravelLoaded)
	}
	if !ok {
		g.stuck = true
		return
	}
	m.seekTo(t.w, float64(cell.X)+0.5, float64(cell.Y), float64(cell.Z)+0.5, 1.0, navReach)
}

func (g *turtleTravelGoal) ShouldContinue(*tickView) bool {
	m := g.m
	return !m.navDone() && !g.stuck && !m.goingHome && !m.IsInLove() && !m.HasEgg
}

func (g *turtleTravelGoal) Stop(*tickView)          {}
func (g *turtleTravelGoal) RequiresEveryTick() bool { return false }

// chunksLoadedAround reports whether every chunk within r blocks of the
// column is loaded.
// vanilla: LevelReader.hasChunksAt
func chunksLoadedAround(w World, x, z, r int32) bool {
	for cx := (x - r) >> 4; cx <= (x+r)>>4; cx++ {
		for cz := (z - r) >> 4; cz <= (z+r)>>4; cz++ {
			if !chunkLoaded(w, cx<<4, cz<<4) {
				return false
			}
		}
	}
	return true
}

// turtleTravelPos is posTowards while a travel position is set: a stable
// destination is then a water block.
// vanilla: Turtle.TurtlePathNavigation.isStableDestination
func turtleTravelPos(w World, m *Mob, xzSpread, ySpread int, target [2]float64, maxAngle float64) (Waypoint, bool) {
	return posTowardsWhere(w, m, xzSpread, ySpread, target, maxAngle, func(spot Waypoint) bool {
		return spot.Y >= w.MinY() && spot.Y < w.MaxY() && rules.IsWater(w.BlockAt(spot.X, spot.Y, spot.Z)) && !hasMalus(w, m, spot)
	})
}

// turtleSettle is the speed TurtleMoveControl settles at for a target: each
// tick it divides the speed by div (down to floor), then lerps 0.125 toward
// the target.
func turtleSettle(target, div, floor float64) float64 {
	if floored := floor*(1-turtleLerp) + turtleLerp*target; floored <= div*floor {
		return floored
	}
	return turtleLerp * target / (1 - (1-turtleLerp)/div)
}

// turtleMoveSpeed is the move control's settled speed for a pace: on land
// halved toward 0.06, in water halved toward 0.08 away from home and a third
// toward 0.06 for a hatchling; in the air the pace itself.
// vanilla: Turtle.TurtleMoveControl.updateSpeed, tick
func (m *Mob) turtleMoveSpeed(speed float64) float64 {
	switch {
	case m.InWater():
		div, floor := 1.0, 0.0
		if !m.homeCloserThan(16) {
			div, floor = 2, 0.08
		}
		if m.IsBaby() {
			div, floor = div*3, 0.06
		}
		if div == 1 {
			return turtleSettle(speed, 1, 0)
		}
		return turtleSettle(speed, div, floor)
	case m.OnGround:
		return turtleSettle(speed, 2, turtleLandFloor)
	}
	return speed
}

// turtleSwimDrive is the move control's vertical push toward a waypoint of
// the walk, applied while the turtle is in water.
// vanilla: Turtle.TurtleMoveControl.tick
func (m *Mob) turtleSwimDrive(x, z float64) {
	if m.Type != TypeTurtle || !m.InWater() || len(m.nav.path) == 0 {
		return
	}
	dx, dy, dz := x-m.X, float64(m.nav.path[0].Y)-m.Y, z-m.Z
	if dist := math.Sqrt(dx*dx + dy*dy + dz*dz); dist >= 1e-5 {
		m.VY += m.walk.speed * dy / dist * 0.1
	}
}

// turtleTravelInWater moves a turtle through water: acceleration 0.1, drag
// 0.9, and a sink of 0.005 unless it is going home within 20 blocks. The
// move control lifts it 0.005 every tick.
// vanilla: Turtle.travelInWater, TurtleMoveControl.updateSpeed
func (m *Mob) turtleTravelInWater(w World) {
	m.VY += 0.005
	if m.walk.speed > 0 {
		input := math.Min(1, m.walk.speed*0.98)
		m.VX += m.walk.dirX * 0.1 * input
		m.VZ += m.walk.dirZ * 0.1 * input
	}
	m.move(w, m.VX, m.VY, m.VZ)
	m.VX *= 0.9
	m.VY *= 0.9
	m.VZ *= 0.9
	if !(m.TargetID != 0 || m.goingHome && m.homeCloserThan(20)) {
		m.VY -= 0.005
	}
}

// posTowards is DefaultRandomPos.getPosTowards for a turtle with no travel
// position: ten cells within the spreads, bearing within maxAngle of the
// target (x, z), best walk-target value winning. A cell is stable over any
// block but air, so open water of any depth qualifies; a cell the body
// cannot enter (a solid block or a door, malus -1) is refused as hasMalus.
// vanilla: DefaultRandomPos.getPosTowards, Turtle.TurtlePathNavigation.isStableDestination
func posTowards(w World, m *Mob, xzSpread, ySpread int, target [2]float64, maxAngle float64) (Waypoint, bool) {
	return posTowardsWhere(w, m, xzSpread, ySpread, target, maxAngle, func(spot Waypoint) bool {
		if spot.Y < w.MinY() || spot.Y >= w.MaxY() {
			return false
		}
		state := w.BlockAt(spot.X, spot.Y, spot.Z)
		below := w.BlockAt(spot.X, spot.Y-1, spot.Z)
		return !registry.BlockOf(below).InTag(registry.TagIDAir) &&
			(blockExtent(state) == 0 || pathfindableOpen(state)) && !hasMalus(w, m, spot)
	})
}

// posTowardsWalkable is getPosTowards for a walking body: a cell its
// footprint can stand in.
func posTowardsWalkable(w World, m *Mob, xzSpread, ySpread int, target [2]float64, maxAngle float64) (Waypoint, bool) {
	foot := footprintOf(m.width(), m.height())
	return posTowardsWhere(w, m, xzSpread, ySpread, target, maxAngle, func(spot Waypoint) bool {
		return walkableAt(w, foot, spot.X, spot.Y, spot.Z)
	})
}

// posTowardsWhere is posTowards with the cell test given.
func posTowardsWhere(w World, m *Mob, xzSpread, ySpread int, target [2]float64, maxAngle float64,
	valid func(Waypoint) bool) (Waypoint, bool) {
	from := m.cell()
	center := math.Atan2(target[1]-m.Z, target[0]-m.X) - math.Pi/2
	best := math.Inf(-1)
	var cell Waypoint
	found := false
	for range defaultRandomTries {
		angle := center + float64(2*m.Rand.Float32()-1)*maxAngle
		dist := math.Sqrt(m.Rand.Float64()) * float64(xzSpread) * math.Sqrt2
		dx, dz := -dist*math.Sin(angle), dist*math.Cos(angle)
		if math.Abs(dx) > float64(xzSpread) || math.Abs(dz) > float64(xzSpread) {
			continue
		}
		dy := m.Rand.Intn(2*ySpread+1) - ySpread
		spot := Waypoint{from.X + int32(math.Floor(dx)), from.Y + int32(dy), from.Z + int32(math.Floor(dz))}
		if !valid(spot) {
			continue
		}
		if value := m.walkTargetValue(w, spot); value > best {
			best, cell, found = value, spot, true
		}
	}
	return cell, found
}
