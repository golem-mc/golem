package entity

import "testing"

func TestVillageSectionDistanceAndClosestSection(t *testing.T) {
	if got := sectionOf(Waypoint{-1, -17, -32}); got != (Waypoint{-1, -2, -2}) {
		t.Fatalf("negative sections %v", got)
	}
	centers := []Waypoint{{0, 0, 0}}
	if d := villageDistance(Waypoint{20, 0, 0}, centers); d != 7 {
		t.Fatalf("distance %d, want capped at seven", d)
	}
	if s, ok := closerVillageSection(Waypoint{3, 4, 5}, centers); !ok || s != (Waypoint{1, 2, 3}) {
		t.Fatalf("closest section %v, %v; want first tied minimum 1 2 3", s, ok)
	}
	for _, c := range [][]Waypoint{nil, centers} {
		if _, ok := closerVillageSection(Waypoint{1, 1, 1}, c); ok {
			t.Fatal("biased a stroll inside a village or without a village")
		}
	}
}

func TestVillageCentersComeFromValidLoadedOwners(t *testing.T) {
	w := flatWorld(t)
	w.set(32, 64, 0, bedHead(t))
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	m.Village = fakeVillage{w: w}
	o := NewMob(TypeVillager, 2, 32.5, 64, 0.5, 2)
	o.vb.home = &GlobalPos{m.Village.Dimension(), 32, 64, 0}
	tick := &tickView{w: w, others: []*Mob{o}}
	if got := m.villageCenters(tick); len(got) != 1 || got[0] != (Waypoint{2, 4, 0}) {
		t.Fatalf("centers %v", got)
	}
	o.vb.home.Dim = "minecraft:the_nether"
	if got := m.villageCenters(tick); len(got) != 0 {
		t.Fatalf("foreign memory counted: %v", got)
	}
	o.vb.home.Dim = m.Village.Dimension()
	w.set(32, 64, 0, defaultState(t, "minecraft:lectern"))
	if got := m.villageCenters(tick); len(got) != 0 {
		t.Fatalf("stale home counted: %v", got)
	}
	w.set(32, 64, 0, bedHead(t))
	if got := m.villageCenters(&tickView{w: w}); len(got) != 0 {
		t.Fatalf("unloaded owner counted: %v", got)
	}
	m.vb.home = o.vb.home
	if got := m.villageCenters(&tickView{w: w}); len(got) != 1 {
		t.Fatalf("own memory omitted: %v", got)
	}
}

func TestVillageBoundAndClosestVillagePicksMoveTowardOwners(t *testing.T) {
	w := newTestWorld()
	floor(w, -35, -35, 110, 35, 63, defaultState(t, "minecraft:stone"))
	w.set(96, 64, 0, bedHead(t))
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 2162)
	m.Village = fakeVillage{w: w}
	o := NewMob(TypeVillager, 2, 96.5, 64, 0.5, 2)
	o.vb.home = &GlobalPos{m.Village.Dimension(), 96, 64, 0}
	tick := &tickView{w: w, others: []*Mob{o}}
	picked := 0
	for range 20 {
		if p, ok := m.villageBoundPos(tick, 10, 7); ok {
			picked++
			if p.X < 0 {
				t.Fatalf("village-bound pick went away from the village: %v", p)
			}
		}
	}
	if picked == 0 {
		t.Fatal("no village-bound land picks")
	}
	p, ok := m.closestVillagePos(tick)
	if !ok || villageDistance(sectionOf(p), m.villageCenters(tick)) > 6 {
		t.Fatalf("closest-village pick %v, %v moved farther away", p, ok)
	}
	m.X = 80.5 // already within one section of the occupied bed
	if _, ok := m.closestVillagePos(tick); ok {
		t.Fatal("village walk started inside a village")
	}
}

func TestInsideBrownianWalkNeedsShadeSupportAndAClearCurrentBody(t *testing.T) {
	w := flatWorld(t)
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	w.sky = 15
	tick := &tickView{w: w}
	if _, ok := m.insidePos(tick); ok {
		t.Fatal("indoor walk started under open sky")
	}
	w.shade(0, 64, 0, 0)
	w.shade(1, 63, 0, 0)
	p, ok := m.insidePos(tick)
	if !ok || p != (Waypoint{1, 63, 0}) {
		t.Fatalf("indoor pick %v, %v; want shaded supporting block 1 63 0", p, ok)
	}
	w.set(0, 64, 0, defaultState(t, "minecraft:stone"))
	if _, ok := m.insidePos(tick); ok {
		t.Fatal("indoor walk started with a colliding current body")
	}
}

func TestHomelessRestWalkStopsAtHomeOrDayAndHonorsItsPause(t *testing.T) {
	w := flatWorld(t)
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 2)
	m.vb.activity = actRest
	g := &restWanderGoal{m: m}
	for range 100 {
		if g.CanStart(&tickView{w: w}) && g.wait > 0 {
			break
		}
	}
	if g.wait < 20 || g.wait > 40 {
		t.Fatalf("pause %d, want 20-40 ticks", g.wait)
	}
	for g.wait > 0 {
		if !g.ShouldContinue(nil) {
			t.Fatal("pause stopped too early")
		}
		g.Tick(nil)
	}
	if g.ShouldContinue(nil) {
		t.Fatal("pause did not end")
	}
	g.wait = 20
	m.vb.home = &GlobalPos{}
	if g.ShouldContinue(nil) || g.CanStart(&tickView{w: w}) {
		t.Fatal("rest walk ignored home")
	}
	m.vb.home, m.vb.activity = nil, actIdle
	if g.ShouldContinue(nil) || g.CanStart(&tickView{w: w}) {
		t.Fatal("rest walk continued by day")
	}
}
