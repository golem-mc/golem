package entity

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestDolphinBreathMoistnessAndSave(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeDolphin, 1, 0.5, 64, 0.5, 1)
	m.waterFluid.height, m.waterFluid.eyesIn = 1, true
	m.AirSupply = 1
	m.airStep(nil)
	if m.AirSupply != 0 || m.Health != 10 {
		t.Fatal("dolphin did not spend ordinary breath")
	}
	trapped := NewMob(TypeDolphin, 3, 0, 64, 0, 3)
	trapped.waterFluid.eyesIn = true
	trapped.AirSupply = -19
	if ev := trapped.airStep(nil); len(ev) != 1 || trapped.Health != 8 || trapped.AirSupply != 0 {
		t.Fatal("submerged dolphin did not drown at the air floor")
	}
	m.waterFluid.eyesIn = false
	m.airStep(nil)
	if m.AirSupply != 4800 {
		t.Fatal("surfacing did not fully refill breath")
	}
	m.waterFluid.eyesIn = true
	m.waterFluid.bubbleAtEyes = true
	m.AirSupply = 1
	m.airStep(nil)
	if m.AirSupply != 4800 {
		t.Fatal("bubble column did not refill breath")
	}
	m.dolphin.moistness = 1
	m.dolphinStep(w, nil)
	if m.dolphin.moistness != 2400 {
		t.Fatal("water did not rehydrate")
	}
	m.waterFluid.height = 0
	m.NoAI = true
	m.dolphin.moistness = 1
	m.AirSupply = 2
	m.dolphinStep(w, nil)
	if m.AirSupply != 4800 || m.dolphin.moistness != 1 {
		t.Fatal("NoAI dried out")
	}
	m.NoAI = false
	w.rain = true
	m.dolphinStep(w, nil)
	if m.dolphin.moistness != 2400 {
		t.Fatal("rain did not rehydrate")
	}
	w.rain = false
	m.dolphin.moistness = 1
	events := m.dolphinStep(w, nil)
	if len(events) != 1 || events[0].DamageType != registry.DamageTypeDryOut || m.Health != 9 {
		t.Fatalf("dry-out = %+v, health %g", events, m.Health)
	}
	m.dolphin.gotFish = true
	m.dolphin.moistness = 1234
	m.SetBaby(true)
	tag := nbt.Compound{}
	m.SaveTo(tag, nil)
	back := NewMob(TypeDolphin, 2, 0, 64, 0, 2)
	back.LoadFrom(tag, nil, nil)
	if !back.dolphin.gotFish || back.dolphin.moistness != 1234 || !back.IsBaby() || back.AirSupply != 4800 {
		t.Fatalf("lost saved state: %v", tag)
	}
	if IsAnimal(TypeDolphin) || !Leashable(TypeDolphin) || back.eyeHeight() != 0.09375 {
		t.Fatal("ageable water creature capabilities")
	}
	back.LoadFrom(nbt.Compound{}, nil, nil)
	if back.dolphin.gotFish {
		t.Fatal("absent GotFish did not default false")
	}
}

func TestDolphinSurfaceSpawnAndBabies(t *testing.T) {
	w := newSpawnTestWorld()
	water := registry.BlockWater.DefaultState()
	for _, y := range []int32{49, 50, 63, 64} {
		w.set(0, y-1, 0, water)
		w.set(0, y, 0, water)
		w.set(0, y+1, 0, water)
		if got := dolphinSpawnOk(w, 0, y, 0); got != (y >= 50 && y <= 63) {
			t.Errorf("spawn y=%d: %v", y, got)
		}
	}
	w.set(0, 64, 0, registry.BlockStone.DefaultState())
	if dolphinSpawnOk(w, 0, 63, 0) {
		t.Fatal("spawn accepted solid overhead")
	}
	if !WaterCreatureKind(TypeDolphin) || CreatureKind(TypeDolphin) {
		t.Fatal("wrong spawn category")
	}
	babies := 0
	group := NewSpawnGroup(GoDice{rand.New(rand.NewSource(9))})
	for i := range 1000 {
		m := NewMob(TypeDolphin, int32(i+1), 0, 62, 0, int64(i))
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:ocean"), group, rules.LocalDifficulty{})
		if m.IsBaby() {
			babies++
		}
	}
	if babies < 70 || babies > 130 {
		t.Fatalf("%d babies, want about 10%%", babies)
	}
}

func TestFedDolphinApproachesTreasureAndClearsFishWhenStuck(t *testing.T) {
	w := newTestWorld()
	water := registry.BlockWater.DefaultState()
	for y := int32(58); y < 68; y++ {
		floor(w, -32, -32, 64, 32, y, water)
	}
	m := NewMob(TypeDolphin, 1, 0.5, 62, 0.5, 42)
	m.FeedDolphin()
	if !m.DolphinNeedsTreasure() {
		t.Fatal("fed dolphin did not request a structure")
	}
	m.SetDolphinTreasure(40, 0, true)
	for range 200 {
		m.tickInto(nil, &tickView{}, w, nil, nil)
	}
	if d := math.Hypot(m.X-40.5, m.Z-0.5); d >= 40 {
		t.Fatalf("distance to treasure %g, want below 40", d)
	}

	trapped := NewMob(TypeDolphin, 2, 0.5, 62, 0.5, 42)
	trapped.FeedDolphin()
	trapped.SetDolphinTreasure(40, 0, true)
	// No legal random step through this solid enclosure.
	solid := newTestWorld()
	for y := int32(55); y < 70; y++ {
		floor(solid, -20, -20, 20, 20, y, registry.BlockStone.DefaultState())
	}
	g := &dolphinTreasureGoal{m: trapped}
	tv := &tickView{w: solid}
	g.Start(tv)
	g.Tick(tv)
	if g.ShouldContinue(tv) {
		t.Fatal("stuck dolphin continued treasure goal")
	}
	g.Stop(tv)
	if trapped.dolphin.gotFish {
		t.Fatal("stuck dolphin kept GotFish")
	}
}

func TestDolphinJumpRequiresClearSurfaceAndImpulsesForward(t *testing.T) {
	w := newTestWorld()
	floor(w, -8, -8, 8, 8, 62, registry.BlockWater.DefaultState())
	m := NewMob(TypeDolphin, 1, 0.5, 62, 0.5, 42)
	g := &dolphinJumpGoal{m: m}
	tv := &tickView{w: w}
	started := false
	for range 100 {
		if g.CanStart(tv) {
			started = true
			break
		}
	}
	if !started {
		t.Fatal("never jumped at clear surface")
	}
	g.Start(tv)
	if m.VY != 0.7 || m.VZ != 0.6 || m.VX != 0 {
		t.Fatalf("jump velocity %g %g %g", m.VX, m.VY, m.VZ)
	}
	g.Tick(tv)
	if len(tv.events) != 1 || tv.events[0].SoundID.Name() != "minecraft:entity.dolphin.jump" || m.Pitch >= 0 {
		t.Fatal("missing jump sound or pitch")
	}
	m.OnGround = true
	if g.ShouldContinue(tv) {
		t.Fatal("continued after landing")
	}
	g.Stop(tv)
	if m.Pitch != 0 {
		t.Fatal("pitch not reset")
	}
	w.set(0, 63, 4, registry.BlockStone.DefaultState())
	for range 100 {
		if g.CanStart(tv) {
			t.Fatal("jumped into blocked surface")
		}
	}
}

func TestDolphinPlaysWithWaterItemsAndThrowsItsWholeHand(t *testing.T) {
	w := newTestWorld()
	floor(w, -8, -8, 8, 8, 62, registry.BlockWater.DefaultState())
	m := NewMob(TypeDolphin, 1, 0.5, 62, 0.5, 42)
	if m.DispenserEquip(protocol.ItemStack{ItemID: int32(registry.ItemIronHelmet), Count: 1}) {
		t.Fatal("dispenser equipped dolphin armor")
	}
	m.DolphinItems = []FoxItem{{X: 3.5, Y: 62, Z: 0.5}}
	g := &dolphinPlayGoal{m: m}
	tv := &tickView{w: w}
	if !g.CanStart(tv) {
		t.Fatal("did not notice water item")
	}
	g.Start(tv)
	if len(tv.events) != 1 || tv.events[0].SoundID.Name() != "minecraft:entity.dolphin.play" {
		t.Fatal("no play sound")
	}
	stack := protocol.ItemStack{ItemID: int32(registry.ItemStick), Count: 12}
	takes := Mobs{m}.TakeLoot([]LootItem{{X: m.X, Y: m.Y, Z: m.Z, Stack: stack}})
	if len(takes) != 1 || takes[0].Count != 12 || takes[0].Slot != "mainhand" {
		t.Fatalf("pickup %+v", takes)
	}
	m.TookLoot(takes[0], stack)
	if m.Persistence {
		t.Fatal("pickup made dolphin persistent")
	}
	g.Tick(tv)
	ev := tv.events[len(tv.events)-1]
	if !m.MainHand.Empty() || ev.Kind != MobThrewItem || ev.Stack.Count != 12 || ev.VZ <= 0 {
		t.Fatalf("throw %+v", ev)
	}
	m.DolphinItems = nil
	if g.ShouldContinue(tv) {
		t.Fatal("continued with no items")
	}
	m.MainHand = stack
	g.Stop(tv)
	if !m.MainHand.Empty() || g.cooldown < m.Age {
		t.Fatal("stop did not throw and cool down")
	}
	m.DolphinItems = []FoxItem{{X: 3.5, Y: 64, Z: 0.5}}
	m.Age += 100
	if g.CanStart(tv) {
		t.Fatal("played with dry item")
	}
}
