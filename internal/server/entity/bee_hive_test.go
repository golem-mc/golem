package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func beeBlock(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	id, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("no state %s", name)
	}
	return id
}

// hiveBee is a bee at (0.5, 64.5, 0.5) whose hive stands at (0, 64, 0) with n
// bees in it.
func hiveBee(n int) (*Mob, *tickView) {
	w := newTestWorld()
	w.hives = map[[3]int32]int{{0, 64, 0}: n}
	m := NewMob(TypeBee, 1, 0.5, 64.5, 0.5, 1)
	m.SetBeeHive(0, 64, 0)
	return m, &tickView{w: w}
}

func TestABeeEntersAHiveOnlyWhenItWantsTo(t *testing.T) {
	m, tv := hiveBee(0)
	g := &beeEnterHiveGoal{m: m}
	if g.CanStart(tv) {
		t.Fatal("a bee with no nectar by day goes in")
	}
	m.setNectar(true)
	if !g.CanStart(tv) {
		t.Fatal("a bee with nectar stays out")
	}
	m.SetBeeStayOut(10)
	if g.CanStart(tv) {
		t.Fatal("a bee kept out goes in")
	}
	m.SetBeeStayOut(0)
	m.Sting()
	if g.CanStart(tv) {
		t.Fatal("a bee that stung goes in")
	}
	g.Start(tv)
	if len(tv.events) != 1 || tv.events[0].Kind != MobEnteredHive {
		t.Fatalf("events %v", tv.events)
	}
}

func TestABeeGoesHomeAtNightAndAfterAWhileWithoutNectar(t *testing.T) {
	m, tv := hiveBee(0)
	g := &beeEnterHiveGoal{m: m}
	w := tv.w.(testWorld)
	w.staysIn = true
	tv.w = w
	if !g.CanStart(tv) {
		t.Fatal("a bee stays out at night")
	}
	tv.w = tv.w.(testWorld).withStays(false)
	m.bee.mem.noNectar = beeTiredAfter + 1
	if !g.CanStart(tv) {
		t.Fatal("a bee tired of looking for nectar stays out")
	}
}

func (w testWorld) withStays(v bool) testWorld { w.staysIn = v; return w }

func TestAPollinatingBeeEarnsNectarAfterFourHundredTicks(t *testing.T) {
	w := newTestWorld()
	w.set(0, 64, 0, beeBlock(t, "minecraft:poppy", nil))
	m := NewMob(TypeBee, 1, 0.5, 64.6, 0.5, 1)
	tv := &tickView{w: w}
	g := &beePollinateGoal{m: m}
	if !g.CanStart(tv) {
		t.Fatal("no flower found")
	}
	g.Start(tv)
	for range 400 {
		g.Tick(tv)
	}
	g.Stop(tv)
	if m.HasNectar() {
		t.Fatal("nectar after 400 ticks")
	}
	g.Start(tv)
	for range 402 {
		g.Tick(tv)
	}
	g.Stop(tv)
	if !m.HasNectar() {
		t.Fatal("no nectar after 402 ticks of pollination")
	}
}

func TestABeeWithNectarGrowsTheCropBelowAndStopsAtTen(t *testing.T) {
	m, tv := hiveBee(0)
	w := tv.w.(testWorld)
	w.set(0, 63, 0, beeBlock(t, "minecraft:wheat", map[string]string{"age": "2"}))
	tv.w = w
	m.Y = 64.2
	m.setNectar(true)
	g := &beeGrowCropGoal{m: m}
	grown := 0
	for range 4000 {
		if g.CanStart(tv) {
			g.Tick(tv)
		}
		for _, ev := range tv.events {
			if ev.Kind == MobGrewCrop {
				grown++
				if ev.BlockState != beeBlock(t, "minecraft:wheat", map[string]string{"age": "3"}) || ev.Y != 63 {
					t.Fatalf("grew to %d at y %v", ev.BlockState, ev.Y)
				}
			}
		}
		tv.events = nil
		if grown > 0 {
			break
		}
	}
	if grown != 1 {
		t.Fatalf("grew %d crops", grown)
	}
	m.bee.mem.crops = beeMaxCrops
	if g.CanStart(tv) {
		t.Fatal("a bee past ten crops grows another")
	}
}
