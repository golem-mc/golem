package entity

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The goal framework, after vanilla's GoalSelector. A goal has a priority
// (lower first) and the controls it holds while running: MOVE, LOOK, JUMP.
// On every other mob tick the selector drops running goals that no longer
// hold, then offers idle goals a start in priority order: a goal starts when
// no higher-priority goal runs on any of its controls, and it takes those
// controls from lower-priority holders. On the tick between, only goals that
// require every-tick updates are ticked, and none starts or stops.

// GoalFlags are the controls a running goal holds, one holder per flag.
type GoalFlags uint8

const (
	GoalMove GoalFlags = 1 << iota
	GoalLook
	GoalJump
)

// The passive goals' priorities, vanilla's numbers, lower first. One set
// serves the farm animals; the pig registers its courting goals one notch
// later, which still keeps them ahead of the wheat.
const (
	priorityFloat        = 0
	priorityPanic        = 1
	priorityBreed        = 2
	priorityTempt        = 3
	priorityFollowParent = 4
	// The pig registers its breed, tempt and follow-parent goals one notch
	// behind the other animals'.
	// vanilla: Pig.registerGoals
	priorityPigBreed        = 3
	priorityPigTempt        = 4
	priorityPigFollowParent = 5
	// priorityEat is the sheep's grazing, between the following and the
	// stroll.
	// vanilla: Sheep.registerGoals' EatBlockGoal
	priorityEat        = 5
	priorityStroll     = 6
	priorityLookAt     = 7
	priorityLookAround = 8
)

// tickView is what one tick shows the goals: the world, the players and
// nearby mobs, and the tick's events, which a goal may add to (a hunt's
// swing).
type tickView struct {
	w       World
	players []Player
	// others is the herd beside this mob, which a grudge may target and an
	// alert reaches.
	others []*Mob
	// byID indexes others by entity ID when the tick has a push grid.
	byID map[int32]*Mob
	// prey is the part of others a zombie's mob targets can match, when preyKnown.
	prey      []*Mob
	preyKnown bool
	// stands is the world's visible armor stands, a goat ram's targets.
	stands []StandTarget
	events []MobEvent
}

// Goal is one behaviour the selector drives. CanStart is offered every
// selection, so an idle goal also counts down its cooldown there.
// RequiresEveryTick is vanilla's requiresUpdateEveryTick: whether the goal
// is ticked on the ticks between selections too (default false).
type Goal interface {
	Priority() int
	Flags() GoalFlags
	CanStart(*tickView) bool
	Start(*tickView)
	ShouldContinue(*tickView) bool
	Tick(*tickView)
	Stop(*tickView)
	RequiresEveryTick() bool
}

// gatedGoal restricts a shared behavior to an activity and optionally adds controls.
type gatedGoal struct {
	Goal
	enabled  func() bool
	controls GoalFlags
}

func (g *gatedGoal) CanStart(t *tickView) bool       { return g.enabled() && g.Goal.CanStart(t) }
func (g *gatedGoal) ShouldContinue(t *tickView) bool { return g.enabled() && g.Goal.ShouldContinue(t) }
func (g *gatedGoal) Flags() GoalFlags                { return g.Goal.Flags() | g.controls }

// runOneGoal is a RunOne over shared behaviors.
type runOneGoal struct {
	m        *Mob
	choices  []Goal
	chosen   Goal
	priority int
	// weights are the choices' RunOne weights; nil weighs them equally.
	weights []int
	// flags replaces GoalMove|GoalLook where nonzero.
	flags GoalFlags
}

func (g *runOneGoal) Priority() int { return g.priority }
func (g *runOneGoal) Flags() GoalFlags {
	if g.flags != 0 {
		return g.flags
	}
	return GoalMove | GoalLook
}
func (g *runOneGoal) CanStart(t *tickView) bool {
	if g.weights != nil {
		return g.canStartWeighted(t)
	}
	start := g.m.Rand.Intn(len(g.choices))
	for i := range g.choices {
		c := g.choices[(start+i)%len(g.choices)]
		if c.CanStart(t) {
			g.chosen = c
			return true
		}
	}
	return false
}
func (g *runOneGoal) Start(t *tickView)               { g.chosen.Start(t) }
func (g *runOneGoal) ShouldContinue(t *tickView) bool { return g.chosen.ShouldContinue(t) }
func (g *runOneGoal) Tick(t *tickView)                { g.chosen.Tick(t) }
func (g *runOneGoal) Stop(t *tickView)                { g.chosen.Stop(t); g.chosen = nil }
func (g *runOneGoal) RequiresEveryTick() bool         { return true }

// maxRunOneWeighted bounds a weighted RunOne's choices so its shuffle needs
// no allocation.
const maxRunOneWeighted = 4

// canStartWeighted tries the choices in a weighted shuffle: each draws the
// key -pow(nextFloat, 1/weight), tried in ascending order.
// vanilla: ShufflingList.shuffle, GateBehavior.RunningPolicy.RUN_ONE
func (g *runOneGoal) canStartWeighted(t *tickView) bool {
	var keys [maxRunOneWeighted]float64
	var order [maxRunOneWeighted]int
	n := len(g.choices)
	for i := range n {
		keys[i] = -math.Pow(float64(g.m.Rand.Float32()), float64(1/float32(g.weights[i])))
		j := i
		for ; j > 0 && keys[order[j-1]] > keys[i]; j-- {
			order[j] = order[j-1]
		}
		order[j] = i
	}
	for _, i := range order[:n] {
		if g.choices[i].CanStart(t) {
			g.chosen = g.choices[i]
			return true
		}
	}
	return false
}

// newBrainIdle is the brain mobs' idle RunOne: a stroll weighted 2, a walk
// to the player look returns (when look is set) weighted 2, and a 30-60
// tick pause weighted 1. It takes only the legs, so glances run alongside.
// vanilla: HoglinAi.createIdleMovementBehaviors, GoatAi.initIdleActivity, CreakingAi, WardenAi
func newBrainIdle(m *Mob, priority int, pace float64, look func(*tickView) *Player) *runOneGoal {
	// vanilla: RandomStroll.stroll(speed), LandRandomPos.getPos(body, 10, 7)
	stroll := &randomStroll{m: m, interval: 1, pace: pace, ignoreIdle: true, emptyPick: true,
		pick: func(t *tickView) (Waypoint, bool) { return landRandomPos(t.w, m, strollSpreadXZ, strollSpreadY) }}
	g := &runOneGoal{m: m, priority: priority, flags: GoalMove,
		choices: []Goal{newWalkPause(m, stroll)}, weights: []int{2}}
	if look != nil {
		g.choices = append(g.choices, newWalkPause(m, &lookWalkChoice{m: m, pace: pace, look: look}))
		g.weights = append(g.weights, 2)
	}
	g.choices = append(g.choices, &doNothingGoal{m: m, min: 30, max: 60})
	g.weights = append(g.weights, 1)
	return g
}

// walkPause holds a brain RunOne's walk choice for the walk and the
// DoNothing(30, 60) spans that fill it: vanilla's walk choices only set
// WALK_TARGET, so from the next tick the RunOne can start nothing but
// DoNothing, chained while the walk lasts, and the span running when the walk
// ends is finished before the next pick.
// vanilla: OneShot.tickOrStop, GateBehavior.tryStart, RandomStroll.java:55
type walkPause struct {
	Goal
	pause   doNothingGoal
	fresh   bool // started this tick; the first span begins on the next
	pausing bool
}

func newWalkPause(m *Mob, walk Goal) *walkPause {
	return &walkPause{Goal: walk, pause: doNothingGoal{m: m, min: 30, max: 60}}
}

func (g *walkPause) Start(t *tickView) {
	g.Goal.Start(t)
	g.fresh, g.pausing = true, false
}

func (g *walkPause) ShouldContinue(t *tickView) bool {
	return g.pausing && g.pause.ShouldContinue(t) || g.Goal.ShouldContinue(t)
}

func (g *walkPause) Tick(t *tickView) {
	g.Goal.Tick(t)
	if g.fresh {
		g.fresh = false
		return
	}
	if (!g.pausing || !g.pause.ShouldContinue(t)) && g.Goal.ShouldContinue(t) {
		g.pause.Start(t)
		g.pausing = true
	}
	if g.pausing {
		g.pause.Tick(t)
	}
}

func (g *walkPause) RequiresEveryTick() bool { return true }

// lookWalkChoice walks to the player the look goal holds, to within 3
// blocks; it needs no walk under way.
// vanilla: SetWalkTargetFromLookTarget.create(speed, 3)
type lookWalkChoice struct {
	m    *Mob
	pace float64
	look func(*tickView) *Player
}

func (g *lookWalkChoice) Priority() int           { return priorityStroll }
func (g *lookWalkChoice) Flags() GoalFlags        { return GoalMove }
func (g *lookWalkChoice) RequiresEveryTick() bool { return true }
func (g *lookWalkChoice) CanStart(t *tickView) bool {
	return len(g.m.navOf().nav.path) == 0 && g.look(t) != nil
}
func (g *lookWalkChoice) Start(t *tickView) {
	if p := g.look(t); p != nil {
		g.m.seekTo(t.w, p.X, p.Y, p.Z, g.pace, 3)
	}
}
func (g *lookWalkChoice) ShouldContinue(*tickView) bool { return len(g.m.navOf().nav.path) > 0 }
func (g *lookWalkChoice) Tick(*tickView)                {}
func (g *lookWalkChoice) Stop(*tickView)                { g.m.stopSeeking() }

// doNothingGoal is DoNothing: it holds the legs for min to max ticks.
type doNothingGoal struct {
	m         *Mob
	min, max  int
	ran, span int
}

func (g *doNothingGoal) Priority() int           { return priorityStroll }
func (g *doNothingGoal) Flags() GoalFlags        { return GoalMove }
func (g *doNothingGoal) CanStart(*tickView) bool { return true }
func (g *doNothingGoal) Start(*tickView)         { g.ran, g.span = 0, g.min+g.m.Rand.Intn(g.max-g.min+1) }
func (g *doNothingGoal) Tick(*tickView)          { g.ran++ }
func (g *doNothingGoal) Stop(*tickView)          {}
func (g *doNothingGoal) RequiresEveryTick() bool { return true }

// ShouldContinue is DoNothing.tickOrStop: it runs span+1 ticks.
func (g *doNothingGoal) ShouldContinue(*tickView) bool { return g.ran <= g.span }

// uninterruptible marks a goal whose isInterruptable is false: no goal takes
// its controls while it runs.
type uninterruptible interface{ Uninterruptible() bool }

func isUninterruptible(g Goal) bool {
	u, ok := g.(uninterruptible)
	return ok && u.Uninterruptible()
}

// goalSelector is vanilla's GoalSelector: the registered goals in priority
// order and the running ones.
type goalSelector struct {
	goals   []Goal
	running []Goal
	// everyTick selects brain behaviors without GoalSelector's alternate ticks.
	everyTick bool
}

// wire registers a mob's goals, in the priority order the selector walks.
func (sel *goalSelector) wire(goals ...Goal) {
	for i, g := range goals {
		if i > 0 && g.Priority() < goals[i-1].Priority() {
			panic("entity: goals wired out of priority order")
		}
	}
	sel.goals = append(sel.goals, goals...)
}

// clear drops every goal (Mob.removeAllGoals), as the happy ghast does when
// it crosses the age boundary.
func (sel *goalSelector) clear() {
	for _, r := range sel.running {
		r.Stop(nil)
	}
	sel.goals = nil
	sel.running = nil
}

func (sel *goalSelector) isRunning(g Goal) bool {
	return slices.Contains(sel.running, g)
}

func (sel *goalSelector) retire(g Goal) {
	for i, r := range sel.running {
		if r == g {
			sel.running = append(sel.running[:i], sel.running[i+1:]...)
			return
		}
	}
}

// run is one tick of decisions at Mob.serverAiStep's cadence: a full
// selection when age plus id is even (and always on the first tick), else
// only the every-tick goals. It reports whether a goal held the head, so the
// mob need not ease it back to the body.
func (sel *goalSelector) run(m *Mob, t *tickView) (lookHeld bool) {
	// A controlling first rider takes the mount's MOVE, JUMP and LOOK, except a
	// ravager's raider rider, which leaves it to hunt: goals holding them stop,
	// while target goals (which hold none) keep running, so vanilla's TARGET
	// disable under a ravager's non-raider rider is not modelled.
	// vanilla: Mob.getControllingPassenger, Mob.updateControlFlags, Ravager.updateControlFlags,
	// GoalSelector.setControlFlag
	disabled := GoalFlags(0)
	if p := m.controllingPassenger(); p != nil &&
		(m.Type != TypeRavager || !p.inTag(registry.EntityTypeTagIDRaiders)) {
		disabled = GoalMove | GoalJump | GoalLook
	}
	if !sel.everyTick && (m.Age+int(m.ID))%2 != 0 && m.Age > 1 {
		return sel.tickRunningGoals(t, disabled)
	}
	return sel.tick(t, disabled)
}

// holdsLook says whether a running goal holds the head; its look stands
// between its own ticks until the goal stops.
func (sel *goalSelector) holdsLook() bool {
	for _, g := range sel.running {
		if g.Flags()&GoalLook != 0 {
			return true
		}
	}
	return false
}

// tick is GoalSelector.tick: stop goals that no longer hold, offer idle ones
// a start in priority order (several may start in one tick, a look and a
// walk together), pre-empting lower-priority holders of clashing controls,
// then tick each running goal once. Goals on disabled controls are stopped
// and offered nothing.
func (sel *goalSelector) tick(t *tickView, disabled GoalFlags) bool {
	for _, g := range sel.goals {
		if sel.isRunning(g) && (!g.ShouldContinue(t) || g.Flags()&disabled != 0) {
			g.Stop(t)
			sel.retire(g)
		}
	}
	for _, g := range sel.goals {
		if sel.isRunning(g) || g.Flags()&disabled != 0 {
			continue
		}
		blocked := false
		for _, r := range sel.running {
			if (r.Priority() <= g.Priority() || isUninterruptible(r)) && r.Flags()&g.Flags() != 0 {
				blocked = true
				break
			}
		}
		if blocked || !g.CanStart(t) {
			continue
		}
		for _, r := range append([]Goal(nil), sel.running...) {
			if r.Priority() > g.Priority() && r.Flags()&g.Flags() != 0 {
				r.Stop(t)
				sel.retire(r)
			}
		}
		g.Start(t)
		sel.running = append(sel.running, g)
	}
	for _, g := range sel.goals {
		if !sel.isRunning(g) {
			continue
		}
		g.Tick(t)
	}
	return sel.holdsLook()
}

// tickRunningGoals is GoalSelector.tickRunningGoals(false), the tick between
// selections: only running goals that require every-tick updates are
// ticked, skipping disabled controls.
func (sel *goalSelector) tickRunningGoals(t *tickView, disabled GoalFlags) bool {
	for _, g := range sel.goals {
		if sel.isRunning(g) && g.RequiresEveryTick() && g.Flags()&disabled == 0 {
			g.Tick(t)
		}
	}
	return sel.holdsLook()
}

// reducedTickDelay is Mth.reducedTickDelay: a goal delay halved, rounding
// up, because the selector ticks it every other tick, so the felt wait stays
// as written.
func reducedTickDelay(ticks int) int {
	return (ticks + 1) / 2
}

// The idle glances' windows in ticks and start chances, and the head's
// return pace, vanilla's numbers.
const (
	lookChance      = 0.02
	lookForTicks    = 40
	lookForVaries   = 40
	lookAroundFor   = 20
	lookAroundAlike = 20
	headTurnSpeed   = 30
)

// passiveGoals wires every animal's goals in priority order: float, panic,
// breed, tempt, follow parent, stroll and the two glances, with the sheep's
// grazing between following and stroll.
// vanilla: Sheep.registerGoals
func passiveGoals(m *Mob) []Goal {
	goals := []Goal{
		newFloatGoal(m),
		&panicGoal{m: m},
		&breedGoal{m: m, priority: pigPriority(m, priorityPigBreed)},
		&temptGoal{m: m, priority: pigPriority(m, priorityPigTempt)},
		&followParentGoal{m: m, priority: pigPriority(m, priorityPigFollowParent)},
	}
	if m.Type == TypeSheep {
		goals = append(goals, &eatBlockGoal{m: m})
	}
	return append(goals,
		&randomStroll{m: m},
		&lookAtPlayer{m: m, reach: 6}, // vanilla: Cow/Pig/Sheep/Chicken.registerGoals
		&randomLookAround{m: m},
	)
}

// pigPriority is the pig's priority for a passive goal, or 0 (the goal's
// default) for other kinds.
func pigPriority(m *Mob, pig int) int {
	if m.Type == TypePig {
		return pig
	}
	return 0
}

// flyingGoals wires the grown happy ghast's goals and nothing else: float,
// a tempt by happy_ghast_tempt_items (a harness) stopping at seven blocks
// without pathfinding, and the move control's wander. The ghastling runs
// flyingBabyGoals.
// vanilla: HappyGhast.registerGoals, TemptGoal.ForNonPathfinders
func flyingGoals(m *Mob) []Goal {
	return []Goal{
		newFloatGoal(m),
		&temptGoal{m: m, reach: temptFlyReach, stopAt: temptFlyStop, nonPathfinding: true},
		&flyingWander{m: m},
	}
}

// flyingBabyGoals approximates the ghastling's HappyGhastAi brain as goals:
// float, panic, a food-only tempt, following a player or a grown friendly
// mob, wander and the glances. The priorities keep the brain's activity
// order, so following holds the walk ahead of the wander.
// vanilla: HappyGhastAi
func flyingBabyGoals(m *Mob) []Goal {
	return []Goal{
		newFloatGoal(m),
		&panicGoal{m: m},
		&temptGoal{m: m, reach: temptFlyReach, stopAt: babyTemptStop, speedFactor: babyTemptPace, nonPathfinding: true},
		&babyFollowGoal{m: m},
		&flyingWander{m: m},
		&lookAtPlayer{m: m},
		&randomLookAround{m: m},
	}
}

// The ghastling brain's numbers: the tempt pace and stop
// (HappyGhastAi.SPEED_MULTIPLIER_WHEN_TEMPTED, BABY_GHAST_CLOSE_ENOUGH_DIST)
// and the following's speed, window and replan cadence. The window is at
// least ADULT_FOLLOW_RANGE's minInclusive of 3 and at most 16: BabyFollowAdult
// allows up to maxInclusive + 1, but both sensors only see a target within
// the happy ghast's FOLLOW_RANGE of 16 (HappyGhast.createAttributes,
// PlayerSensor.doTick, Sensor.isEntityTargetable).
const (
	babyTemptPace    = 1.25
	babyTemptStop    = 3.0
	babyFollowPace   = 1.1 // SPEED_MULTIPLIER_WHEN_FOLLOWING_ADULT
	babyFollowNear   = 3.0
	babyFollowLeash  = 16.0 // HappyGhast FOLLOW_RANGE
	babyFollowReplan = 10
)

// babyFollowGoal is BabyFollowAdult as a goal: the ghastling follows the
// nearest player if it is in the follow window, or failing that the nearest
// grown followable_friendly_mobs mob if it is in the window, at
// SPEED_MULTIPLIER_WHEN_FOLLOWING_ADULT.
// The target is re-found each tick, as the brain re-resolves its memory.
// Unlike FollowParentGoal it holds MOVE, so it takes the walk from the
// wander as the brain's activity order does.
// vanilla: HappyGhastAi.getActivities, BabyFollowAdult, AdultSensorAnyType
type babyFollowGoal struct {
	m      *Mob
	recalc int
}

func (g *babyFollowGoal) Priority() int    { return priorityFollowParent }
func (g *babyFollowGoal) Flags() GoalFlags { return GoalMove }

// babyFollowInWindow reports whether a squared distance is in the follow window.
func babyFollowInWindow(d float64) bool {
	return d >= babyFollowNear*babyFollowNear && d <= babyFollowLeash*babyFollowLeash
}

// nearestTarget is the position of the target to follow. Each kind has one
// candidate, its nearest, and only that one is tested against the window: a
// nearest one closer than the window falls through rather than passing to the
// next nearest. The player behaviour runs at priority 2, before the adult one
// at 3. vanilla: PlayerSensor.doTick, AdultSensorAnyType, BabyFollowAdult
func (g *babyFollowGoal) nearestTarget(t *tickView) (x, y, z float64, ok bool) {
	best := math.MaxFloat64
	for _, p := range t.players {
		if dd := Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z); dd < best {
			best, x, y, z = dd, p.X, p.Y, p.Z
		}
	}
	if babyFollowInWindow(best) {
		return x, y, z, true
	}
	best = math.MaxFloat64
	for _, o := range t.others {
		if o == g.m || !o.Alive() || o.IsBaby() || !o.inTag(registry.EntityTypeTagIDFollowableFriendlyMobs) {
			continue
		}
		if dd := Dist2(g.m.X, g.m.Y, g.m.Z, o.X, o.Y, o.Z); dd < best {
			best, x, y, z = dd, o.X, o.Y, o.Z
		}
	}
	return x, y, z, babyFollowInWindow(best)
}

func (g *babyFollowGoal) CanStart(t *tickView) bool {
	if !g.m.IsBaby() {
		return false
	}
	_, _, _, ok := g.nearestTarget(t)
	return ok
}

func (g *babyFollowGoal) Start(*tickView) { g.recalc = 0 }

func (g *babyFollowGoal) ShouldContinue(t *tickView) bool {
	_, _, _, ok := g.nearestTarget(t)
	return ok
}

func (g *babyFollowGoal) Tick(t *tickView) {
	if g.recalc--; g.recalc > 0 {
		return
	}
	g.recalc = reducedTickDelay(babyFollowReplan)
	x, y, z, ok := g.nearestTarget(t)
	if !ok {
		return
	}
	g.m.seekToFly(x, y, z, babyFollowPace)
}

func (g *babyFollowGoal) Stop(*tickView)          { g.m.stopSeeking() }
func (g *babyFollowGoal) RequiresEveryTick() bool { return false }

// golemGoals wires the golems' simulated goals: the snow golem's snowballs and
// wander, both kinds' glances, and the iron golem's village defense and melee.
// vanilla: SnowGolem.registerGoals, IronGolem.registerGoals
func golemGoals(m *Mob) []Goal {
	if m.Type == TypeIronGolem {
		return ironGolemGoals(m)
	}
	goals := []Goal{}
	if m.Type == TypeSnowGolem {
		goals = append(goals,
			&rangedGoal{m: m, speed: snowballSpeed, interval: snowballInterval, radius: snowballRadius, fire: m.throwSnowball},
			&mobTargetGoal{m: m, priority: priorityPanic, interval: snowballScanInterval,
				matches: func(o *Mob) bool { return Enemy(o.Type) }},
			&randomStroll{m: m})
	}
	return append(goals,
		&lookAtPlayer{m: m, reach: 6}, // vanilla: SnowGolem.registerGoals
		&randomLookAround{m: m},
	)
}

// golemKind reports the pumpkin-built golems, the MobCategory.MISC kinds
// here.
func golemKind(t MobType) bool {
	return t == TypeSnowGolem || t == TypeIronGolem
}

// floatGoal is FloatGoal: in water deeper than the jump threshold, or in any
// lava, the body kicks on four ticks in five. Wading depth is ignored.
type floatGoal struct{ m *Mob }

// newFloatGoal also sets the navigation's canFloat for life, which makes the
// liquid jump give the water's kick instead of a non-floater's full leap.
func newFloatGoal(m *Mob) *floatGoal {
	m.canFloat = true
	return &floatGoal{m: m}
}

func (g *floatGoal) Priority() int { return priorityFloat }
func (g *floatGoal) Flags() GoalFlags {
	return GoalJump
}

// CanStart is FloatGoal.canUse: water over the threshold, or any lava, by
// the fluid reading taken at the start of the tick.
func (g *floatGoal) CanStart(*tickView) bool {
	return (g.m.InWater() && g.m.waterFluid.height > g.m.fluidJumpThreshold()) || g.m.InLava()
}

func (g *floatGoal) Start(*tickView) {}

// ShouldContinue is the base Goal's canContinueToUse: canUse again.
func (g *floatGoal) ShouldContinue(*tickView) bool {
	return (g.m.InWater() && g.m.waterFluid.height > g.m.fluidJumpThreshold()) || g.m.InLava()
}

// Tick is FloatGoal.tick: on four ticks of five it asks the jump control for
// a one-tick kick.
func (g *floatGoal) Tick(*tickView) {
	if g.m.Rand.Float64() < 0.8 {
		g.m.jump = true
	}
}

func (g *floatGoal) Stop(*tickView) {}

// RequiresEveryTick is FloatGoal's override, so the kick does not wait on
// the selector's cadence.
func (g *floatGoal) RequiresEveryTick() bool { return true }

// panicCauses is the damage-type tag the panic answers to.
const panicCauses = registry.DamageTypeTagIDPanicCauses

// panicEnvironmentalCauses is the narrower tag a wolf panics on.
const panicEnvironmentalCauses = registry.DamageTypeTagIDPanicEnvironmentalCauses

// Panic numbers: a burning body looks for water within 5 sideways and 1
// vertically (PanicGoal.lookForWater); DefaultRandomPos.getPos(mob, 5, 4)
// spreads ±5 on x and z and ±4 on y.
const (
	panicWaterRange   = 5
	panicWaterRise    = 1
	panicFleeSpreadXZ = 5
	panicFleeSpreadY  = 4
)

// defaultRandomTries is the ten tries DefaultRandomPos.getPos makes.
const defaultRandomTries = 10

// panicGoal runs while the starting hurt stays fresh: to the nearest water
// when burning and some is in reach, else to the best of ten walkable spots.
// vanilla: PanicGoal
type panicGoal struct {
	m *Mob
	// brain keeps AnimalPanic active for 100–120 ticks, choosing another leg when navigation ends.
	brain     bool
	remaining int
	// turtle marks TurtlePanicGoal: top priority, always looking for water
	// within seven blocks, at 1.2.
	// vanilla: Turtle.TurtlePanicGoal
	turtle bool
	// pace replaces the default panic pace where nonzero.
	pace float64
	// priority replaces priorityPanic where nonzero.
	priority int
	// causes replaces panicCauses where nonzero; babyCauses replaces it
	// for a baby.
	causes, babyCauses registry.DamageTypeTagID
	// tame marks TamableAnimalPanicGoal: a pet 12+ blocks from its owner
	// teleports to them each tick.
	// vanilla: TamableAnimal.TamableAnimalPanicGoal.tick
	tame bool
	// refuse stops the goal from starting where it returns true.
	refuse func() bool
	// onStart runs when the goal starts.
	onStart func()
	fleeX   float64
	fleeY   float64
	fleeZ   float64
}

func (g *panicGoal) Priority() int {
	if g.turtle {
		return 0
	}
	if g.priority != 0 {
		return g.priority
	}
	return priorityPanic
}
func (g *panicGoal) Flags() GoalFlags { return GoalMove }

// CanStart is PanicGoal.canUse: only a fresh hurt of a panic cause starts
// it, and a burning body runs to water in range whatever the hurt. Without
// water, ten tries pick a spot; finding none starts nothing.
func (g *panicGoal) CanStart(t *tickView) bool {
	if g.refuse != nil && g.refuse() {
		return false
	}
	causes := panicCauses
	if g.causes != 0 {
		causes = g.causes
	}
	if g.babyCauses != 0 && g.m.IsBaby() {
		causes = g.babyCauses
	}
	// An expired source belongs to no tag.
	if src := g.m.lastDamageSource(); src == 0 || !src.InTag(causes) {
		return false
	}
	if g.brain {
		return true
	}
	if g.turtle {
		if cell, ok := lookForWater(t.w, g.m, turtlePanicWater); ok {
			g.setFleeTarget(cell)
			return true
		}
	} else if g.m.OnFire() {
		if cell, ok := lookForWater(t.w, g.m, panicWaterRange); ok {
			g.setFleeTarget(cell)
			return true
		}
	}
	cell, ok := defaultRandomPos(t.w, g.m, panicFleeSpreadXZ, panicFleeSpreadY)
	if !ok {
		return false
	}
	g.setFleeTarget(cell)
	return true
}

// setFleeTarget sets the leg's target to the centre of the cell's floor.
func (g *panicGoal) setFleeTarget(cell Waypoint) {
	g.fleeX, g.fleeY, g.fleeZ = float64(cell.X)+0.5, float64(cell.Y), float64(cell.Z)+0.5
}

// Start orders the flee once: re-ordering every selection would re-arm the
// stuck watch before its window closes, and a flee that makes no way would
// never be given up.
// vanilla: PanicGoal.start
func (g *panicGoal) Start(t *tickView) {
	if g.onStart != nil {
		g.onStart()
	}
	if g.brain {
		g.remaining = 100 + g.m.Rand.Intn(21) // vanilla: AnimalPanic.PANIC_MIN_DURATION / PANIC_MAX_DURATION
		g.m.stopSeeking()
		return
	}
	pace := panicPace
	if p := g.m.kind().panicPace; p != 0 {
		pace = p
	}
	if g.turtle {
		pace = 1.2
	}
	if g.pace != 0 {
		pace = g.pace
	}
	g.m.seekTo(t.w, g.fleeX, g.fleeY, g.fleeZ, pace, navReach)
}

// ShouldContinue is PanicGoal.canContinueToUse: the flee's walk not done.
func (g *panicGoal) ShouldContinue(*tickView) bool {
	if g.brain {
		return g.remaining > 0
	}
	if g.m.kind().flying {
		return g.m.navOf().nav.flyActive
	}
	return len(g.m.navOf().nav.path) > 0
}

func (g *panicGoal) Tick(t *tickView) {
	if g.tame {
		if owner := g.m.ownerIn(t); owner != nil && !g.m.unableToMoveToOwner(owner) &&
			Dist2(g.m.X, g.m.Y, g.m.Z, owner.X, owner.Y, owner.Z) >= teleportOwnerDistSq {
			g.m.teleportAround(t.w, owner)
		}
	}
	if !g.brain {
		return
	}
	g.remaining--
	if len(g.m.nav.path) > 0 {
		return
	}
	var cell Waypoint
	var ok bool
	if g.m.OnFire() {
		cell, ok = animalPanicWater(t.w, g.m)
	}
	if !ok {
		cell, ok = landRandomPos(t.w, g.m, panicFleeSpreadXZ, panicFleeSpreadY)
	}
	if ok {
		g.m.seekTo(t.w, float64(cell.X)+0.5, float64(cell.Y), float64(cell.Z)+0.5, g.pace, 0)
	}
}

func (g *panicGoal) Stop(*tickView) {
	if g.brain {
		return
	}
	g.m.stopSeeking()
}

func (g *panicGoal) RequiresEveryTick() bool { return g.brain }

// panicking is PathfinderMob.isPanicking for goal-based mobs: the panic goal
// holds the legs.
func (m *Mob) panicking() bool {
	for _, g := range m.ai.running {
		if _, ok := g.(*panicGoal); ok {
			return true
		}
	}
	return false
}

// lookForWater is PanicGoal.lookForWater: a body in a cell with a collision
// shape (farmland, path, soul sand, mud, deep snow) never looks; otherwise
// it returns the nearest water in BlockPos.findClosestMatch's order.
func lookForWater(w World, m *Mob, within int32) (Waypoint, bool) {
	feet := m.cell()
	if blockExtent(w.BlockAt(feet.X, feet.Y, feet.Z)) != 0 {
		return Waypoint{}, false
	}
	return closestWater(w, feet, within, panicWaterRise, false)
}

// animalPanicWater is AnimalPanic.lookForWater: lookForWater within five,
// except that a body whose width rounds up to 2 takes only a cell whose
// south, east and south-east neighbours are water too.
func animalPanicWater(w World, m *Mob) (Waypoint, bool) {
	feet := m.cell()
	if blockExtent(w.BlockAt(feet.X, feet.Y, feet.Z)) != 0 {
		return Waypoint{}, false
	}
	return closestWater(w, feet, panicWaterRange, panicWaterRise, math.Ceil(m.width()) == 2)
}

// closestWater is BlockPos.findClosestMatch for water over withinManhattan's
// box: the first cell by increasing manhattan distance, ties in
// withinManhattan's order (x, then y, ascending, +z before −z). pool also
// asks for water at +z, +x and +x+z.
func closestWater(w World, from Waypoint, xz, upOrDown int32, pool bool) (Waypoint, bool) {
	var best Waypoint
	nearest := int32(-1)
	for dx := -xz; dx <= xz; dx++ {
		for dy := -upOrDown; dy <= upOrDown; dy++ {
			for dz := xz; dz >= -xz; dz-- {
				d := max(dx, -dx) + max(dy, -dy) + max(dz, -dz)
				if nearest >= 0 && d >= nearest {
					continue
				}
				cell := Waypoint{from.X + dx, from.Y + dy, from.Z + dz}
				if rules.IsWater(w.BlockAt(cell.X, cell.Y, cell.Z)) && (!pool ||
					rules.IsWater(w.BlockAt(cell.X, cell.Y, cell.Z+1)) &&
						rules.IsWater(w.BlockAt(cell.X+1, cell.Y, cell.Z)) &&
						rules.IsWater(w.BlockAt(cell.X+1, cell.Y, cell.Z+1))) {
					best, nearest = cell, d
				}
			}
		}
	}
	return best, nearest >= 0
}

// randomPosTries is RandomPos.generateRandomPos: ten picks scored by
// walkTargetValue, the first strictly best kept. resolve turns a pick into
// the cell to walk to, runs only for a pick that would beat the best, and
// may reject it.
func randomPosTries(w World, m *Mob, pick func() (Waypoint, bool), resolve func(Waypoint) (Waypoint, bool)) (Waypoint, bool) {
	return randomPosScored(m.walkTargetValue, w, m, pick, resolve)
}

// randomPosScored is randomPosTries ranking spots by score.
func randomPosScored(score func(World, Waypoint) float64, w World, m *Mob, pick func() (Waypoint, bool), resolve func(Waypoint) (Waypoint, bool)) (Waypoint, bool) {
	best := math.Inf(-1)
	var target Waypoint
	found := false
	for range defaultRandomTries {
		spot, ok := pick()
		if !ok {
			continue
		}
		value := score(w, spot)
		if value <= best {
			continue
		}
		if cell, ok := resolve(spot); ok {
			best, target, found = value, cell, true
		}
	}
	return target, found
}

// randomDirection is RandomPos.generateRandomDirection added to the feet.
func randomDirection(m *Mob, xzSpread, ySpread int) Waypoint {
	from := m.cell()
	dx, dy, dz := randomOffset(m, xzSpread, ySpread)
	return Waypoint{from.X + int32(dx), from.Y + int32(dy), from.Z + int32(dz)}
}

// randomOffset is RandomPos.generateRandomDirection.
func randomOffset(m *Mob, xzSpread, ySpread int) (dx, dy, dz int) {
	dx = m.Rand.Intn(2*xzSpread+1) - xzSpread
	dy = m.Rand.Intn(2*ySpread+1) - ySpread
	dz = m.Rand.Intn(2*xzSpread+1) - xzSpread
	return dx, dy, dz
}

// defaultRandomPos is DefaultRandomPos.getPos(mob, xzSpread, ySpread) as
// PanicGoal.findRandomPosition uses it: the best-scoring walkable, harmless
// cell of ten offsets, or false when all are rejected.
func defaultRandomPos(w World, m *Mob, xzSpread, ySpread int) (Waypoint, bool) {
	return randomNavigablePos(w, m, xzSpread, ySpread, false)
}

// randomNavigablePos also admits the unsupported water nodes used by a swimming brain.
func randomNavigablePos(w World, m *Mob, xzSpread, ySpread int, swimming bool) (Waypoint, bool) {
	foot := footprintOf(m.width(), m.height())
	m.setFireMaluses(&foot)
	return randomPosTries(w, m, func() (Waypoint, bool) {
		spot := randomDirection(m, xzSpread, ySpread)
		if swimming {
			return spot, guardianCellOK(w, m, spot, m.homeRestricted(xzSpread))
		}
		return spot, walkableAt(w, foot, spot.X, spot.Y, spot.Z) && !hasMalus(w, m, spot)
	}, func(spot Waypoint) (Waypoint, bool) { return spot, true })
}

// strollPos is WaterAvoidingRandomStrollGoal.getPosition: in water the land
// pick over (15, 7), falling back to the plain one; otherwise the land pick
// over (10, 7), or on a 1-in-1000 roll the plain pick.
func strollPos(w World, m *Mob) (Waypoint, bool) {
	if m.IsStrider() {
		return plainStrollPos(w, m)
	}
	if m.InWater() {
		if cell, ok := landRandomPos(w, m, strollWaterSpreadXZ, strollSpreadY); ok {
			return cell, true
		}
		return plainStrollPos(w, m)
	}
	if m.Rand.Float32() >= strollPlainChance {
		return landRandomPos(w, m, strollSpreadXZ, strollSpreadY)
	}
	return plainStrollPos(w, m)
}

// plainStrollPos is the stroll's DefaultRandomPos.getPos(mob, 10, 7): an
// offset over a stable destination and not harmful, resolved to the nearest
// standing cell, where vanilla's pathfinder would end the walk.
func plainStrollPos(w World, m *Mob) (Waypoint, bool) {
	return randomPosTries(w, m, func() (Waypoint, bool) {
		spot := randomDirection(m, strollSpreadXZ, strollSpreadY)
		return spot, isStableDestination(w, m, spot) && !hasMalus(w, m, spot)
	}, func(spot Waypoint) (Waypoint, bool) { return resolveStrollSpot(w, m, spot) })
}

// landRandomPos is LandRandomPos.getPos(mob, xzSpread, ySpread): an offset
// over a stable destination, moved up out of solid blocks, rejected when
// water or harmful.
func landRandomPos(w World, m *Mob, xzSpread, ySpread int) (Waypoint, bool) {
	return landRandomPosScored(m.walkTargetValue, w, m, xzSpread, ySpread)
}

// landRandomPosScored is landRandomPos ranking spots by score.
func landRandomPosScored(score func(World, Waypoint) float64, w World, m *Mob, xzSpread, ySpread int) (Waypoint, bool) {
	return randomPosScored(score, w, m, func() (Waypoint, bool) {
		return landSpot(w, m, randomDirection(m, xzSpread, ySpread))
	}, func(spot Waypoint) (Waypoint, bool) { return resolveStrollSpot(w, m, spot) })
}

// landSpot validates a land pick and climbs out of solid blocks before scoring.
// vanilla: LandRandomPos.movePosUpOutOfSolid
func landSpot(w World, m *Mob, spot Waypoint) (Waypoint, bool) {
	if spot.Y < w.MinY() || spot.Y >= w.MaxY() || !isStableDestination(w, m, spot) {
		return spot, false
	}
	for spot.Y < w.MaxY() && registry.IsSolid(w.BlockAt(spot.X, spot.Y, spot.Z)) {
		spot.Y++
	}
	return spot, !rules.IsWater(w.BlockAt(spot.X, spot.Y, spot.Z)) && !hasMalus(w, m, spot)
}

// isStableDestination is PathNavigation.isStableDestination (not overridden
// by GroundPathNavigation): the block below is a full opaque cube.
// StriderNavigation also accepts lava in the destination cell.
func isStableDestination(w World, m *Mob, spot Waypoint) bool {
	if m.Type == TypeDolphin {
		return !registry.IsSolidRender(w.BlockAt(spot.X, spot.Y, spot.Z))
	}
	if m.IsStrider() && rules.IsLava(w.BlockAt(spot.X, spot.Y, spot.Z)) {
		return true
	}
	return registry.IsSolidRender(w.BlockAt(spot.X, spot.Y-1, spot.Z))
}

// resolveStrollSpot is the nearest standing cell to a pick, where vanilla's
// pathfinder would end the walk.
func resolveStrollSpot(w World, m *Mob, spot Waypoint) (Waypoint, bool) {
	foot := footprintOf(m.width(), m.height())
	foot.amphibious = m.Type == TypeDrowned
	m.setFireMaluses(&foot)
	return walkableNear(w, foot, float64(spot.X)+0.5, float64(spot.Y), float64(spot.Z)+0.5)
}

// hasMalus rejects any nonzero price of the candidate's static path type.
// vanilla: GoalUtils.hasMalus
func hasMalus(w World, m *Mob, spot Waypoint) bool {
	t := positionPathType(w, spot.X, spot.Y, spot.Z)
	switch t {
	case pathBlockedType, pathLeaves, pathFence, pathDoorWoodClosed, pathDoorIronClosed:
		return true
	case pathRail:
		// Static classification does not turn rails into UNPASSABLE_RAIL.
		return false
	}
	if m.Type == TypeTurtle && t == pathDoorOpen {
		// vanilla: Turtle.<init>
		return true
	}
	foot := footprintOf(m.width(), m.height())
	m.setFireMaluses(&foot)
	return foot.malusOfType(t) != 0
}

// walkTargetValue is PathfinderMob.getWalkTargetValue with Animal's
// override: 10 over grass, else getPathfindingCostFromLightLevels' light
// cost; a Monster scores that cost negated. A non-animal creature scores 0. A
// mooshroom scores mycelium 10 instead of grass.
// vanilla: MushroomCow.getWalkTargetValue
func (m *Mob) walkTargetValue(w World, cell Waypoint) float64 {
	if m.IsGuardian() {
		return guardianWalkTargetValue(w, cell)
	}
	if m.hoglinKind() {
		return m.hoglinWalkTargetValue(w, cell)
	}
	if m.Type == TypeStrider {
		return m.striderWalkTargetValue(w, cell)
	}
	if m.Type == TypeSilverfish && rules.IsInfestableHost(w.BlockAt(cell.X, cell.Y-1, cell.Z)) {
		// vanilla: Silverfish.getWalkTargetValue
		return 10
	}
	switch m.Type {
	case TypeEnderman, TypePillager, TypeCreaking, TypeWarden, TypeGhast, TypeShulker:
		// vanilla: Enderman, Pillager, Creaking and Warden.getWalkTargetValue;
		// a ghast is no PathfinderMob and a shulker an AbstractGolem
		return 0
	case TypeGiant:
		// vanilla: Giant.getWalkTargetValue takes the animal sign
		return -monsterWalkTargetValue(w, cell.X, cell.Y, cell.Z)
	}
	if m.monsterClass() {
		// vanilla: Monster.getWalkTargetValue
		return monsterWalkTargetValue(w, cell.X, cell.Y, cell.Z)
	}
	if !ageableAnimal(m.Type) && m.Type != TypeParrot {
		return 0
	}
	switch m.Type {
	case TypeAxolotl, TypeSquid, TypeGlowSquid, TypeDolphin, TypeNautilus, TypeZombieNautilus:
		// vanilla: Axolotl.getWalkTargetValue, AbstractNautilus.getWalkTargetValue;
		// Squid and Dolphin are no Animal
		return 0
	}
	if m.Type == TypeTurtle {
		// vanilla: Turtle.getWalkTargetValue
		if (!m.goingHome && rules.IsWater(w.BlockAt(cell.X, cell.Y, cell.Z))) || isSandAt(w, cell.X, cell.Y-1, cell.Z) {
			return 10
		}
		return -monsterWalkTargetValue(w, cell.X, cell.Y, cell.Z)
	}
	below := registry.BlockOf(w.BlockAt(cell.X, cell.Y-1, cell.Z))
	if m.Type == TypeMooshroom {
		if below == registry.BlockMycelium {
			return 10
		}
	} else if below == registry.BlockGrassBlock {
		return 10
	}
	return -monsterWalkTargetValue(w, cell.X, cell.Y, cell.Z)
}

// BreedGoal's numbers: the mate search range (TargetingConditions.range's
// 8), the breeding distance (distanceToSqr's 9), the courtship length in
// goal ticks, and the head's paces on the mate (LookControl.setLookAt's 10
// a tick on yaw, Mob.getMaxHeadXRot's 40 on pitch).
const (
	breedScanRange = 8.0
	breedDistance  = 3.0
	breedGiveUp    = 60
	breedHeadTurn  = 10
	breedPitchTurn = 40
	breedWait      = 60
	breedPace      = 1.0
	breedXpDie     = 7

	rabbitBreedPace = 0.8 // Rabbit, Cat and Ocelot.registerGoals
)

// breedGoal is BreedGoal: a loving body walks to the nearest loving adult of
// its kind in range, and after courting long enough close together they
// breed; parents are settled here and the child, hearts and experience are
// left to the event. After the give-up count the courtship can restart
// while love lasts.
// vanilla: BreedGoal, Animal.canMate
type breedGoal struct {
	m *Mob
	// priority is the goal's vanilla registration; zero means the passive
	// set's.
	priority int
	// turtle marks TurtleBreedGoal: a turtle carrying an egg does not court, and
	// breeding yields an egg rather than a child.
	// vanilla: Turtle.TurtleBreedGoal
	turtle   bool
	partner  *Mob
	loveTime int
	// brainWait is AnimalMakeLove's duration in full ticks.
	brainWait int
	// onStart runs when the goal starts.
	onStart func()
}

func (g *breedGoal) Priority() int {
	if g.priority != 0 {
		return g.priority
	}
	return priorityBreed
}
func (g *breedGoal) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *breedGoal) CanStart(t *tickView) bool {
	if !g.m.IsInLove() || (g.turtle && g.m.HasEgg) || (g.m.Type == TypeFrog && (g.m.pregnant || g.m.InWater())) {
		return false
	}
	g.partner = g.freePartner(t)
	return g.partner != nil && (g.m.Type != TypePanda || g.m.pandaMayBreed(t))
}

// pace is the speed modifier of the walk to the mate.
// vanilla: Rabbit's BreedGoal(this, 0.8)
func (g *breedGoal) pace() float64 {
	if g.m.Type == TypeNautilus {
		return 0.4
	}
	if g.m.Type == TypeAxolotl {
		return 0.2
	}
	if g.m.Type == TypeRabbit || g.m.Type == TypeCat || g.m.Type == TypeOcelot {
		return rabbitBreedPace
	}
	return breedPace
}

// freePartner is BreedGoal.getFreePartner: the nearest live, loving,
// non-panicking body of the kind in range, ties to the first found
// (vanilla's strict <).
func (g *breedGoal) freePartner(t *tickView) *Mob {
	var nearest *Mob
	best := math.MaxFloat64
	for _, o := range t.others {
		// vanilla: BreedGoal(this, 1.0, AbstractHorse.class) courts across kinds
		if IsEquine(g.m.Type) {
			if !o.Alive() || o.panicking() || !g.m.horseCanMate(o) {
				continue
			}
		} else if o == g.m || o.Type != g.m.Type || !o.Alive() || !o.IsInLove() || o.panicking() {
			continue
		}
		if g.m.Type == TypeWolf && !g.m.wolfCanMate(o) {
			continue
		}
		// vanilla: Sniffer.canMate
		if g.m.Type == TypeSniffer && (!g.m.snifferCanMate() || !o.snifferCanMate()) {
			continue
		}
		// vanilla: Cat.canMate
		if g.m.Type == TypeCat && (!g.m.Tamable.Tame || !o.Tamable.Tame) {
			continue
		}
		d := Dist2(g.m.X, g.m.Y, g.m.Z, o.X, o.Y, o.Z)
		if d > breedScanRange*breedScanRange || d >= best {
			continue
		}
		nearest, best = o, d
	}
	return nearest
}

func (g *breedGoal) Start(*tickView) {
	if g.onStart != nil {
		g.onStart()
	}
	if g.m.ai.everyTick {
		// vanilla: AnimalMakeLove.start
		g.brainWait = 60 + g.m.Rand.Intn(50)
	}
}

// timing uses the selector's cadence; AnimalMakeLove runs through its birth time.
func (g *breedGoal) timing() (wait, giveUp int) {
	if g.m.ai.everyTick {
		// loveTime counts the start tick, before AnimalMakeLove's duration elapses.
		return g.brainWait + 1, g.brainWait + 1
	}
	return reducedTickDelay(breedWait), breedGiveUp
}

// ShouldContinue is BreedGoal.canContinueToUse: the partner lives, loves and
// is not panicking, and the courtship count is not spent. A partner unloaded
// with its chunk counts as dead.
func (g *breedGoal) ShouldContinue(t *tickView) bool {
	if g.partner == nil || !slices.Contains(t.others, g.partner) {
		return false
	}
	_, giveUp := g.timing()
	return g.partner.Alive() && g.partner.IsInLove() && !g.partner.panicking() && g.loveTime < giveUp
}

func (g *breedGoal) Tick(t *tickView) {
	p := g.partner
	// The head held on the mate, the way the look control eases it round.
	dx, dz := p.X-g.m.X, p.Z-g.m.Z
	g.m.HeadYaw = turnToward(g.m.HeadYaw, float32(yawToward(dx, dz)), breedHeadTurn)
	g.m.Pitch = turnToward(g.m.Pitch,
		float32(pitchToward(dx, (p.Y+p.eyeHeight())-(g.m.Y+g.m.eyeHeight()), dz)), breedPitchTurn)
	// Re-ordered every tick of the goal.
	// vanilla: BreedGoal.tick
	g.m.seekTo(t.w, p.X, p.Y, p.Z, g.pace(), navReach)
	g.loveTime++
	wait, _ := g.timing()
	if g.loveTime < wait ||
		Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z) >= breedDistance*breedDistance {
		return
	}
	// vanilla: BreedGoal.breed → spawnChildFromBreeding. The parents settle at
	// once so one courtship cannot count twice; the event asks for the child,
	// hearts and orb. The experience is the mother's nextInt(7) + 1.
	g.m.BredWith(p)
	if g.m.Type == TypeFrog {
		g.m.pregnant = true
	}
	if g.turtle {
		g.m.SetMetadataHasEgg(true)
	}
	t.events = append(t.events, MobEvent{
		Mob:     g.m,
		Kind:    MobBred,
		Partner: p,
		Amount:  g.m.Rand.Intn(breedXpDie) + 1,
	})
}

// Stop is BreedGoal.stop: partner and count reset, so the courtship can
// restart while love lasts.
func (g *breedGoal) Stop(*tickView) {
	g.partner = nil
	g.loveTime = 0
}

// RequiresEveryTick is false; BreedGoal does not override it.
func (g *breedGoal) RequiresEveryTick() bool { return false }

// FollowParentGoal's numbers: the search box (8 sideways, 4 vertically
// around the baby's box), the 3-block no-follow distance, the 16-block
// give-up, and the goal ticks between replans.
const (
	followScanRange = 8.0
	followScanRise  = 4.0
	followNear      = 3.0
	followLeash     = 16.0
	followReplan    = 10
)

// followParentGoal is FollowParentGoal: a baby walks to the nearest grown
// body of its kind in the search box at the kind's pace, replanning every
// few goal ticks, until within three blocks. Vanilla registers it with no
// controls, so it neither pre-empts nor waits for another goal.
type followParentGoal struct {
	m      *Mob
	parent *Mob
	recalc int
	// priority overrides the default when non-zero.
	priority int
	// ranged, when set, replaces the distances with a brain's BabyFollowAdult:
	// follow starts at ranged.min and stops within min-1 blocks Manhattan, within ranged.max+1 of an
	// adult found within ranged.scan.
	ranged       *followRange
	sensedParent **Mob
	oneShot      bool
}

// followRange is BabyFollowAdult's UniformInt follow range and the nearest-
// adult sensor's reach.
type followRange struct{ min, max, scan float64 }

func (g *followParentGoal) near() float64 {
	if g.ranged != nil {
		return g.ranged.min
	}
	return followNear
}

func (g *followParentGoal) Priority() int {
	if g.priority != 0 {
		return g.priority
	}
	return priorityFollowParent
}
func (g *followParentGoal) Flags() GoalFlags { return 0 }

// CanStart is FollowParentGoal.canUse: a baby with a grown body of its kind
// in the search box, but not already too close to follow.
func (g *followParentGoal) CanStart(t *tickView) bool {
	if g.oneShot && len(g.m.nav.path) > 0 {
		return false
	}
	if !g.m.IsBaby() {
		return false
	}
	var nearest *Mob
	best := math.MaxFloat64
	scan := followScanRange
	if g.ranged != nil {
		scan = g.ranged.scan
	}
	for _, o := range t.others {
		if g.sensedParent != nil {
			break
		}
		if o == g.m || o.Type != g.m.Type || !o.Alive() || o.IsBaby() {
			continue
		}
		if !boxesOverlap(
			g.m.X, g.m.Y-followScanRise, g.m.Z,
			g.m.width()+2*scan, g.m.height()+2*followScanRise,
			o.X, o.Y, o.Z, o.width(), o.height()) {
			continue
		}
		if d := Dist2(g.m.X, g.m.Y, g.m.Z, o.X, o.Y, o.Z); d <= best {
			best, nearest = d, o
		}
	}
	if g.sensedParent != nil {
		nearest = *g.sensedParent
		if nearest != nil {
			best = Dist2(g.m.X, g.m.Y, g.m.Z, nearest.X, nearest.Y, nearest.Z)
		}
	}
	if nearest == nil || best < g.near()*g.near() {
		return false
	}
	if g.ranged != nil && best >= (g.ranged.max+1)*(g.ranged.max+1) {
		return false
	}
	g.parent = nearest
	return true
}

func (g *followParentGoal) Start(*tickView) {
	g.recalc = 0
}

// ShouldContinue is FollowParentGoal.canContinueToUse: still a baby, the
// parent still present, and neither close enough to stop nor past the leash.
func (g *followParentGoal) ShouldContinue(t *tickView) bool {
	if g.oneShot {
		return false
	}
	if !g.m.IsBaby() || g.parent == nil || t.mobByID(g.parent.ID) != g.parent || !g.parent.Alive() {
		return false
	}
	d := Dist2(g.m.X, g.m.Y, g.m.Z, g.parent.X, g.parent.Y, g.parent.Z)
	if g.ranged != nil {
		// vanilla: MoveToTargetSink.reachedTarget, block-position Manhattan
		// distance against closeEnoughDist (min-1)
		reached := float64(manhattan(g.m.cell(), g.parent.cell())) <= g.ranged.min-1
		far := g.ranged.max + 1
		return !reached && d <= far*far
	}
	return d >= followNear*followNear && d <= followLeash*followLeash
}

func (g *followParentGoal) Tick(t *tickView) {
	if g.recalc--; g.recalc > 0 {
		return
	}
	g.recalc = reducedTickDelay(followReplan)
	pace := g.m.kind().followPace
	if g.m.Type == TypeAxolotl && !g.m.InWater() {
		pace = 0.15
	}
	g.m.seekTo(t.w, g.parent.X, g.parent.Y, g.parent.Z, pace, navReach)
}

// Stop forgets the parent; the replan count restarts with the next start.
func (g *followParentGoal) Stop(*tickView) {
	if g.ranged != nil && !g.oneShot {
		// vanilla: MoveToTargetSink.stop ends the walk
		g.m.stopSeeking()
	}
	g.parent = nil
}

// RequiresEveryTick is false; FollowParentGoal does not override it.
func (g *followParentGoal) RequiresEveryTick() bool { return false }

// temptGoal follows a lure: the nearest player in range holding the mob's
// food in either hand is walked to at the tempt pace and looked at, while
// they stay near and keep holding it. Within the stop distance the walk ends
// but the look holds; when the lure goes the goal stops and waits out a
// calm-down.
type temptGoal struct {
	m *Mob
	// priority is the goal's vanilla registration; zero means the passive
	// set's.
	priority int
	// calmDown is TemptGoal.calmDown, the selections left before a lure works
	// again.
	calmDown int
	// reach, stopAt and nonPathfinding are TemptGoal.ForNonPathfinders for the
	// happy ghast: TEMPT_RANGE's 16 instead of 10, a 7-block stop instead of
	// 2.5, and a random point toward the player's eyes instead of a path. Zero
	// reach and stopAt mean the defaults.
	reach          float64
	stopAt         float64
	nonPathfinding bool
	// speedFactor overrides the tempt pace (the ghastling brain's
	// SPEED_MULTIPLIER_WHEN_TEMPTED of 1.25); zero means the kind's default.
	speedFactor float64
	// untamed holds the lure to a wild mob.
	// vanilla: Cat.CatTemptGoal.canUse
	untamed bool
	// canScare ends the goal when the player within 6 blocks moves or turns;
	// scareOff suspends that (a trusting ocelot). px..pRotY are the player's
	// remembered position and rotation.
	// vanilla: TemptGoal.canContinueToUse
	canScare     bool
	scareOff     func() bool
	px, py, pz   float64
	pRotX, pRotY float64
	// catSelected is Cat.CatTemptGoal.selectedPlayer for the one player.
	catSelected bool
	// cooldownTicks is a brain memory counted each tick by its owner, rather
	// than a goal delay counted on selection.
	cooldownTicks *int
	sensedPlayer  *int32
	// onStart runs when the goal starts.
	onStart func()
}

// pace is the speed the walk toward the lure is driven at.
func (g *temptGoal) pace() float64 {
	if g.m.Type == TypeAxolotl && !g.m.InWater() {
		return 0.15
	}
	if g.speedFactor > 0 {
		return g.speedFactor
	}
	if g.nonPathfinding {
		return temptFlyPace
	}
	if p := g.m.kind().temptPace; p != 0 {
		return p
	}
	return temptPace
}

func (g *temptGoal) Priority() int {
	if g.priority != 0 {
		return g.priority
	}
	return priorityTempt
}
func (g *temptGoal) Flags() GoalFlags {
	if g.m.isCube() {
		return GoalLook
	}
	return GoalMove | GoalLook
}

// Tempt numbers: the lure range (10 for animals, 16 for the happy ghast),
// the stop distance (the happy ghast's 7), and the calm-down after a lure is
// lost.
const (
	temptRange        = 10.0
	temptStopDistance = 2.5
	temptCalmDown     = 100
	temptFlyReach     = 16.0
	temptFlyStop      = 7.0
	temptFlyPace      = 1.0
)

// within is the mob's tempt_range; reach, else temptRange, stands in for a
// mob with no attribute map installed.
// vanilla: TemptGoal.canUse
func (g *temptGoal) within() float64 {
	fallback := temptRange
	if g.reach > 0 {
		fallback = g.reach
	}
	return g.m.temptRangeOr(fallback)
}

func (g *temptGoal) stopDistance() float64 {
	if g.m.Type == TypeArmadillo {
		if g.m.IsBaby() {
			return 1
		}
		return 2
	}
	if g.m.Type == TypeNautilus && g.m.IsBaby() {
		return 2.5
	}
	if g.m.Type == TypeSniffer {
		// vanilla: SnifferAi.initIdleActivity's FollowTemptation
		if g.m.IsBaby() {
			return 2.5
		}
		return 3.5
	}
	if g.stopAt > 0 {
		return g.stopAt
	}
	return temptStopDistance
}

func (g *temptGoal) player(t *tickView) *Player {
	if g.sensedPlayer != nil {
		for i := range t.players {
			if t.players[i].EntityID == *g.sensedPlayer {
				return &t.players[i]
			}
		}
		return nil
	}
	return g.m.nearestTemptingPlayerWithin(t, g.within())
}

func (g *temptGoal) CanStart(t *tickView) bool {
	if g.cooldownTicks != nil && *g.cooldownTicks > 0 {
		return false
	}
	if g.untamed && g.m.Tamable.Tame {
		return false
	}
	if g.calmDown > 0 {
		g.calmDown--
		return false
	}
	return g.player(t) != nil
}

func (g *temptGoal) Start(t *tickView) {
	if p := g.player(t); p != nil {
		g.px, g.py, g.pz = p.X, p.Y, p.Z
	}
	if g.onStart != nil {
		g.onStart()
	}
}

func (g *temptGoal) ShouldContinue(t *tickView) bool {
	p := g.player(t)
	if p != nil && g.scares() {
		if Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z) < 36 {
			if Dist2(p.X, p.Y, p.Z, g.px, g.py, g.pz) > 0.010000000000000002 {
				return false
			}
			if math.Abs(p.Pitch-g.pRotX) > 5 || math.Abs(p.Yaw-g.pRotY) > 5 {
				return false
			}
		} else {
			g.px, g.py, g.pz = p.X, p.Y, p.Z
		}
		g.pRotX, g.pRotY = p.Pitch, p.Yaw
	}
	return p != nil
}

// scares is TemptGoal.canScare with its Ocelot and Cat overrides.
// vanilla: Cat.CatTemptGoal.canScare, Ocelot.OcelotTemptGoal.canScare
func (g *temptGoal) scares() bool {
	return g.canScare && !g.catSelected && (g.scareOff == nil || !g.scareOff())
}

func (g *temptGoal) Tick(t *tickView) {
	p := g.player(t)
	if p == nil {
		return
	}
	dx, dz := p.X-g.m.X, p.Z-g.m.Z
	if g.canScare && g.m.Type == TypeCat {
		// vanilla: Cat.CatTemptGoal.tick
		if !g.catSelected && g.m.Rand.Intn(reducedTickDelay(600)) == 0 {
			g.catSelected = true
		} else if g.m.Rand.Intn(reducedTickDelay(500)) == 0 {
			g.catSelected = false
		}
	}
	g.m.HeadYaw = float32(yawToward(dx, dz))
	g.m.Pitch = float32(pitchToward(dx, p.Y+p.eyeHeight()-(g.m.Y+g.m.height()*0.85), dz))
	stop := g.stopDistance()
	// vanilla: TemptGoal.tick — inside the stop distance the walk stops.
	if Dist2(g.m.X, g.m.Y, g.m.Z, p.X, p.Y, p.Z) < stop*stop {
		g.m.stopSeeking()
		if g.m.isCube() {
			g.m.cube.wanted, g.m.cube.speedMod = true, 0
		}
		return
	}
	if g.m.isCube() {
		// Cube move control takes a heading instead of a navigation path.
		g.m.Yaw = turnToward(g.m.Yaw, float32(yawToward(dx, dz)), 10)
		g.m.cube.yRot, g.m.cube.aggressive = g.m.Yaw, true
		g.m.cube.wanted, g.m.cube.speedMod = true, 1
		return
	}
	if g.nonPathfinding {
		// vanilla: TemptGoal.ForNonPathfinders.navigateTowards — a random point
		// between the body and the player's eyes, at full speed.
		eyeX, eyeY, eyeZ := p.X, p.Y+p.eyeHeight(), p.Z
		r := g.m.Rand.Float64()
		g.m.seekToFly(g.m.X+(eyeX-g.m.X)*r, g.m.Y+(eyeY-g.m.Y)*r, g.m.Z+(eyeZ-g.m.Z)*r, g.pace())
		return
	}
	g.m.seekTo(t.w, p.X, p.Y, p.Z, g.pace(), navReach)
}

func (g *temptGoal) Stop(*tickView) {
	g.m.stopSeeking()
	if g.cooldownTicks != nil {
		*g.cooldownTicks = temptCalmDown
	} else {
		// Goal delays count selections; brain memories count full ticks.
		g.calmDown = reducedTickDelay(temptCalmDown)
	}
}

// RequiresEveryTick is false; TemptGoal does not override it.
func (g *temptGoal) RequiresEveryTick() bool { return false }

// eatBlockGoal is EatBlockGoal, the sheep's grazing: a sheep on grass, or at
// an edible_for_sheep plant, lowers its head for 40 ticks and bites 4 ticks
// before raising it. The bite regrows a shorn fleece; changing the block is
// the server's, which mob_griefing may veto, but the sheep has eaten either
// way.
// vanilla: EatBlockGoal
type eatBlockGoal struct {
	m       *Mob
	remains int
}

// Grazing numbers: the die's faces per selection (a lamb's chance is 20
// times higher) and the 40 ticks head-down, counted at the goal's
// every-other-tick cadence.
const (
	eatDieGrown = 1000
	eatDieBaby  = 50
	eatFor      = 40
	eatBiteAt   = 4
)

func (g *eatBlockGoal) Priority() int { return priorityEat }

// Flags holds MOVE, LOOK and JUMP while grazing, as vanilla registers it.
func (g *eatBlockGoal) Flags() GoalFlags { return GoalMove | GoalLook | GoalJump }

func (g *eatBlockGoal) CanStart(t *tickView) bool {
	die := reducedTickDelay(eatDieGrown)
	if g.m.IsBaby() {
		die = reducedTickDelay(eatDieBaby)
	}
	if g.m.Rand.Intn(die) != 0 {
		return false
	}
	return grazingAt(t.w, g.m.cell())
}

func (g *eatBlockGoal) ShouldContinue(*tickView) bool { return g.remains > 0 }

func (g *eatBlockGoal) Start(t *tickView) {
	g.remains = reducedTickDelay(eatFor)
	g.m.stopSeeking()
	t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobBeganEating})
}

func (g *eatBlockGoal) Tick(t *tickView) {
	g.remains = max(0, g.remains-1)
	if g.remains != reducedTickDelay(eatBiteAt) {
		return
	}
	cell := g.m.cell()
	if !edibleForSheep(t.w, cell.X, cell.Y, cell.Z) {
		// No plant at the feet: the grass block below is the bite, if it is one.
		cell = Waypoint{cell.X, cell.Y - 1, cell.Z}
		if registry.BlockOf(t.w.BlockAt(cell.X, cell.Y, cell.Z)) != registry.BlockGrassBlock {
			return
		}
	}
	g.m.ate(t.w)
	t.events = append(t.events, MobEvent{
		Mob: g.m, Kind: MobAte,
		X: float64(cell.X), Y: float64(cell.Y), Z: float64(cell.Z),
	})
}

func (g *eatBlockGoal) Stop(*tickView) {
	g.remains = 0
}

// RequiresEveryTick is false; EatBlockGoal does not override it.
func (g *eatBlockGoal) RequiresEveryTick() bool { return false }

// edibleForSheepBlocks is BlockTags.EDIBLE_FOR_SHEEP (short and dry grasses,
// ferns).
var edibleForSheepBlocks = registry.BlockTagOf("minecraft:edible_for_sheep")

// edibleForSheep reports whether the cell holds one of the plants the
// edible_for_sheep tag lists. vanilla: EatBlockGoal.IS_EDIBLE
func edibleForSheep(w World, x, y, z int32) bool {
	return registry.BlockOf(w.BlockAt(x, y, z)).InTag(edibleForSheepBlocks)
}

// grazingAt reports whether a cell holds an edible plant or has grass below
// it.
// vanilla: EatBlockGoal.canUse
func grazingAt(w World, cell Waypoint) bool {
	if edibleForSheep(w, cell.X, cell.Y, cell.Z) {
		return true
	}
	return registry.BlockOf(w.BlockAt(cell.X, cell.Y-1, cell.Z)) == registry.BlockGrassBlock
}

// ate is Sheep.ate: a shorn fleece regrows, and a lamb that can age gains
// the sixty seconds of growth a bite is worth.
// vanilla: Sheep.ate, AgeableMob.ageUp
func (m *Mob) ate(w World) {
	m.SetMetadataSheared(false)
	if m.IsBaby() && !m.AgeLocked {
		m.ageUp(w, eatAgesBy)
	}
}

// ageUp grows a baby by ticks, stopping at 0, where the growth feeding
// forced becomes the adult's breeding cooldown; the box follows the crossing.
// vanilla: AgeableMob.ageUp, unforced; AgeableMob.onSyncedDataUpdated
func (m *Mob) ageUp(w World, ticks int) {
	oldWidth, oldHeight := m.width(), m.height()
	m.SetMetadataBabyAge(min(0, m.BabyAge+ticks))
	if m.BabyAge == 0 {
		m.SetMetadataBabyAge(m.forcedAge)
		m.refreshDimensions(w, oldWidth, oldHeight)
	}
}

// eatAgesBy is AgeableMob.ageUp(60) in ticks.
const eatAgesBy = 60 * 20

// Stroll numbers: the one-in-interval start roll and the wander spreads.
// The paces are shares of the type's walk.
const (
	strollEvery = 120
	// strollIdleLimit is the noActionTime from which a mob no longer
	// strolls (checkNoActionTime's default).
	strollIdleLimit = 100
	// strollSpreadXZ and strollSpreadY are RandomStrollGoal.getPosition's
	// DefaultRandomPos.getPos(mob, 10, 7).
	strollSpreadXZ = 10
	strollSpreadY  = 7
	// strollWaterSpreadXZ is WaterAvoidingRandomStrollGoal.getPosition's
	// LandRandomPos.getPos(mob, 15, 7) for a mob in water.
	strollWaterSpreadXZ = 15
	// strollPlainChance is WaterAvoidingRandomStrollGoal.PROBABILITY.
	strollPlainChance = 0.001
	strollPace        = 1.0
	temptPace         = 1.1
	panicPace         = 1.4
)

// randomStroll is the wander of an animal with nowhere to be.
type randomStroll struct {
	m *Mob
	// priority is the goal's vanilla registration; zero means the passive
	// set's.
	priority int
	// interval replaces the roll of strollEvery where nonzero, and blocked
	// refuses the stroll while it answers true.
	interval   int
	ignoreIdle bool // Brain strolls and village golems skip the noActionTime gate.
	blocked    func() bool
	// emptyPick starts the stroll even when no destination is found; it then
	// ends at once, using up the brain RunOne's pick.
	// vanilla: RandomStroll.strollFlyOrSwim
	emptyPick bool
	none      bool // the started stroll found no destination
	// pick replaces the water-avoiding destination search where set.
	pick func(*tickView) (Waypoint, bool)
	// pace is the share of the walk the stroll runs at; zero is strollPace.
	pace   float64
	target Waypoint
}

func (g *randomStroll) Priority() int {
	if g.priority != 0 {
		return g.priority
	}
	return priorityStroll
}
func (g *randomStroll) Flags() GoalFlags { return GoalMove }

// CanStart rolls 1 in reducedTickDelay(120) per selection and picks a
// destination; no walkable spot means no start.
// vanilla: RandomStrollGoal.canUse
func (g *randomStroll) CanStart(t *tickView) bool {
	if !g.ignoreIdle && g.m.NoActionTicks >= strollIdleLimit {
		return false
	}
	if g.blocked != nil && g.blocked() {
		return false
	}
	every := strollEvery
	if g.interval != 0 {
		every = g.interval
	}
	if g.m.Rand.Intn(reducedTickDelay(every)) != 0 {
		return false
	}
	var ok bool
	if g.pick != nil {
		g.target, ok = g.pick(t)
	} else {
		g.target, ok = strollPos(t.w, g.m)
	}
	g.none = !ok
	return ok || g.emptyPick
}

func (g *randomStroll) Start(t *tickView) {
	if g.none {
		return
	}
	// Ordered once so the stuck watch's window can close; re-ordering would
	// re-arm it and a stroll that makes no way would never be given up.
	// vanilla: RandomStrollGoal.start
	pace := strollPace
	if p := g.m.kind().strollPace; p != 0 {
		pace = p
	}
	if g.pace != 0 {
		pace = g.pace
	}
	g.m.seekTo(t.w, float64(g.target.X)+0.5, float64(g.target.Y), float64(g.target.Z)+0.5, pace, navReach)
}

func (g *randomStroll) ShouldContinue(*tickView) bool {
	return len(g.m.navOf().nav.path) > 0
}

// Tick does nothing; the goal ends when the walk is done or given up.
func (g *randomStroll) Tick(*tickView) {}

func (g *randomStroll) Stop(*tickView) {
	g.m.stopSeeking()
}

// RequiresEveryTick is false; RandomStrollGoal does not override it.
func (g *randomStroll) RequiresEveryTick() bool { return false }

// lookAtPlayer is LookAtPlayerGoal: offered about every fiftieth tick, it
// holds the head square on the nearest player within eight blocks for a
// couple of seconds or until they leave.
type lookAtPlayer struct {
	m       *Mob
	remains int
	// target is the entity id of the player the glance opened on.
	target int32
	// priority is the goal's vanilla registration; zero means the passive
	// set's.
	priority int
	// reach overrides lookRange; zero stands for it.
	reach float64
	// chance replaces lookChance where non-zero (LookAtPlayerGoal's
	// probability argument).
	chance float64
	// accept filters the players considered when opening a glance.
	accept func(*Player) bool
	// brain uses SetEntityLookTarget with LookAtTargetSink's visible target
	// and 45–90 tick lifetime instead of LookAtPlayerGoal's glance.
	brain bool
}

func (g *lookAtPlayer) Priority() int {
	if g.priority != 0 {
		return g.priority
	}
	return priorityLookAt
}
func (g *lookAtPlayer) Flags() GoalFlags { return GoalLook }

// lookRange is vanilla's eight-block gaze range.
const lookRange = 8.0

func (g *lookAtPlayer) lookRange() float64 {
	if g.reach != 0 {
		return g.reach
	}
	return lookRange
}

func (g *lookAtPlayer) CanStart(t *tickView) bool {
	chance := lookChance
	if g.chance != 0 {
		chance = g.chance
	}
	if g.m.Rand.Float64() >= chance {
		return false
	}
	p := g.m.nearestPlayerMatching(t, g.lookRange(), true, g.accept)
	if p == nil {
		return false
	}
	g.target = p.EntityID
	return true
}

func (g *lookAtPlayer) Start(*tickView) {
	if g.brain {
		// vanilla: PiglinAi.initCoreActivity (LookAtTargetSink)
		g.remains = 45 + g.m.Rand.Intn(46)
		return
	}
	// vanilla: adjustedTickDelay(40 + nextInt(40)), halved because remains
	// counts only the ticks the goal is ticked.
	g.remains = reducedTickDelay(lookForTicks + g.m.Rand.Intn(lookForVaries))
}

// targetPlayer keeps a glance by raw reach and a brain look by visibility.
// vanilla: LookAtPlayerGoal.canContinueToUse, LookAtTargetSink.canStillUse
func (g *lookAtPlayer) targetPlayer(t *tickView) *Player {
	for i := range t.players {
		if p := &t.players[i]; p.EntityID == g.target {
			if g.brain {
				if !g.m.noticesPlayer(t, p, g.m.followRange(), true) {
					return nil
				}
				return p
			}
			if g.m.distanceToPlayer(p) > g.lookRange() {
				return nil
			}
			return p
		}
	}
	return nil
}

func (g *lookAtPlayer) ShouldContinue(t *tickView) bool {
	return g.remains > 0 && g.targetPlayer(t) != nil
}

func (g *lookAtPlayer) Tick(t *tickView) {
	g.remains--
	if p := g.targetPlayer(t); p != nil {
		g.m.lookAtDefault(p.X, p.Y+p.eyeHeight(), p.Z)
	}
}

func (g *lookAtPlayer) Stop(*tickView) { g.remains = 0 }

// player is the player a running glance holds, or nil.
func (g *lookAtPlayer) player(t *tickView) *Player {
	if g.remains <= 0 {
		return nil
	}
	return g.targetPlayer(t)
}

func (g *lookAtPlayer) RequiresEveryTick() bool { return g.brain }

// randomLookAround turns the head to a random nearby point now and then,
// briefly.
type randomLookAround struct {
	m          *Mob
	relX, relZ float64
	remains    int
	// priority is the goal's vanilla registration; zero means the passive
	// set's.
	priority int
}

func (g *randomLookAround) Priority() int {
	if g.priority != 0 {
		return g.priority
	}
	return priorityLookAround
}
func (g *randomLookAround) Flags() GoalFlags { return GoalMove | GoalLook }

func (g *randomLookAround) CanStart(*tickView) bool {
	return g.m.Rand.Float64() < lookChance
}

func (g *randomLookAround) Start(*tickView) {
	rnd := 2 * math.Pi * g.m.Rand.Float64()
	g.relX, g.relZ = math.Cos(rnd), math.Sin(rnd)
	g.remains = lookAroundFor + g.m.Rand.Intn(lookAroundAlike)
}

func (g *randomLookAround) ShouldContinue(*tickView) bool {
	return g.remains > 0
}

func (g *randomLookAround) Tick(*tickView) {
	g.remains--
	g.m.lookAtDefault(g.m.X+g.relX, g.m.Y+g.m.eyeHeight(), g.m.Z+g.relZ)
}

func (g *randomLookAround) Stop(*tickView) {}

// RequiresEveryTick is RandomLookAroundGoal's override: the head keeps
// easing between selections.
func (g *randomLookAround) RequiresEveryTick() bool { return true }

// brainStep updates sensor memories before the goal selector runs.
func (m *Mob) brainStep(t *tickView) {
	switch m.Type {
	case TypeTadpole:
		m.tadpoleBrainStep(t)
	case TypeAxolotl:
		m.axolotlBrainStep()
		m.axolotlSense(t)
	case TypeFrog:
		m.frogBrainStep(t)
	case TypeGoat:
		m.goatBrainStep(t)
	case TypeVillager:
		m.villagerBrainStep(t)
	}
}
