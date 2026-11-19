package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestArmadilloSaveKeepsStateAndScuteTimer(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"idle", "rolling", "scared", "unrolling", "unknown"} {
		m := NewMob(TypeArmadillo, 1, 0, 64, 0, 1)
		if m.ScuteTimer < 6000 || m.ScuteTimer >= 12000 {
			t.Fatalf("initial timer = %d", m.ScuteTimer)
		}
		m.LoadFrom(nbt.Compound{"state": nbt.String(state), "scute_time": nbt.Int(-3)}, nil, nil)
		saved := nbt.Compound{}
		m.SaveTo(saved, nil)
		want := state
		if state == "unknown" {
			want = "idle"
		}
		if saved["state"] != nbt.String(want) || saved["scute_time"] != nbt.Int(-3) {
			t.Fatalf("saved state/timer = %v/%v", saved["state"], saved["scute_time"])
		}
		back := NewMob(TypeArmadillo, 2, 0, 64, 0, 2)
		back.LoadFrom(saved, nil, nil)
		if back.ArmadilloState() != m.ArmadilloState() || back.ScuteTimer != -3 {
			t.Fatal("state/timer lost on reload")
		}
	}
}

func TestArmadilloSpawnGroundAndLight(t *testing.T) {
	t.Parallel()
	w := newSpawnTestWorld()
	w.sky = 15
	for _, ground := range []string{"minecraft:grass_block", "minecraft:red_sand", "minecraft:coarse_dirt", "minecraft:terracotta", "minecraft:stone"} {
		w.set(0, 63, 0, defaultState(t, ground))
		got := validSpawnPositionAt(w, GoDice{rand.New(rand.NewSource(1))}, TypeArmadillo, 0.5, 64, 0.5, 100, CategoryCreature)
		if want := ground != "minecraft:stone"; got != want {
			t.Errorf("spawn on %s = %v, want %v", ground, got, want)
		}
	}
	w.set(0, 63, 0, defaultState(t, "minecraft:grass_block"))
	w.sky = 0
	if validSpawnPositionAt(w, GoDice{rand.New(rand.NewSource(1))}, TypeArmadillo, 0.5, 64, 0.5, 100, CategoryCreature) {
		t.Fatal("spawned in darkness")
	}
}

func TestArmadilloShedWaitsForAIAndResetsTimer(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	m := NewMob(TypeArmadillo, 1, 0, 64, 0, 1)
	m.ScuteTimer = 1
	m.NoAI = true
	m.Tick(w, nil)
	if m.ScuteTimer != 1 {
		t.Fatal("NoAI changed timer")
	}
	m.NoAI = false
	found := false
	for _, event := range m.Tick(w, nil) {
		if event.Kind == MobShedScute {
			found = true
		}
	}
	if !found || m.ScuteTimer < 6000 || m.ScuteTimer >= 12000 {
		t.Fatalf("shed = %v, next timer = %d", found, m.ScuteTimer)
	}
	m.ScuteTimer = 0
	m.Health = 0
	tick := tickView{w: w}
	m.armadilloStep(&tick)
	if len(tick.events) != 0 {
		t.Fatal("dead armadillo shed a scute")
	}
}

func TestBabyArmadilloKeepsExpiredScuteTimer(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	m := NewMob(TypeArmadillo, 1, 0, 64, 0, 1)
	m.BabyAge = -100
	m.ScuteTimer = 1
	tick := tickView{w: w}
	m.armadilloStep(&tick)
	m.armadilloStep(&tick)
	if len(tick.events) != 0 || m.ScuteTimer != -1 {
		t.Fatalf("events = %v, timer = %d; want no shed and a pending timer", tick.events, m.ScuteTimer)
	}
	m.BabyAge = 0
	m.armadilloStep(&tick)
	if len(tick.events) != 1 || m.ScuteTimer < 6000 {
		t.Fatalf("events = %v, timer = %d", tick.events, m.ScuteTimer)
	}
}

func TestArmadilloRollsAndUnrolls(t *testing.T) {
	w := newTestWorld()
	floor(w, -20, -20, 20, 20, 63, defaultState(t, "minecraft:stone"))
	m := NewMob(TypeArmadillo, 1, 0.5, 64, 0.5, 1)
	m.OnGround = true
	zombie := NewMob(TypeZombie, 2, 4.5, 64, 0.5, 2)
	zombie.NoAI = true
	herd := Mobs{m, zombie}
	herd.Tick(w, nil, nil)
	if m.ArmadilloState() != 1 {
		t.Fatalf("state = %d, want rolling", m.ArmadilloState())
	}
	x, z := m.X, m.Z
	for range 12 {
		herd.Tick(w, nil, nil)
	}
	if m.ArmadilloState() != 2 {
		t.Fatalf("state = %d, want scared", m.ArmadilloState())
	}
	if m.X != x || m.Z != z {
		t.Fatal("rolled armadillo walked")
	}
	if m.Sounds().Hurt.Name() != "minecraft:entity.armadillo.hurt_reduced" || m.Sounds().Ambient.Name() != "" {
		t.Fatalf("rolled sounds: %+v", m.Sounds())
	}
	zombie.X = 40
	unrolling := false
	for range 81 {
		herd.Tick(w, nil, nil)
		unrolling = unrolling || m.ArmadilloState() == 3
	}
	if !unrolling || m.ArmadilloState() != 0 {
		t.Fatalf("unrolling=%v, final state=%d", unrolling, m.ArmadilloState())
	}
}

func TestArmadilloScareSources(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    Player
		want bool
	}{
		{"walking", Player{EntityID: 2, X: 2}, false},
		{"sprinting", Player{EntityID: 2, X: 2, Sprinting: true}, true},
		{"riding", Player{EntityID: 2, X: 2, Passenger: true}, true},
		{"spectator", Player{EntityID: 2, X: 2, Sprinting: true, Spectator: true}, false},
		{"above", Player{EntityID: 2, X: 2, Y: 4, Sprinting: true}, false},
		{"far", Player{EntityID: 2, X: 9, Sprinting: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMob(TypeArmadillo, 1, 0, 0, 0, 1)
			m.OnGround = true
			m.armadilloScareStep(&tickView{w: newTestWorld(), players: []Player{tc.p}})
			if m.ArmadilloScared() != tc.want {
				t.Fatalf("scared=%v, want %v", m.ArmadilloScared(), tc.want)
			}
		})
	}
}

func TestSpidersAvoidIdleArmadillos(t *testing.T) {
	for _, typ := range []MobType{TypeSpider, TypeCaveSpider} {
		w := newTestWorld()
		floor(w, -25, -25, 25, 25, 63, defaultState(t, "minecraft:stone"))
		spider := NewMob(typ, 1, 0.5, 64, 0.5, 1)
		spider.OnGround = true
		armadillo := NewMob(TypeArmadillo, 2, 3.5, 64, 0.5, 2)
		armadillo.NoAI = true
		herd := Mobs{spider, armadillo}
		before := Dist2(spider.X, spider.Y, spider.Z, armadillo.X, armadillo.Y, armadillo.Z)
		for range 40 {
			herd.Tick(w, nil, nil)
		}
		after := Dist2(spider.X, spider.Y, spider.Z, armadillo.X, armadillo.Y, armadillo.Z)
		if after <= before {
			t.Fatalf("%s: distance squared %v -> %v", typ, before, after)
		}
		for _, goal := range hostileGoals(spider) {
			if g, ok := goal.(*avoidGoal); ok {
				armadillo.armadilloSwitch(2)
				if g.threatens(armadillo) {
					t.Fatal("spider fears a rolled armadillo")
				}
			}
		}
	}
}

func TestArmadilloHurtRollsAndKeepsReducedDamageNonnegative(t *testing.T) {
	m := NewMob(TypeArmadillo, 1, 0, 64, 0, 1)
	w, h := m.BbWidth(), m.BbHeight()
	if !m.TakeDamage(5, Player{EntityID: 2, X: 2, Y: 64}) || m.Health != 7 || m.ArmadilloState() != 1 {
		t.Fatalf("first hit: health=%v, state=%d", m.Health, m.ArmadilloState())
	}
	if m.BbWidth() != w || m.BbHeight() != h {
		t.Fatal("rolling changed the vanilla collision dimensions")
	}
	m.InvulnTicks = 0
	m.HurtByWorld(1, registry.DamageTypeGeneric)
	if m.Health != 7 {
		t.Fatalf("one damage changed rolled health to %v", m.Health)
	}
	m.InvulnTicks = 0
	m.HurtByWorld(5, registry.DamageTypeGeneric)
	if m.Health != 5 {
		t.Fatalf("five damage changed rolled health to %v", m.Health)
	}
}

func TestArmadilloCannotRollWhenLeashedOrRiding(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Mob)
	}{
		{"leashed", func(m *Mob) { m.LeashHolderID = 2 }},
		{"riding", func(m *Mob) { m.RidesVehicle = true }},
		{"carrying", func(m *Mob) { m.Passengers = []*Mob{NewMob(TypeZombie, 2, 0, 64, 0, 2)} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMob(TypeArmadillo, 1, 0, 64, 0, 1)
			m.OnGround = true
			tc.setup(m)
			m.armadilloScareStep(&tickView{w: newTestWorld(), players: []Player{{EntityID: 2, Y: 64, Sprinting: true}}})
			if m.ArmadilloScared() {
				t.Fatal("rolled while constrained")
			}
		})
	}
}

func TestArmadilloEnvironmentalPanicUnrolls(t *testing.T) {
	w := newTestWorld()
	floor(w, -20, -20, 20, 20, 63, defaultState(t, "minecraft:stone"))
	m := NewMob(TypeArmadillo, 1, 0.5, 64, 0.5, 1)
	m.OnGround = true
	z := NewMob(TypeZombie, 2, 4.5, 64, 0.5, 2)
	z.NoAI = true
	herd := Mobs{m, z}
	herd.Tick(w, nil, nil)
	m.HurtByWorld(5, registry.DamageTypeOnFire)
	herd.Tick(w, nil, nil)
	if m.ArmadilloScared() {
		t.Fatal("environmental panic kept the armadillo rolled up")
	}
}

// TestBrushRollCarriesTargetAndInteractingEntity checks a brush table's
// copy_name reads the armadillo as target_entity and the brusher as
// interacting_entity, and that this_entity is empty. It is not parallel: the
// pack loot layer is process-wide.
// vanilla: LootContextParamSets.ENTITY_INTERACT
func TestBrushRollCarriesTargetAndInteractingEntity(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	m := NewMob(TypeArmadillo, 1, 0, 64, 0, 1)
	name := &protocol.Text{Text: "Dillo"}
	brusher := rules.LootEntity{Type: "minecraft:player", Name: &protocol.Text{Text: "Steve"}}
	for _, c := range []struct{ source, want string }{
		{"target_entity", "Dillo"},
		{"interacting_entity", "Steve"},
		{"this", ""},
	} {
		registry.ApplyPackLoot(map[string]registry.LootTable{"minecraft:brush/armadillo": {Pools: []registry.LootPool{{
			Rolls: registry.LootConstant{Value: 1},
			Entries: []registry.LootEntry{registry.LootItemEntry{
				Item: registry.ItemArmadilloScute, Weight: 1,
				Functions: []registry.LootFunction{registry.LootCopyName{Source: c.source}},
			}},
		}}}}, nil, nil)
		stacks := m.RollArmadilloBrush(protocol.ItemStack{}, name, brusher, nil)
		if len(stacks) != 1 {
			t.Fatalf("%s: rolled %d stacks", c.source, len(stacks))
		}
		got := stacks[0].CustomName
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%s: copied %q from an absent entity", c.source, got.Text)
		case c.want != "" && (got == nil || got.Text != c.want):
			t.Errorf("%s: custom name = %v, want %q", c.source, got, c.want)
		}
	}
}

func TestBabyArmadilloOnlyVoicesRollAndUnrollFinish(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeArmadillo, 1, 0, 64, 0, 1)
	m.BabyAge = -100
	for _, sound := range []registry.Sound{registry.SoundEntityArmadilloRoll, registry.SoundEntityArmadilloUnrollFinish, registry.SoundEntityArmadilloLand, registry.SoundEntityArmadilloUnrollStart} {
		v := tickView{}
		m.armadilloSound(&v, sound)
		for _, ev := range v.events {
			if ev.Kind != MobSounded {
				continue
			}
			voiced := sound == registry.SoundEntityArmadilloRoll || sound == registry.SoundEntityArmadilloUnrollFinish
			if ev.Volume != 1 || (voiced && (ev.Pitch < 1.29 || ev.Pitch > 1.71)) || (!voiced && ev.Pitch != 1) {
				t.Errorf("%v: volume=%v pitch=%v; voiced=%v", sound, ev.Volume, ev.Pitch, voiced)
			}
		}
	}
}

// A brain-style follower ends its walk once within min-1 blocks Manhattan of
// the adult's block, instead of walking on to the Euclidean distance.
func TestBabyBrainFollowStopsAtManhattanArrival(t *testing.T) {
	for _, typ := range []MobType{TypeArmadillo, TypeCamel} {
		w := newTestWorld()
		floor(w, -25, -25, 25, 25, 63, defaultState(t, "minecraft:stone"))
		baby := NewMob(typ, 1, 12.5, 64, 0.5, 1)
		baby.BabyAge = -24000
		baby.OnGround = true
		adult := NewMob(typ, 2, 0.5, 64, 0.5, 2)
		adult.NoAI = true
		herd := Mobs{baby, adult}
		for range 400 {
			herd.Tick(w, nil, nil)
		}
		d := manhattan(baby.cell(), adult.cell())
		if d < 4 {
			t.Fatalf("%v: baby walked to Manhattan distance %d, vanilla stops at 4", typ, d)
		}
	}
}
