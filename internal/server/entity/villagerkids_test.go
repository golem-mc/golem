package entity

import (
	"slices"
	"testing"
)

func playingChild(id int32, x float64) *Mob {
	m := NewMob(TypeVillager, id, x, 64, 0.5, int64(id))
	m.BabyAge = -24000
	m.vb.activity = actPlay
	return m
}

func TestVillagerChildSensorFiltersSortsAndSamples(t *testing.T) {
	w := flatWorld(t)
	m := playingChild(1, 0.5)
	near, far := playingChild(2, 2.5), playingChild(3, 6.5)
	adult, cow := NewMob(TypeVillager, 4, 1.5, 64, 0.5, 4), NewMob(TypeCow, 5, 1.5, 64, 0.5, 5)
	cow.BabyAge = -100
	dead, gone, out := playingChild(6, 1.5), playingChild(7, 1.5), playingChild(8, 20.5)
	dead.Health, gone.gone = 0, true
	tick := &tickView{w: w, others: []*Mob{far, adult, cow, dead, gone, out, m, near}}
	m.villagerKidsStep(tick)
	if got := m.vb.kids.visible; !slices.Equal(got, []*Mob{near, far}) {
		t.Fatalf("visible %v, want nearest two children", got)
	}
	tick.others = nil
	for range 19 {
		m.villagerKidsStep(tick)
	}
	if len(m.vb.kids.visible) != 2 {
		t.Fatal("sensor refreshed before twenty ticks")
	}
	m.villagerKidsStep(tick)
	if len(m.vb.kids.visible) != 0 {
		t.Fatal("sensor retained unloaded children")
	}
	tick.others = []*Mob{near}
	m.vb.kids.scan = 0
	for y := int32(64); y < 67; y++ {
		w.set(1, y, 0, defaultState(t, "minecraft:stone"))
	}
	m.villagerKidsStep(tick)
	if len(m.vb.kids.visible) != 0 {
		t.Fatal("sensor saw through a wall")
	}
	for y := int32(64); y < 67; y++ {
		w.set(1, y, 0, 0)
	}
	m.vb.kids.scan = 0
	m.villagerKidsStep(tick)
	m.vb.kids.interaction = near
	near.gone = true
	m.villagerKidsStep(tick)
	if m.vb.kids.interaction != nil {
		t.Fatal("interaction retained removed child")
	}
	m.BabyAge = 0
	m.villagerKidsStep(tick)
	if m.vb.kids.visible != nil {
		t.Fatal("grown villager retained child sensor")
	}
}

func TestTagJoinsTheLeastCrowdedChaseOrFleesAChaser(t *testing.T) {
	m, a, b, c := playingChild(1, 0.5), playingChild(2, 2.5), playingChild(3, 3.5), playingChild(4, 4.5)
	m.vb.kids.visible = []*Mob{a, b, c}
	if target, flee := m.tagTarget(); target != a || flee {
		t.Fatal("did not choose nearest child")
	}
	a.vb.kids.interaction, b.vb.kids.interaction = c, c
	c.vb.kids.interaction = a
	if target, flee := m.tagTarget(); target != a || flee {
		t.Fatal("did not join chase with one rather than two chasers")
	}
	c.vb.kids.interaction = m
	if target, flee := m.tagTarget(); target != nil || !flee {
		t.Fatal("did not flee a child chasing me")
	}
	// A chase with more than five chasers is ineligible; all five are allowed.
	crowd := []*Mob{a, b, c, playingChild(5, 5.5), playingChild(6, 6.5), playingChild(7, 7.5)}
	for _, o := range crowd {
		o.vb.kids.interaction = b
	}
	m.vb.kids.visible = crowd[:5]
	if target, _ := m.tagTarget(); target != b {
		t.Fatal("did not join a five-chaser chase")
	}
	m.vb.kids.visible = crowd
	if target, _ := m.tagTarget(); target != a {
		t.Fatal("joined an overcrowded chase instead of the nearest child")
	}
}

func startChildPlay(t *testing.T, g *kidPlayGoal, tick *tickView) {
	t.Helper()
	for range 200 {
		if g.CanStart(tick) {
			g.Start(tick)
			return
		}
	}
	t.Fatal("child did not start playing")
}

func TestChildTagTracksItsTargetAndStopsAtActivityChanges(t *testing.T) {
	w := flatWorld(t)
	m, o := playingChild(1, 0.5), playingChild(2, 6.5)
	m.vb.kids.visible = []*Mob{o}
	tick := &tickView{w: w, others: []*Mob{o}}
	g := &kidPlayGoal{m: m}
	startChildPlay(t, g, tick)
	if m.vb.kids.interaction != o || m.nav.speedFactor != 0.6 || g.reach != 1 || !g.ShouldContinue(tick) {
		t.Fatal("tag did not establish the chase walk")
	}
	end := m.nav.end
	o.X++
	g.Tick(tick)
	if m.nav.end != end {
		t.Fatal("replanned before target moved over two blocks")
	}
	o.X += 2
	g.Tick(tick)
	if m.nav.end == end {
		t.Fatal("did not replan for moving target")
	}
	m.X, m.Z = o.X, o.Z
	if g.ShouldContinue(tick) {
		t.Fatal("tag continued after arrival")
	}
	m.X = 0.5
	for _, act := range []villagerActivity{actIdle, actRest, actPanic} {
		m.vb.activity = act
		if g.ShouldContinue(tick) {
			t.Fatal("tag continued outside PLAY")
		}
		g.Tick(tick)
		if len(m.nav.path) != 0 {
			t.Fatal("intervening tick kept old play walk")
		}
	}
	m.vb.activity = actPlay
	startChildPlay(t, g, tick)
	tick.others = nil
	g.Tick(tick)
	if g.left != 0 || len(m.nav.path) != 0 {
		t.Fatal("chased unloaded target")
	}
	g.Stop(tick)
	if g.target != nil || g.look != (villagerLookTarget{}) || g.jumping != nil {
		t.Fatal("stop retained target")
	}
	m.BabyAge = 0
	if g.CanStart(tick) {
		t.Fatal("adult started playing")
	}
}

func TestATaggedChildFleesOnlyInsideAVillage(t *testing.T) {
	w := newTestWorld()
	floor(w, -30, -30, 30, 30, 63, defaultState(t, "minecraft:stone"))
	w.set(0, 64, 2, bedHead(t))
	m, o := playingChild(1, 0.5), playingChild(2, 4.5)
	m.Village = fakeVillage{w: w}
	m.vb.kids.visible = []*Mob{o}
	o.vb.kids.interaction = m
	tick := &tickView{w: w, others: []*Mob{o}}
	g := &kidPlayGoal{m: m}
	for range 60 {
		if g.CanStart(tick) {
			t.Fatal("fled without an occupied village")
		}
	}
	o.vb.home = &GlobalPos{m.Village.Dimension(), 0, 64, 2}
	startChildPlay(t, g, tick)
	if g.target != nil || g.reach != 0 || g.pace != 0.6 {
		t.Fatal("flee became a chase")
	}
	if villageDistance(sectionOf(g.destination), m.villageCenters(tick)) > 1 {
		t.Fatal("fled outside village")
	}
	if g.left < 80 || g.left > 120 {
		t.Fatalf("walk timeout %d", g.left)
	}
	g.left = 0
	if g.ShouldContinue(tick) {
		t.Fatal("timed-out walk continued")
	}
}

func TestSolitaryChildPlayUsesEveryFallbackAndNeverFallsBackWithFriends(t *testing.T) {
	w := flatWorld(t)
	w.set(0, 64, 2, bedHead(t))
	adult, cat := NewMob(TypeVillager, 50, 6.5, 64, 0.5, 1), NewMob(TypeCat, 51, 4.5, 64, 0.5, 2)
	tick := &tickView{w: w, others: []*Mob{adult, cat}}
	seen := map[string]bool{}
	for id := int32(1); id <= 120; id++ {
		m := playingChild(id, 0.5)
		m.Village = fakeVillage{w: w}
		bed := Waypoint{0, 64, 2}
		m.vb.nearestBed = &bed
		m.vb.look = villagerLookTarget{mob: cat}
		g := &kidPlayGoal{m: m}
		if !g.CanStart(tick) {
			t.Fatal("solitary child had no play choice")
		}
		g.Start(tick)
		wantInteraction := g.target
		if g.look != (villagerLookTarget{}) {
			wantInteraction = nil
		}
		if m.vb.kids.interaction != wantInteraction {
			t.Fatal("fallback established the wrong interaction memory")
		}
		switch {
		case g.jumping != nil:
			seen["bed"] = true
		case g.waiting:
			seen["pause"] = true
			if g.left < 20 || g.left > 40 || !g.ShouldContinue(tick) {
				t.Fatal("invalid play pause")
			}
		case g.look != (villagerLookTarget{}):
			seen["look"] = true
		case g.target == adult:
			seen["villager"] = true
		case g.target == cat:
			seen["cat"] = true
		default:
			seen["stroll"] = true
		}
		g.Stop(tick)
	}
	for _, choice := range []string{"bed", "pause", "villager", "cat", "stroll", "look"} {
		if !seen[choice] {
			t.Errorf("did not select %s", choice)
		}
	}
	m, child := playingChild(1, 0.5), playingChild(2, 5.5)
	m.vb.kids.visible = []*Mob{child}
	tick.others = append(tick.others, child)
	g := &kidPlayGoal{m: m}
	starts := 0
	for range 100 {
		if g.CanStart(tick) {
			starts++
			if g.target != child || g.waiting || g.jumping != nil {
				t.Fatal("used fallback with visible children")
			}
		}
	}
	if starts == 0 || starts == 100 {
		t.Fatalf("tag's one-in-ten gate started %d/100", starts)
	}
}

func TestAChildsPlayGoalRunsOnTheHerdAndYieldsToNight(t *testing.T) {
	w := flatWorld(t)
	m, o := playingChild(1, 0.5), playingChild(2, 6.5)
	m.Village, o.Village = fakeVillage{w: w}, fakeVillage{w: w}
	o.NoAI = true
	herd := Mobs{m, o}
	for n := int64(1); n <= 200; n++ {
		m.GameTime, m.DayTime = n, 3000
		herd.Tick(w, nil, nil)
		if m.X > 4.5 {
			break
		}
	}
	if m.X <= 4.5 || m.vb.activity != actPlay {
		t.Fatalf("child did not chase in PLAY: x=%v act=%v", m.X, m.vb.activity)
	}
	m.GameTime, m.DayTime = 300, 12500
	herd.Tick(w, nil, nil)
	if m.vb.activity != actRest {
		t.Fatal("night did not replace PLAY")
	}
	for _, running := range m.ai.running {
		if _, ok := running.(*kidPlayGoal); ok && running.ShouldContinue(&tickView{w: w, others: []*Mob{o}}) {
			t.Fatal("play survived night")
		}
	}
	if o.vb.kids.visible != nil {
		t.Fatal("NoAI child sensed friends")
	}
}

func TestChildLookWalkDoesNotCreateAChase(t *testing.T) {
	w := flatWorld(t)
	cow := NewMob(TypeCow, 50, 4.5, 64, 0.5, 50)
	tick := &tickView{w: w, others: []*Mob{cow}}
	for _, previous := range []*Mob{nil, playingChild(51, 6.5)} {
		found := false
		for id := int32(1); id <= 120; id++ {
			m := playingChild(id, 0.5)
			m.vb.kids.interaction = previous
			m.vb.look = villagerLookTarget{mob: cow}
			g := &kidPlayGoal{m: m}
			if !g.CanStart(tick) || g.look.mob != cow {
				continue
			}
			g.Start(tick)
			if m.vb.kids.interaction != previous {
				t.Fatal("look walk changed interaction memory")
			}
			if previous == nil {
				observer := playingChild(52, 2.5)
				observer.vb.kids.visible = []*Mob{m}
				if target, _ := observer.tagTarget(); target != m {
					t.Fatal("another child joined a chase of the look target")
				}
			}
			found = true
			break
		}
		if !found {
			t.Fatal("child never walked toward its look target")
		}
	}
}

func TestNearestSeenFiltersBeforeRaycasting(t *testing.T) {
	m := playingChild(1, 0.5)
	far := NewMob(TypeVillager, 2, 9.5, 64, 0.5, 2)
	cat := NewMob(TypeCat, 3, 2.5, 64, 0.5, 3)
	accept := func(o *Mob) bool { return o.Type == TypeVillager }
	// A nil world makes any raycast fail: neither candidate needs one.
	if got := m.nearestSeen(&tickView{others: []*Mob{m, far, cat}}, 64, accept); got != nil {
		t.Fatal("accepted an out-of-range or filtered target")
	}
	w := flatWorld(t)
	near := NewMob(TypeVillager, 4, 4.5, 64, 0.5, 4)
	edge := NewMob(TypeVillager, 5, 8.5, 64, 0.5, 5)
	dead, gone := playingChild(6, 1.5), playingChild(7, 1.5)
	dead.Health, gone.gone = 0, true
	tick := &tickView{w: w, others: []*Mob{edge, near, cat, dead, gone}}
	if got := m.nearestSeen(tick, 64, accept); got != near {
		t.Fatal("did not choose the nearest living villager")
	}
	if got := m.nearestSeen(tick, 64, nil); got != cat {
		t.Fatal("unfiltered look did not choose the nearest living mob")
	}
	tick.others = []*Mob{edge}
	if got := m.nearestSeen(tick, 64, accept); got != edge {
		t.Fatal("excluded a target at the range boundary")
	}
	for y := int32(64); y < 67; y++ {
		w.set(1, y, 0, defaultState(t, "minecraft:stone"))
	}
	if got := m.nearestSeen(tick, 64, accept); got != nil {
		t.Fatal("selected a target through a wall")
	}
}
