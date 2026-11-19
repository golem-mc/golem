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
