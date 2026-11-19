package entity

import (
	"math/rand"
	"slices"
	"testing"
)

// The selector's cadence as ordinary unit tests, the way the goals' own
// behaviour is: each case steps a mob against a world that is nothing but
// the blocks it sets, and counts the offers and the ticks a goal is given.
// The black-box tests in internal/server/server_creeper_test.go remain the proof of
// the spider's daylight drop over the wire.

// cadenceGoal is a goal whose say is counted: it keeps the ages the
// selector offered it a start on and the ages it was ticked on, and it
// holds nothing — no controls — so nothing of the mob's own machinery
// stands in its way.
type cadenceGoal struct {
	m        *Mob
	priority int
	every    bool
	start    bool
	offered  []int
	started  int
	ticked   []int
}

func (g *cadenceGoal) Priority() int    { return g.priority }
func (g *cadenceGoal) Flags() GoalFlags { return 0 }

func (g *cadenceGoal) CanStart(*tickView) bool {
	g.offered = append(g.offered, g.m.Age)
	return g.start
}

func (g *cadenceGoal) Start(*tickView) { g.started++ }

func (g *cadenceGoal) ShouldContinue(*tickView) bool { return true }

func (g *cadenceGoal) Tick(*tickView) { g.ticked = append(g.ticked, g.m.Age) }

func (g *cadenceGoal) Stop(*tickView) {}

func (g *cadenceGoal) RequiresEveryTick() bool { return g.every }

// controlGoal is a goal of a chosen control that records whether the
// selector stopped it, for the ridden mount's control-flag test.
type controlGoal struct {
	m        *Mob
	flags    GoalFlags
	priority int
	stopped  bool
}

func (g *controlGoal) Priority() int                 { return g.priority }
func (g *controlGoal) Flags() GoalFlags              { return g.flags }
func (g *controlGoal) CanStart(*tickView) bool       { return true }
func (g *controlGoal) Start(*tickView)               {}
func (g *controlGoal) ShouldContinue(*tickView) bool { return true }
func (g *controlGoal) Tick(*tickView)                {}
func (g *controlGoal) Stop(*tickView)                { g.stopped = true }
func (g *controlGoal) RequiresEveryTick() bool       { return false }

// TestAMountLosesItsControlledGoalsButKeepsItsTargetGoals holds Mob's
// control flags over a ridden mount: a rider takes only the MOVE, JUMP and
// LOOK controls, stopping the goals that hold them, and leaves the mount's
// target goals — which hold none of those — running.
// vanilla: Mob.updateControlFlags, GoalSelector.setControlFlag
func TestAMountLosesItsControlledGoalsButKeepsItsTargetGoals(t *testing.T) {
	m := NewMob(TypeZombie, 1, 0.5, 64, 0.5, 3)
	m.ai.goals = nil
	m.ai.running = nil
	move := &controlGoal{m: m, flags: GoalMove, priority: 1}
	target := &controlGoal{m: m, flags: GoalTarget, priority: 2}
	m.ai.wire(move, target)
	view := &tickView{}

	m.ai.run(m, view)
	if !m.ai.isRunning(move) || !m.ai.isRunning(target) {
		t.Fatal("an unridden mob did not start its move and target goals")
	}

	rider := NewMob(TypeSkeleton, 2, 0.5, 64, 0.5, 4)
	Ride(m, rider)
	m.ai.run(m, view)

	if !move.stopped || m.ai.isRunning(move) {
		t.Error("a rider did not stop the mount's move goal, want the MOVE control taken")
	}
	if !m.ai.isRunning(target) {
		t.Error("a rider stopped the mount's target goal, want the TARGET control kept")
	}
}

// TestTheSelectorOffersAndTicksOnEveryOtherTick checks the cadence the
// selector runs at: with the goals swapped for two of the test's own — one
// that never starts, one that starts the first chance it gets — the
// offers and the ticks land on the full selections alone. The mob's id is
// the even four, so the selections are its first tick of life and the even
// ages after it: the grace of the first tick, which the parity alone
// would have made a light tick, is part of what the ages list pins.
func TestTheSelectorOffersAndTicksOnEveryOtherTick(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	m := NewMob(TypeZombie, 4, 0.5, 64, 0.5, 3)
	m.ai.goals = nil
	m.ai.running = nil
	hold := &cadenceGoal{m: m, priority: 1}
	run := &cadenceGoal{m: m, priority: 2, start: true}
	m.ai.wire(hold, run)
	for range 9 {
		m.Tick(w, nil)
	}
	// The full selections of a nine-tick week: the first tick, and the
	// even ages the id's parity reads even — one, two, four, six, eight.
	wantSelections := []int{1, 2, 4, 6, 8}
	if !slices.Equal(hold.offered, wantSelections) {
		t.Errorf("the idle goal was offered starts on ages %v, want %v", hold.offered, wantSelections)
	}
	if !slices.Equal(run.offered, []int{1}) {
		t.Errorf("the running goal was offered starts on ages %v, want the one its start took", run.offered)
	}
	if !slices.Equal(run.ticked, wantSelections) {
		t.Errorf("the goal was ticked on ages %v, want the selections %v", run.ticked, wantSelections)
	}
}

// The grazing tests. Sheep.ate's half — the fleece back, the lamb grown —
// is the goal's to say; the block the bite is taken against is the
// server's, so what these check is which cell the goal names.

// grazingGoal finds the grazing goal a sheep was wired with, the one kind
// that carries it.
func grazingGoal(t *testing.T, m *Mob) *eatBlockGoal {
	t.Helper()
	for _, g := range m.ai.goals {
		if eat, ok := g.(*eatBlockGoal); ok {
			return eat
		}
	}
	t.Fatal("a sheep was wired without its grazing goal")
	return nil
}

// grazingView is the tick view the grazing tests drive the goal against.
func grazingView(w testWorld) *tickView {
	return &tickView{w: w}
}

// TestTheGrazingBiteGrowsTheWoolBackOffTheGrassBlock drives a shorn
// sheep's grazing by hand: the head goes down with the eating-start
// event, stays down the forty ticks the animation runs, and four ticks
// before the raise the bite is taken against the grass block under the
// feet — the event that names it, and the fleece back on the sheep.
func TestTheGrazingBiteGrowsTheWoolBackOffTheGrassBlock(t *testing.T) {
	w := newTestWorld()
	w.set(0, 63, 0, defaultState(t, "minecraft:grass_block"))
	m := NewMob(TypeSheep, 4, 0.5, 64, 0.5, 3)
	m.Sheared = true
	grazing := grazingGoal(t, m)
	view := grazingView(w)

	grazing.Start(view)
	if len(view.events) != 1 || view.events[0].Kind != MobBeganEating {
		t.Fatalf("the grazing began with %d events, want the eating-start event alone", len(view.events))
	}
	for range reducedTickDelay(eatFor) - reducedTickDelay(eatBiteAt) - 1 {
		grazing.Tick(view)
	}
	if !m.Sheared {
		t.Error("the fleece grew back before the bite, want the head still down")
	}
	grazing.Tick(view)
	if m.Sheared {
		t.Error("a shorn sheep grazed a grass block and stayed shorn, want the bite's fleece back")
	}
	if len(view.events) != 2 || view.events[1].Kind != MobAte {
		t.Fatalf("the bite came back as %d events, want the one MobAte", len(view.events)-1)
	}
	ate := view.events[1]
	if ate.X != 0 || ate.Y != 63 || ate.Z != 0 {
		t.Errorf("the bite named the cell (%.0f,%.0f,%.0f), want the grass block under the feet (0,63,0)",
			ate.X, ate.Y, ate.Z)
	}
	// The last four ticks of the head down, and then the goal lets go.
	grazing.Tick(view)
	grazing.Tick(view)
	if grazing.ShouldContinue(view) {
		t.Error("the grazing held on past its forty ticks, want the head back up")
	}
	grazing.Stop(view)
	if grazing.ShouldContinue(view) {
		t.Error("the stopped grazing still asked to continue, want it let go")
	}
}

// TestABiteOfGrassAgesALambBySixtySeconds checks the lamb's half of
// Sheep.ate: a baby that may still age — an age-locked one stays a lamb —
// leaps the sixty seconds of growing a bite is worth.
func TestABiteOfGrassAgesALambBySixtySeconds(t *testing.T) {
	w := newTestWorld()
	w.set(0, 63, 0, defaultState(t, "minecraft:grass_block"))
	m := NewMob(TypeSheep, 4, 0.5, 64, 0.5, 3)
	m.SetBaby(true)
	m.Sheared = true
	grazing := grazingGoal(t, m)
	view := grazingView(w)

	grazing.Start(view)
	for range reducedTickDelay(eatFor) - reducedTickDelay(eatBiteAt) {
		grazing.Tick(view)
	}
	if m.BabyAge != BabyAgeStart+eatAgesBy {
		t.Errorf("a lamb that ate stands at age %d, want the bite's %d above start %d", m.BabyAge, eatAgesBy, BabyAgeStart)
	}
	if !m.IsBaby() {
		t.Error("a lamb grew out of its baby-ness on one bite")
	}

	m.SetBaby(true)
	m.AgeLocked = true
	m.Sheared = true
	m.ate(w)
	if m.BabyAge != BabyAgeStart {
		t.Errorf("an age-locked lamb ate and aged to %d, want its age held at start", m.BabyAge)
	}
}

// TestAStrollThatCannotBeWalkedIsGivenUp drives the stroll itself, not a
// path handed to a body: the goal orders its walk on the selection that
// starts it and then leaves it alone, so a walk the body never makes a step
// of is dropped when the stuck watch's hundred-tick window closes. A stroll
// that re-ordered its walk every selection — vanilla's RandomStrollGoal
// asks only in start — would re-arm the sampling before the window ever
// closed and the body would press at its walls forever.
func TestAStrollThatCannotBeWalkedIsGivenUp(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -40, -40, 40, 40, 63, stone)

	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	cow.ai.goals = nil
	cow.ai.running = nil
	cow.ai.wire(&randomStroll{m: cow})
	cow.OnGround = true
	view := &tickView{w: w}

	// The stroll orders its walk; the body is then held where it stands, so
	// the walk can never be taken a step of its way.
	for range 2000 {
		cow.ai.run(cow, view)
		cow.navigate(w)
		if len(cow.nav.path) > 0 {
			break
		}
	}
	if len(cow.nav.path) == 0 {
		t.Fatal("the stroll ordered no walk, want one in hand")
	}

	dropped := false
	for range 110 {
		cow.ai.run(cow, view)
		cow.navigate(w)
		if len(cow.nav.path) == 0 {
			dropped = true
			break
		}
	}
	if !dropped {
		t.Error("a stroll's walk that made no way survived its window, want it given up")
	}
}

// TestEachKindWalksAtItsOwnGoalSpeeds pins the speed modifiers the kinds'
// panic, tempt and stroll goals are built with.
// vanilla: AbstractCow, Pig, Sheep, Chicken, Creeper, Spider registerGoals,
// AbstractHorse.addBehaviourGoals
func TestEachKindWalksAtItsOwnGoalSpeeds(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -30, -30, 30, 30, 63, stone)
	view := &tickView{w: w}
	cases := []struct {
		typ                  MobType
		panic, tempt, stroll float64
	}{
		{TypeCow, 2.0, 1.25, 1.0},
		{TypeMooshroom, 2.0, 1.25, 1.0},
		{TypePig, 1.25, 1.2, 1.0},
		{TypeSheep, 1.25, 1.1, 1.0},
		{TypeChicken, 1.4, 1.0, 1.0},
		{TypeHorse, 1.2, 0, 1.0},
		{TypeCreeper, 0, 0, 0.8},
		{TypeSpider, 0, 0, 0.8},
		{TypeCaveSpider, 0, 0, 0.8},
		{TypeZombie, 0, 0, 1.0},
		{TypeSkeleton, 0, 0, 1.0},
	}
	for _, c := range cases {
		m := NewMob(c.typ, 1, 0.5, 64, 0.5, 1)
		speed := func() float64 { return m.navOf().nav.speedFactor }
		stroll := &randomStroll{m: m, target: Waypoint{X: 5, Y: 64, Z: 5}}
		stroll.Start(view)
		if got := speed(); got != c.stroll {
			t.Errorf("%v strolls at %v, want %v", c.typ, got, c.stroll)
		}
		if c.panic != 0 {
			m.stopSeeking()
			p := &panicGoal{m: m, fleeX: 5.5, fleeY: 64, fleeZ: 5.5}
			p.Start(view)
			if got := speed(); got != c.panic {
				t.Errorf("%v panics at %v, want %v", c.typ, got, c.panic)
			}
			if got := (&temptGoal{m: m}).pace(); c.tempt != 0 && got != c.tempt {
				t.Errorf("%v is tempted at %v, want %v", c.typ, got, c.tempt)
			}
		}
	}
}

// TestAStrollPrefersGrass checks the best-of-ten pick: on a floor that is
// half grass, half stone, a grazer's strolls end on grass more often than
// the half a blind pick would give.
func TestAStrollPrefersGrass(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	grass := defaultState(t, "minecraft:grass_block")
	w := newTestWorld()
	groundTo(w, -40, -40, 40, 40, 63, stone)
	for x := int32(-40); x <= 40; x++ {
		for z := int32(0); z <= 40; z++ {
			w.set(x, 63, z, grass)
		}
	}
	view := &tickView{w: w}
	onGrass, total := 0, 0
	for seed := int64(1); seed <= 1500; seed++ {
		cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, seed)
		cow.Rand = rand.New(rand.NewSource(seed))
		g := &randomStroll{m: cow, interval: 1}
		if !g.CanStart(view) {
			continue
		}
		total++
		if g.target.Z >= 0 {
			onGrass++
		}
	}
	if total == 0 || float64(onGrass)/float64(total) < 0.58 {
		t.Errorf("%d of %d strolls ended on grass, want clearly more than the blind half", onGrass, total)
	}
}
