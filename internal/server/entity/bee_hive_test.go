package entity

import (
	"math/rand"
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

func TestABeeWillNotEnterAFullHiveAndForgetsIt(t *testing.T) {
	m, tv := hiveBee(3)
	m.setNectar(true)
	g := &beeEnterHiveGoal{m: m}
	if g.CanStart(tv) {
		t.Fatal("a bee enters a full hive")
	}
	if m.BeeMemory().Hive != nil {
		t.Fatal("the bee keeps a full hive")
	}
}

func TestABeeFarFromItsHiveDoesNotEnter(t *testing.T) {
	m, tv := hiveBee(0)
	m.setNectar(true)
	m.X = 2.6
	if (&beeEnterHiveGoal{m: m}).CanStart(tv) {
		t.Fatal("a bee 2.1 blocks from the hive's centre goes in")
	}
}

func TestABeeFindsTheNearestOpenHive(t *testing.T) {
	w := newTestWorld()
	w.hives = map[[3]int32]int{{5, 64, 0}: 3, {9, 64, 0}: 1, {15, 64, 0}: 0, {40, 64, 0}: 0}
	m := NewMob(TypeBee, 1, 0.5, 64.5, 0.5, 1)
	m.setNectar(true)
	tv := &tickView{w: w}
	g := &beeLocateHiveGoal{m: m}
	if !g.CanStart(tv) {
		t.Fatal("no search")
	}
	g.Start(tv)
	if got := m.BeeMemory().Hive; got == nil || *got != [3]int32{9, 64, 0} {
		t.Fatalf("hive %v, want the nearest one with room", got)
	}
}

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

func TestABeeLeavesAFlowerInTheRainAlone(t *testing.T) {
	w := newTestWorld()
	w.rain = true
	w.set(0, 64, 0, beeBlock(t, "minecraft:poppy", nil))
	m := NewMob(TypeBee, 1, 0.5, 64.6, 0.5, 1)
	if (&beePollinateGoal{m: m}).CanStart(&tickView{w: w}) {
		t.Fatal("a bee pollinates in the rain")
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

func TestABeeWithoutNectarGrowsNothing(t *testing.T) {
	m, tv := hiveBee(0)
	for range 100 {
		if (&beeGrowCropGoal{m: m}).CanStart(tv) {
			t.Fatal("a bee with no nectar grows crops")
		}
	}
}

func TestAPollinatingBeeBuzzesNoMoreOftenThanEverySixtyTicks(t *testing.T) {
	w := newTestWorld()
	w.set(0, 64, 0, beeBlock(t, "minecraft:poppy", nil))
	m := NewMob(TypeBee, 1, 0.5, 64.6, 0.5, 1)
	tv := &tickView{w: w}
	g := &beePollinateGoal{m: m}
	g.CanStart(tv)
	g.Start(tv)
	var at []int
	for i := range 500 {
		tv.events = nil
		g.Tick(tv)
		for _, ev := range tv.events {
			if ev.Kind == MobSounded && ev.SoundID.Name() == "minecraft:entity.bee.pollinate" {
				at = append(at, i)
			}
		}
	}
	if len(at) == 0 {
		t.Fatal("no pollinate sound in 500 ticks")
	}
	for i := 1; i < len(at); i++ {
		if at[i]-at[i-1] <= 60 {
			t.Fatalf("buzzes at %d and %d", at[i-1], at[i])
		}
	}
}

func TestABeeFliesToTheFlowerItRemembersAfterSixHundredTicksWithoutNectar(t *testing.T) {
	m, tv := hiveBee(0)
	m.SetBeeMemory(BeeMemory{Flower: &[3]int32{9, 64, 0}})
	g := &beeGoToKnownFlowerGoal{m: m}
	if g.CanStart(tv) {
		t.Fatal("a bee with nectar in mind goes at once")
	}
	m.bee.mem.noNectar = beeKnownFlowerAfter + 1
	if !g.CanStart(tv) {
		t.Fatal("a bee tired of waiting stays")
	}
	g.Start(tv)
	g.Tick(tv)
	if !m.flightOn() {
		t.Fatal("no flight started")
	}
	m.SetBeeMemory(BeeMemory{Flower: &[3]int32{0, 64, 0}, NoNectar: beeKnownFlowerAfter + 1})
	if g.CanStart(tv) {
		t.Fatal("a bee beside the flower walks to it")
	}
	m.SetBeeMemory(BeeMemory{Flower: &[3]int32{100, 64, 0}, NoNectar: beeKnownFlowerAfter + 1})
	m.stopSeeking()
	g.Tick(tv)
	if m.bee.mem.hasFlower {
		t.Fatal("a flower 100 blocks away is kept")
	}
}

func TestARestrictedBeeStaysOffItsKnownFlowerAndHive(t *testing.T) {
	m, tv := hiveBee(0)
	m.setNectar(true)
	m.SetBeeMemory(BeeMemory{Hive: &[3]int32{0, 64, 0}, Flower: &[3]int32{9, 64, 0}, NoNectar: beeKnownFlowerAfter + 1})
	m.X = 6.5
	w := tv.w.(testWorld)
	w.set(0, 64, 0, beeBlock(t, "minecraft:bee_nest", map[string]string{"facing": "north", "honey_level": "0"}))
	tv.w = w
	flower, hive := &beeGoToKnownFlowerGoal{m: m}, &beeGoToHiveGoal{m: m}
	if !flower.CanStart(tv) {
		t.Fatal("a bee without a home restriction skips its flower")
	}
	if !hive.CanStart(tv) {
		t.Fatal("a bee without a home restriction skips its hive")
	}
	m.RestrictTo(0, 64, 0, 16)
	if flower.CanStart(tv) {
		t.Fatal("a restricted bee flies to its flower")
	}
	if hive.CanStart(tv) {
		t.Fatal("a restricted bee flies to its hive")
	}
}

func TestABeeGivesUpTheKnownFlowerAfterTwentyFourHundredTicks(t *testing.T) {
	m, tv := hiveBee(0)
	m.SetBeeMemory(BeeMemory{Flower: &[3]int32{9, 64, 0}, NoNectar: beeKnownFlowerAfter + 1})
	g := &beeGoToKnownFlowerGoal{m: m}
	g.Start(tv)
	for range beeMaxTravel {
		g.Tick(tv)
	}
	if !m.bee.mem.hasFlower {
		t.Fatal("the flower went early")
	}
	g.Tick(tv)
	if m.bee.mem.hasFlower {
		t.Fatal("the flower is kept after 2400 ticks of travel")
	}
}

func TestABeeWithNectarGrowsBerriesOnTheCaveVineBelow(t *testing.T) {
	m, tv := hiveBee(0)
	w := tv.w.(testWorld)
	w.set(0, 63, 0, beeBlock(t, "minecraft:cave_vines_plant", map[string]string{"berries": "false"}))
	tv.w = w
	m.Y = 64.2
	m.setNectar(true)
	g := &beeGrowCropGoal{m: m}
	for range 4000 {
		if g.CanStart(tv) {
			g.Tick(tv)
		}
		if len(tv.events) > 0 {
			break
		}
	}
	if len(tv.events) != 1 || tv.events[0].Kind != MobGrewCrop || tv.events[0].Y != 63 ||
		tv.events[0].BlockState != beeBlock(t, "minecraft:cave_vines_plant", map[string]string{"berries": "true"}) {
		t.Fatalf("events %+v", tv.events)
	}
}

func TestABabyBeeIsHalfTheSizeOfAnAdult(t *testing.T) {
	m := NewMob(TypeBee, 1, 0.5, 64.5, 0.5, 1)
	if m.width() != 0.55 || m.height() != 0.5 {
		t.Fatalf("an adult bee is %v by %v, want 0.55 by 0.5", m.width(), m.height())
	}
	m.SetBaby(true)
	if m.width() != 0.275 || m.height() != 0.25 {
		t.Fatalf("a baby bee is %v by %v, want 0.275 by 0.25", m.width(), m.height())
	}
}

func TestABeeIsCloserOnlyStrictlyInsideTheDistance(t *testing.T) {
	m := NewMob(TypeBee, 1, 0.5, 64.5, 0.5, 1)
	if !m.beeCloserThan([3]int32{2, 64, 0}, 3) {
		t.Fatal("a hive two cells off is not closer than 3")
	}
	if m.beeCloserThan([3]int32{3, 64, 0}, 3) {
		t.Fatal("a hive exactly 3 cells off is closer than 3")
	}
}

func TestABeeGrowCropTickRollsOneInFifteen(t *testing.T) {
	m, tv := hiveBee(0)
	w := tv.w.(testWorld)
	w.set(0, 63, 0, beeBlock(t, "minecraft:wheat", map[string]string{"age": "2"}))
	m.Y = 64.2
	g := &beeGrowCropGoal{m: m}
	hits := 0
	for seed := range int64(300) {
		m.Rand = rand.New(rand.NewSource(seed))
		want := rand.New(rand.NewSource(seed)).Intn(15) == 0
		tv.events = nil
		g.Tick(tv)
		if grew := len(tv.events) == 1; grew != want {
			t.Fatalf("seed %d: grew %v, want %v", seed, grew, want)
		}
		if want {
			hits++
		}
	}
	if hits == 0 {
		t.Fatal("no seed rolled a grow")
	}
}

func TestAnAngryBeeGrowsNoCrops(t *testing.T) {
	m, tv := hiveBee(0)
	m.setNectar(true)
	m.anger.ticks = 400
	if !m.Angry() {
		t.Fatal("the bee is not angry")
	}
	for range 100 {
		if (&beeGrowCropGoal{m: m}).CanStart(tv) {
			t.Fatal("an angry bee grows crops")
		}
	}
}
