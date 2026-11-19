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

// TestTheEveryTickGoalIsTickedBetweenSelections checks the other half of
// the cadence: a goal that requires the every-tick update is ticked on the
// ticks between the selections too, all nine of them.
func TestTheEveryTickGoalIsTickedBetweenSelections(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	m := NewMob(TypeZombie, 4, 0.5, 64, 0.5, 3)
	m.ai.goals = nil
	m.ai.running = nil
	run := &cadenceGoal{m: m, priority: 1, every: true, start: true}
	m.ai.wire(run)
	for range 9 {
		m.Tick(w, nil)
	}
	wantEvery := []int{1, 2, 3, 4, 5, 6, 7, 8, 9}
	if !slices.Equal(run.ticked, wantEvery) {
		t.Errorf("the every-tick goal was ticked on ages %v, want every tick %v", run.ticked, wantEvery)
	}
}

// TestAProvokedSpiderInTheSunDropsItsTargetOnTheThirdCheck checks the
// daylight drop at the cadence vanilla runs it: a spider struck by a
// player hunts them back whatever the light — the retaliation has no gate
// of its own — and Spider$SpiderAttackGoal's canContinueToUse, asked only
// on the ticks the selector offers and cleans up, drops the hunt on one
// tick in a hundred of the bright hour. With the id the even four the
// checks land on the second, fourth and sixth ticks of the hunt, and the
// seed is one whose third roll of the hundred-sided die comes up nought:
// the spider keeps its target through the fifth tick and drops it on the
// sixth.
func TestAProvokedSpiderInTheSunDropsItsTargetOnTheThirdCheck(t *testing.T) {
	w := newTestWorld()
	w.sky = 15
	w.darken = 0
	floor64(w, t)
	m := NewMob(TypeSpider, 4, 0.5, 64, 0.5, 21)
	players := []Player{survivor(7, 5.5)}
	if !m.TakeDamage(1, players[0]) {
		t.Fatalf("the spider refused the hurt that was to provoke it")
	}
	// The blow's knockback would lift the spider off the floor; the hunt's
	// opening search needs a body that may plan, so it is set back down.
	m.OnGround, m.VY = true, 0
	for tick := 1; tick <= 5; tick++ {
		m.Tick(w, players)
		if m.TargetID != 7 {
			t.Fatalf("the provoked spider's target was %d after tick %d, want the striker 7 it hunts in the sun", m.TargetID, tick)
		}
	}
	m.Tick(w, players)
	if m.TargetID != 0 {
		t.Errorf("the provoked spider still hunts %d after tick 6, want the hunt dropped on its third check", m.TargetID)
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

// TestTheGrazingBiteEatsThePlantAtTheFeet is the same bite over a plant
// of the edible_for_sheep tag standing in the feet cell — named against
// the feet cell itself — and the offer the cell must make first: a plant
// at the feet or a grass block under them, and nothing else.
func TestTheGrazingBiteEatsThePlantAtTheFeet(t *testing.T) {
	w := newTestWorld()
	w.set(0, 63, 0, defaultState(t, "minecraft:dirt"))
	w.set(0, 64, 0, defaultState(t, "minecraft:fern"))
	m := NewMob(TypeSheep, 4, 0.5, 64, 0.5, 3)
	m.Sheared = true
	grazing := grazingGoal(t, m)
	view := grazingView(w)

	grazing.Start(view)
	for range reducedTickDelay(eatFor) - reducedTickDelay(eatBiteAt) {
		grazing.Tick(view)
	}
	if m.Sheared {
		t.Error("a shorn sheep grazed a fern and stayed shorn, want the bite's fleece back")
	}
	if len(view.events) != 2 {
		t.Fatalf("the grazing over a fern came back as %d events, want the start and the bite", len(view.events))
	}
	ate := view.events[1]
	if ate.Kind != MobAte || ate.X != 0 || ate.Y != 64 || ate.Z != 0 {
		t.Errorf("the bite = {%d at (%.0f,%.0f,%.0f)}, want the fern at the feet (0,64,0)", ate.Kind, ate.X, ate.Y, ate.Z)
	}
}

// TestGrazingIsOfferedOnlyWhereThereIsGrassToEat checks the offer alone:
// a plant in the feet cell or a grass block under them is a bite waiting,
// and bare dirt or an empty cell is none of the grazing's business.
func TestGrazingIsOfferedOnlyWhereThereIsGrassToEat(t *testing.T) {
	w := newTestWorld()
	if grazingAt(w, Waypoint{0, 64, 0}) {
		t.Error("the grazing offered a bite in an empty cell, want nothing to eat")
	}
	w.set(0, 63, 0, defaultState(t, "minecraft:dirt"))
	if grazingAt(w, Waypoint{0, 64, 0}) {
		t.Error("the grazing offered a bite over bare dirt, want nothing to eat")
	}
	w.set(0, 64, 0, defaultState(t, "minecraft:fern"))
	if !grazingAt(w, Waypoint{0, 64, 0}) {
		t.Error("the grazing offered no bite over a fern, want one waiting")
	}

	flat := newTestWorld()
	flat.set(0, 63, 0, defaultState(t, "minecraft:grass_block"))
	if !grazingAt(flat, Waypoint{0, 64, 0}) {
		t.Error("the grazing offered no bite over a grass block, want one waiting")
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

// TestALambGrownByGrassAfterFeedingKeepsTheFedTimeAsCooldown: the wheat's
// growth adds to ForcedAge, and the bite that makes the lamb grown sets its
// age to that sum, a breeding cooldown in place of the fertile 0.
// vanilla: AgeableMob.ageUp
func TestALambGrownByGrassAfterFeedingKeepsTheFedTimeAsCooldown(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeSheep, 4, 0.5, 64, 0.5, 3)
	m.SetBaby(true)
	// Two wheat: a tenth of 1200 seconds, then of 1080, 2400+2160 ticks.
	m.Feed()
	m.Feed()
	if m.BabyAge != -19440 || m.forcedAge != 4560 {
		t.Fatalf("the twice-fed lamb has age %d and forced age %d, want -19440 and 4560", m.BabyAge, m.forcedAge)
	}
	m.BabyAge = -eatAgesBy + 1
	m.ate(w)
	if m.IsBaby() || m.BabyAge != 4560 {
		t.Errorf("the grazed lamb has age %d, want grown with the fed 4560 ticks as cooldown", m.BabyAge)
	}
	if loved, _ := m.Feed(); loved {
		t.Error("the grown sheep fell in love during its cooldown")
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

// TestAShornSheepGrazesOnItsOwn runs the whole selection under a sheep of
// a seed that takes its first bite within a few hundred ticks: the
// grazing comes up on the selector's own offer — one in five hundred a
// selection holds — and the bite turns the shorn flag off all by itself.
func TestAShornSheepGrazesOnItsOwn(t *testing.T) {
	w := newTestWorld()
	grass := defaultState(t, "minecraft:grass_block")
	for x := int32(-8); x <= 8; x++ {
		for z := int32(-8); z <= 8; z++ {
			w.set(x, 63, z, grass)
		}
	}
	m := NewMob(TypeSheep, 4, 0.5, 64, 0.5, 979)
	m.Sheared = true

	var ate bool
	for tick := 0; tick < 3000 && !ate; tick++ {
		for _, ev := range m.Tick(w, nil) {
			if ev.Kind == MobAte {
				ate = true
			}
		}
	}
	if !ate {
		t.Fatal("a shorn sheep on a grass field never took its bite within 3000 ticks")
	}
	if m.Sheared {
		t.Error("the sheep's own grazing did not grow the wool back, want the bite taken")
	}
}

// TestStrollStartsOnAOneInSixtyRoll pins the stroll's selection roll: over
// many selections the gap between starts averages reducedTickDelay(120)
// selections, and a mob idle for 100 ticks never starts one.
func TestStrollStartsOnAOneInSixtyRoll(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	groundTo(w, -40, -40, 40, 40, 63, stone)
	view := &tickView{w: w}
	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	cow.Rand = rand.New(rand.NewSource(7))
	g := &randomStroll{m: cow}
	const selections = 60000
	starts := 0
	for range selections {
		if g.CanStart(view) {
			starts++
		}
	}
	gap := float64(selections) / float64(starts)
	if gap < 54 || gap > 66 {
		t.Errorf("mean gap between strolls = %.1f selections, want about 60", gap)
	}

	cow.NoActionTicks = 100
	for range selections {
		if g.CanStart(view) {
			t.Fatal("a mob idle for 100 ticks started a stroll")
		}
	}
	cow.NoActionTicks = 99
	started := false
	for range selections {
		started = started || g.CanStart(view)
	}
	if !started {
		t.Error("a mob idle for 99 ticks never strolled")
	}
}

// TestAnAnimalAloneCountsIdleTicksAndStopsStrolling lets the counter run
// instead of setting it: a cow no player is near counts a tick of
// idleness per tick like every mob (Mob.serverAiStep), so once it passes
// the stroll's limit it stands, though the despawn never takes it; a player
// within the no-despawn distance winds it back to zero.
// vanilla: Mob.serverAiStep, Mob.checkDespawn
func TestAnAnimalAloneCountsIdleTicksAndStopsStrolling(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -40, -40, 40, 40, 63, stone)
	cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	cow.NoAI = false
	far := []Player{{X: 500, Y: 64, Z: 500}}
	for range 300 {
		cow.Tick(w, far)
	}
	if cow.NoActionTicks < strollIdleLimit {
		t.Fatalf("a cow with no player near counted %d idle ticks in 300 ticks, want past %d", cow.NoActionTicks, strollIdleLimit)
	}
	if cow.gone {
		t.Error("the idle cow was despawned, want animals to stay")
	}
	x, z := cow.X, cow.Z
	for range 200 {
		cow.Tick(w, far)
	}
	if cow.X != x || cow.Z != z {
		t.Errorf("an idle cow strolled from (%.1f, %.1f) to (%.1f, %.1f), want it standing", x, z, cow.X, cow.Z)
	}

	cow.Tick(w, []Player{{X: cow.X, Y: 64, Z: cow.Z}})
	if cow.NoActionTicks != 0 {
		t.Errorf("a cow with a player beside it counts %d idle ticks, want it wound back", cow.NoActionTicks)
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

// TestAStrollPicksATargetAboveOrBelowTheMob checks the stroll looks ±7 blocks
// up and down: on a floor stepped down by seven blocks across x, some roll
// must pick a cell at a different height than the mob stands at.
func TestAStrollPicksATargetAboveOrBelowTheMob(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	groundTo(w, -40, -40, 0, 40, 63, stone)
	groundTo(w, 1, -40, 40, 40, 56, stone)
	view := &tickView{w: w}
	differ := false
	for seed := int64(1); seed <= 300 && !differ; seed++ {
		cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, seed)
		cow.Rand = rand.New(rand.NewSource(seed))
		g := &randomStroll{m: cow, interval: 1}
		for range 20 {
			if g.CanStart(view) && g.target.Y != 64 {
				differ = true
				break
			}
		}
	}
	if !differ {
		t.Error("every stroll target was at the mob's own height, want the +-7 vertical spread")
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

// TestAStrollNeedsAFullCubeUnderTheTarget checks isStableDestination: over a
// glass floor, which is not a solid-render block, no stroll target exists.
func TestAStrollNeedsAFullCubeUnderTheTarget(t *testing.T) {
	glass := defaultState(t, "minecraft:glass")
	w := newTestWorld()
	groundTo(w, -40, -40, 40, 40, 63, glass)
	for seed := int64(1); seed <= 200; seed++ {
		cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, seed)
		if cell, ok := strollPos(w, cow); ok {
			t.Fatalf("seed %d strolled to %v over glass", seed, cell)
		}
	}
}

// TestAStrollRollsTheLandPickBeforeItsDraws pins WaterAvoidingRandomStrollGoal's
// order: one nextFloat for the 0.1% roll, then LandRandomPos(10, 7).
func TestAStrollRollsTheLandPickBeforeItsDraws(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	groundTo(w, -40, -40, 40, 40, 63, stone)
	for seed := int64(1); seed <= 50; seed++ {
		a := NewMob(TypeCow, 1, 0.5, 64, 0.5, seed)
		a.Rand = rand.New(rand.NewSource(seed))
		b := NewMob(TypeCow, 1, 0.5, 64, 0.5, seed)
		b.Rand = rand.New(rand.NewSource(seed))
		got, gok := strollPos(w, a)
		b.Rand.Float32()
		want, wok := landRandomPos(w, b, strollSpreadXZ, strollSpreadY)
		if got != want || gok != wok {
			t.Fatalf("seed %d: stroll %v,%v, land pick after one roll %v,%v", seed, got, gok, want, wok)
		}
	}
}

// TestAStrollInWaterReachesFifteenBlocks checks the in-water pick spreads
// over 15 where the dry one stays within 10.
func TestAStrollInWaterReachesFifteenBlocks(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	groundTo(w, -40, -40, 40, 40, 63, stone)
	far := func(inWater bool) bool {
		for seed := int64(1); seed <= 300; seed++ {
			cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, seed)
			if inWater {
				cow.waterFluid.height = 1
			}
			if cell, ok := strollPos(w, cow); ok && (cell.X > 11 || cell.X < -11 || cell.Z > 11 || cell.Z < -11) {
				return true
			}
		}
		return false
	}
	if !far(true) {
		t.Error("a mob in water never strolled past 11 blocks, want the 15-block spread")
	}
	if far(false) {
		t.Error("a dry mob strolled past 11 blocks, want the 10-block spread")
	}
}

// TestALandPickMovesUpOutOfSolidBlocks checks a target inside a deep stone
// mass is lifted to the first open cell above it, so the pick ends on top of
// the mass instead of finding no standing place.
// vanilla: RandomPos.moveUpOutOfSolid
func TestALandPickMovesUpOutOfSolidBlocks(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	for y := int32(56); y <= 75; y++ {
		floor(w, -40, -40, 40, 40, y, stone)
	}
	for _, pick := range []func(World, *Mob, int, int) (Waypoint, bool){
		landRandomPos,
		func(w World, m *Mob, xz, y int) (Waypoint, bool) { return landRandomPosAway(w, m, 3, 0, xz, y) },
	} {
		picked := false
		for seed := int64(1); seed <= 50; seed++ {
			cow := NewMob(TypeCow, 1, 0.5, 64, 0.5, seed)
			cell, ok := pick(w, cow, strollSpreadXZ, strollSpreadY)
			if !ok {
				continue
			}
			picked = true
			if cell.Y != 76 {
				t.Fatalf("seed %d picked %v, want the top of the stone at y 76", seed, cell)
			}
		}
		if !picked {
			t.Error("no seed picked a target above the stone")
		}
	}
}

// TestALandPickStopsAtTheBuildHeight checks the climb out of solid blocks
// ends at the dimension's top: a mass of stone reaching past it leaves no
// target.
// vanilla: Level.isOutsideBuildHeight
func TestALandPickStopsAtTheBuildHeight(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	for y := int32(300); y <= 400; y++ {
		floor(w, -40, -40, 40, 40, y, stone)
	}
	for _, pick := range []func(World, *Mob, int, int) (Waypoint, bool){
		landRandomPos,
		func(w World, m *Mob, xz, y int) (Waypoint, bool) { return landRandomPosAway(w, m, 3, 0, xz, y) },
	} {
		for seed := int64(1); seed <= 50; seed++ {
			cow := NewMob(TypeCow, 1, 0.5, 319, 0.5, seed)
			if cell, ok := pick(w, cow, strollSpreadXZ, strollSpreadY); ok {
				t.Fatalf("seed %d picked %v above the build height", seed, cell)
			}
		}
	}
}
