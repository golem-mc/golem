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

func TestDolphinBreathingGoalAndGuardianExemption(t *testing.T) {
	m := NewMob(TypeDolphin, 1, 0, 62, 0, 1)
	guardian := NewMob(TypeGuardian, 2, 1, 62, 0, 2)
	w := newTestWorld()
	tick := &tickView{w: w, others: []*Mob{m, guardian}}
	m.HurtByMob(1, guardian)
	for _, goal := range m.ai.goals {
		if hurt, ok := goal.(*hurtByGoal); ok && hurt.CanStart(tick) {
			t.Fatal("retaliated against guardian")
		}
	}
	g := &breathAirGoal{m: m}
	m.AirSupply = 139
	if !g.CanStart(tick) || !g.Uninterruptible() {
		t.Fatal("low-air goal did not activate")
	}
	m.AirSupply = 4800
	if g.ShouldContinue(tick) {
		t.Fatal("breathing goal continued after refill")
	}
}

func TestDolphinSurfaceNavigation(t *testing.T) {
	w := newTestWorld()
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			for y := int32(62); y <= 63; y++ {
				w.set(x, y, z, registry.BlockWater.DefaultState())
			}
			w.set(x, 65, z, registry.BlockStone.DefaultState())
		}
	}
	for _, kind := range []MobType{TypeDolphin, TypeCod} {
		m := NewMob(kind, 1, 0.5, 62, 0.5, 1)
		m.updateMobFluids(w)
		path, _, ok := m.createSwimPath(w, 0, 65, 0, 1)
		if !ok || len(path) == 0 {
			t.Fatal("no path to surface")
		}
		want := int32(63)
		if kind == TypeDolphin {
			want = 64
		}
		if end := path[len(path)-1]; end.Y != want {
			t.Errorf("%s path ended at %v, want y %d", kind, end, want)
		}
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

func TestDolphinTreasureArrivalMissingStructureAndLowAir(t *testing.T) {
	for _, tc := range []struct {
		name  string
		x     int32
		found bool
		air   int
		keep  bool
	}{
		{"arrived", 0, true, 4800, false},
		{"no structure", 40, false, 4800, false},
		{"low air", 40, true, 99, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMob(TypeDolphin, 1, 0.5, 62, 0.5, 1)
			m.FeedDolphin()
			m.SetDolphinTreasure(tc.x, 0, tc.found)
			g := &dolphinTreasureGoal{m: m}
			tv := &tickView{w: newTestWorld()}
			g.Start(tv)
			m.AirSupply = tc.air
			if g.ShouldContinue(tv) {
				t.Fatal("goal continued")
			}
			g.Stop(tv)
			if m.dolphin.gotFish != tc.keep {
				t.Fatalf("GotFish = %v", m.dolphin.gotFish)
			}
		})
	}
}

func TestDolphinSwimWithPlayerSelectionAndRefresh(t *testing.T) {
	m := NewMob(TypeDolphin, 1, 0.5, 62, 0.5, 1)
	g := &dolphinSwimWithPlayerGoal{m: m}
	tv := &tickView{w: newTestWorld(), players: []Player{{EntityID: 2, X: 5.5, Y: 62, Z: 0.5, Swimming: true}}}
	if !g.CanStart(tv) {
		t.Fatal("did not choose swimmer")
	}
	g.Start(tv)
	if len(tv.events) != 1 || tv.events[0].Kind != MobDolphinGrace || tv.events[0].Player.EntityID != 2 {
		t.Fatal("no initial Grace")
	}
	for range 60 {
		g.Tick(tv)
	}
	if len(tv.events) <= 1 {
		t.Fatal("Grace was never refreshed")
	}
	tv.players[0].X = 16.5
	if g.ShouldContinue(tv) {
		t.Fatal("followed outside 16 blocks")
	}
	tv.players[0].X = 5.5
	tv.players[0].Swimming = false
	if g.CanStart(tv) || g.ShouldContinue(tv) {
		t.Fatal("followed non-swimmer")
	}
	tv.players[0].Swimming = true
	m.TargetID = 2
	if g.CanStart(tv) {
		t.Fatal("followed attack target")
	}
	m.TargetID = 0
	tv.players = append(tv.players, Player{EntityID: 3, X: 2.5, Y: 62, Z: 0.5})
	if g.CanStart(tv) {
		t.Fatal("skipped nearest non-swimmer")
	}
}

func TestFeedingBabyDolphinAgesItInsteadOfFindingTreasure(t *testing.T) {
	m := NewMob(TypeDolphin, 1, 0, 62, 0, 1)
	m.BabyAge = -1200
	m.FeedDolphin()
	if m.BabyAge != -1080 || m.dolphin.gotFish {
		t.Fatalf("age %d, GotFish %v", m.BabyAge, m.dolphin.gotFish)
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

func TestDolphinFollowsMovingBoatAndNautilusRiders(t *testing.T) {
	for _, boat := range []bool{true, false} {
		m := NewMob(TypeDolphin, 1, 0.5, 62, 0.5, 42)
		g := &dolphinRideGoal{m: m, boat: boat}
		tv := &tickView{w: newTestWorld(), players: []Player{{EntityID: 2, X: 2.5, Y: 62, Z: 0.5, RideBox: [6]float64{2, 62, 0, 3, 63, 1}, Passenger: true, BoatDriver: boat, NautilusDriver: !boat, KnownVZ: 0.1}}}
		if !g.CanStart(tv) {
			t.Fatal("did not notice moving rider")
		}
		g.Start(tv)
		if !g.ShouldContinue(tv) {
			t.Fatal("did not continue following rider")
		}
		g.Tick(tv)
		if !g.ahead {
			t.Fatal("did not switch to swimming ahead of close rider")
		}
		tv.players[0].X = 20.5
		g.Tick(tv)
		if g.ahead {
			t.Fatal("did not switch to catching up")
		}
		tv.players[0].KnownVZ = 0
		if g.ShouldContinue(tv) {
			t.Fatal("followed stationary rider")
		}
		tv.players[0].KnownVZ = 0.1
		tv.players[0].Passenger = false
		if g.ShouldContinue(tv) {
			t.Fatal("followed dismounted rider")
		}
	}
}

func TestDolphinGoalsRequestSmoothLooks(t *testing.T) {
	w := newTestWorld()
	for y := int32(58); y < 68; y++ {
		floor(w, -20, -20, 24, 20, y, registry.BlockWater.DefaultState())
	}
	for _, treasure := range []bool{false, true} {
		name := "swimmer"
		if treasure {
			name = "treasure"
		}
		t.Run(name, func(t *testing.T) {
			m := NewMob(TypeDolphin, 1, 0.5, 62, 0.5, 42)
			m.Pitch = -40
			m.updateMobFluids(w)
			tv := &tickView{w: w, players: []Player{{EntityID: 2, X: 5.5, Y: 62, Z: 0.5, Swimming: true}}}
			var g Goal = &dolphinSwimWithPlayerGoal{m: m, player: 2}
			if treasure {
				m.SetDolphinTreasure(40, 0, true)
				g = &dolphinTreasureGoal{m: m}
			}
			g.Tick(tv)
			if m.Pitch != -40 || m.HeadYaw != 0 {
				t.Fatalf("goal wrote pitch/head yaw = %g/%g before controls ran", m.Pitch, m.HeadYaw)
			}
			if m.look.cooldown != 2 || m.look.yawSpeed != 21 || m.look.pitchMax != 1 {
				t.Fatalf("look request = %+v, want cooldown 2 and turn limits 21/1", m.look)
			}
			if len(m.nav.path) == 0 {
				t.Fatal("goal did not start navigation")
			}
			if treasure {
				end := m.nav.end
				if m.look.x != float64(end.X)+0.5 || m.look.y != float64(end.Y) || m.look.z != float64(end.Z)+0.5 {
					t.Fatalf("look target = %+v, want navigation target %v", m.look, end)
				}
			} else if m.look.x != 5.5 || m.look.y != 62.4 || m.look.z != 0.5 {
				t.Fatalf("look target = %+v, want swimmer's eyes at 5.5/62.4/0.5", m.look)
			}
			// vanilla: Mob.serverAiStep runs navigation/move before look.
			// These goals tick every other tick; the request must last for both.
			for i, wantPitch := range []float32{-34, -28} {
				m.moveControl(w)
				m.lookControlTick(true)
				wantHead := float32(-31 - i*10)
				if m.Pitch != wantPitch || m.HeadYaw != wantHead || m.look.cooldown != 1-i {
					t.Fatalf("tick %d: pitch/head yaw/cooldown = %g/%g/%d, want %g/%g/%d", i+1,
						m.Pitch, m.HeadYaw, m.look.cooldown, wantPitch, wantHead, 1-i)
				}
			}
		})
	}
}
