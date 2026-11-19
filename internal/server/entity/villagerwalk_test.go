package entity

import "testing"

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
