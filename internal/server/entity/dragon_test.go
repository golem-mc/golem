package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestDragonPartDamageAndImmunities(t *testing.T) {
	m := NewMob(TypeEnderDragon, 1, 0, 128, 0, 1)
	m.SetDragonPhase(DragonHolding, 0)
	for part := range 8 {
		m.Health, m.InvulnTicks = 200, 0
		m.AimDragonPart(part)
		m.TakeDamage(8, Player{EntityID: 7, X: 5, Y: 128})
		want := 197.0
		if part == DragonHead || part == DragonNeck {
			want = 192
		}
		if m.Health != want {
			t.Errorf("part %d health=%v, want %v", part, m.Health, want)
		}
	}
	m.Health, m.InvulnTicks = 200, 0
	m.HurtByMob(8, NewMob(TypeZombie, 10, 0, 128, 0, 2))
	m.HurtByWorld(8, rules.DamageTypeCactus)
	if m.Health != 200 {
		t.Fatalf("non-player damage changed health: %v", m.Health)
	}
	if _, ok := m.AddEffect(effect.Instance{ID: 1, Duration: 200}); ok {
		t.Error("dragon accepted an effect")
	}
	m.dragon.phase = dragonDying
	m.AimDragonPart(DragonHead)
	if m.TakeDamage(8, Player{EntityID: 7, X: 5, Y: 128}) || m.Health != 200 {
		t.Error("dying phase accepted damage")
	}
}

func TestDragonSavedPhaseAndCrystalDestruction(t *testing.T) {
	m := NewMob(TypeEnderDragon, 1, 0, 128, 0, 1)
	m.SetDragonPhase(DragonHolding, 0)
	m.LoadFrom(nbt.Compound{"DragonPhase": nbt.Int(0), "DragonDeathTime": nbt.Int(12), "sitting_damage_received": nbt.Float(4)}, nil, func() int64 { return 0 })
	tag := nbt.Compound{}
	m.SaveTo(tag, func() int64 { return 0 })
	if tag["DragonPhase"] != nbt.Int(0) || tag["DragonDeathTime"] != nbt.Int(12) || tag["sitting_damage_received"] != nbt.Float(4) {
		t.Fatalf("saved fields=%v", tag)
	}
	m.dragon.crystal = 20
	if landed, softened := m.DragonCrystalDestroyed(21, 0, 10, &Player{EntityID: 7}); landed || softened {
		t.Fatalf("unrelated crystal damage=%v/%v", landed, softened)
	}
	if m.Health != 200 {
		t.Fatal("unrelated crystal hurt dragon")
	}
	if landed, softened := m.DragonCrystalDestroyed(20, 0, 10, &Player{EntityID: 7}); !landed || softened {
		t.Fatalf("healing crystal damage=%v/%v", landed, softened)
	}
	if m.Health != 190 {
		t.Fatalf("healing crystal explosion health=%v", m.Health)
	}
	if typ := m.LastDamageType(); typ != registry.DamageTypePlayerExplosion {
		t.Fatalf("player's crystal damage type=%s", typ)
	}
	if landed, softened := m.DragonCrystalDestroyed(20, 0, 10, &Player{EntityID: 7}); !landed || !softened || m.Health != 190 {
		t.Fatalf("repeated crystal damage=%v/%v health=%v", landed, softened, m.Health)
	}
	m.InvulnTicks = 0
	if landed, softened := m.DragonCrystalDestroyed(20, 0, 10, nil); !landed || softened || m.Health != 180 {
		t.Fatalf("unattributed crystal damage=%v/%v health=%v", landed, softened, m.Health)
	}
	if typ := m.LastDamageType(); typ != registry.DamageTypeExplosion {
		t.Fatalf("unattributed crystal damage type=%s", typ)
	}
	m.DragonCrystals(nil)
	if m.dragon.crystal != 0 {
		t.Fatal("removed crystal stayed selected")
	}
}

func TestDragonContactAttacks(t *testing.T) {
	m := NewMob(TypeEnderDragon, 1, 0, 128, 0, 1)
	m.SetDragonPhase(DragonHolding, 0)
	m.dragonParts()
	p := Player{EntityID: 20, X: 8, Y: 128, Age: 20}
	events := m.dragonContacts([]Player{p}, nil, nil)
	if len(events) != 1 || events[0].Damage != 5 || math.Abs(events[0].ContactPush[0]-32/64.25) > 1e-10 || events[0].ContactPush[1] != 0.2 {
		t.Fatalf("wing contact=%+v", events)
	}
	p.HurtByStamp = 18
	if events = m.dragonContacts([]Player{p}, nil, nil); len(events) != 1 || events[0].Damage != 0 || events[0].ContactPush[1] != 0.2 {
		t.Fatalf("recent victim contact=%+v", events)
	}
	p.X, p.Z = 0, -7
	events = m.dragonContacts([]Player{p}, nil, nil)
	if len(events) != 2 || events[0].Damage != 10 || events[1].Damage != 10 || events[0].ContactPush != [3]float64{} {
		t.Fatalf("head/neck contact=%+v", events)
	}
	for _, p := range []Player{{X: 8, Y: 128, Creative: true}, {X: 8, Y: 128, Spectator: true}, {X: 8, Y: 128, Dead: true}} {
		if events = m.dragonContacts([]Player{p}, nil, nil); len(events) != 0 {
			t.Fatal("ineligible player contacted")
		}
	}
	cow := NewMob(TypeCow, 30, 8, 128, 0, 2)
	cow.Age = 20
	if events = m.dragonContacts(nil, []*Mob{m, cow}, nil); len(events) != 1 || events[0].Player.EntityID != cow.ID || events[0].Damage != 5 {
		t.Fatalf("mob contact=%+v", events)
	}
	m.AimDragonPart(DragonHead)
	m.TakeDamage(1, Player{EntityID: 20})
	if events = m.dragonContacts([]Player{p}, []*Mob{cow}, nil); len(events) != 0 {
		t.Fatal("hurt dragon attacked")
	}
}

func TestDragonWallsAndSlowdown(t *testing.T) {
	for _, grief := range []bool{false, true} {
		w := carryWorld{testWorld: newTestWorld(), noGriefing: !grief}
		m := NewMob(TypeEnderDragon, 1, 0, 70, 0, 1)
		m.SetDragonPhase(DragonHolding, 0)
		m.dragonParts()
		// Disjoint head, neck and body boxes also check the inclusive maximum cell.
		for i := range 3 {
			m.dragon.parts[i] = world.Box{MinX: float64(i*10) + 0.2, MinY: 70.2, MinZ: 0.2, MaxX: float64(i*10 + 1), MaxY: 70.8, MaxZ: 0.8}
		}
		w.set(0, 70, 0, defaultState(t, "minecraft:stone"))
		w.set(1, 70, 0, defaultState(t, "minecraft:fire"))
		w.set(10, 70, 0, defaultState(t, "minecraft:oak_planks"))
		w.set(20, 70, 0, defaultState(t, "minecraft:end_stone"))
		events := m.dragonWalls(w, nil)
		removed, particles := 0, 0
		for _, ev := range events {
			switch ev.Kind {
			case MobRemovedBlock:
				removed++
				if ev.X != 0 && ev.X != 10 {
					t.Fatalf("removed immune/transparent cell: %+v", ev)
				}
			case MobLevelEvent:
				particles++
				if ev.Status != 2008 {
					t.Fatalf("event=%d", ev.Status)
				}
			}
		}
		want := 0
		if grief {
			want = 2
		}
		if removed != want || particles != want || !m.dragon.inWall {
			t.Fatalf("grief=%v removed=%d particles=%d wall=%v", grief, removed, particles, m.dragon.inWall)
		}
	}
	w := griefingWorld{newTestWorld()}
	m := NewMob(TypeEnderDragon, 1, 0, 70, 0, 1)
	m.SetDragonPhase(DragonHolding, 0)
	for i := range 3 {
		m.dragon.parts[i] = world.Box{MinX: 0, MinY: 70, MinZ: 0, MaxX: 0.9, MaxY: 70.9, MaxZ: 0.9}
	}
	w.set(0, 70, 0, defaultState(t, "minecraft:stone"))
	if ev := m.dragonWalls(w, nil); len(ev) != 2 {
		t.Fatalf("overlapping boxes repeated removals/events: %+v", ev)
	}
	for _, name := range []string{"minecraft:water", "minecraft:fire"} {
		w.set(0, 70, 0, defaultState(t, name))
		if ev := m.dragonWalls(w, nil); len(ev) != 0 || m.dragon.inWall {
			t.Fatalf("%s blocked or destroyed: %+v", name, ev)
		}
	}

	clear, blocked := NewMob(TypeEnderDragon, 1, 0, 128, 0, 1), NewMob(TypeEnderDragon, 1, 0, 128, 0, 1)
	clear.SetDragonPhase(DragonHolding, 0)
	blocked.SetDragonPhase(DragonHolding, 0)
	blocked.dragon.inWall = true
	clear.dragonStep(newTestWorld())
	blocked.dragonStep(newTestWorld())
	if math.Abs(blocked.Z-clear.Z*0.8) > 1e-10 {
		t.Fatalf("wall movement=%v, clear=%v", blocked.Z, clear.Z)
	}
}

func TestDragonKillingBlowFliesToPodiumThenDies(t *testing.T) {
	w := motionWorld{testWorld: newTestWorld(), motion: 64}
	m := NewMob(TypeEnderDragon, 1, 0.5, 110, 0.5, 1)
	m.SetDragonPhase(DragonHolding, 0)
	m.Health = 1
	m.AimDragonPart(DragonHead)
	m.TakeDamage(8, Player{EntityID: 7})
	if m.Dead || m.Health != 1 || m.DragonPhase() != dragonDying {
		t.Fatalf("killing blow: dead=%v health=%v phase=%v", m.Dead, m.Health, m.DragonPhase())
	}
	m.Tick(w, nil)
	if m.Health != 1 || m.dragon.deathTime != 0 {
		t.Fatal("death animation started before reaching podium")
	}
	m.X, m.Y, m.Z = 0.5, 70, 0.5
	m.Tick(w, nil)
	if m.Health != 0 {
		t.Fatal("did not die at podium")
	}
	for tick := 1; tick <= 200; tick++ {
		if tick == 50 {
			tag := nbt.Compound{}
			m.SaveTo(tag, func() int64 { return 0 })
			restored := NewMob(TypeEnderDragon, 1, m.X, m.Y, m.Z, 1)
			restored.LoadFrom(tag, w, func() int64 { return 0 })
			if restored.Health != 0 || restored.dragon.deathTime != 49 {
				t.Fatal("death animation did not survive a save")
			}
			m = restored
		}
		events := m.Tick(w, nil)
		if len(events) == 0 || events[0].Kind != MobDragonDeath || int(events[0].Status) != tick {
			t.Fatalf("death tick %d: %v", tick, events)
		}
		if tick == 200 && events[len(events)-1].Kind != MobVanished {
			t.Fatal("dragon not removed at 200")
		}
	}
}

// vanilla: AbstractDragonSittingPhase.onHurt, EnderDragon.hurt
func TestASittingDragonReflectsShotsAndTakesOffAfterFiftyDamage(t *testing.T) {
	m := NewMob(TypeEnderDragon, 1, 0, 80, 0, 1)
	m.SetDragonPhase(DragonHolding, 0)
	m.SetDragonPhase(DragonSittingFlaming, 0)
	m.AimDragonPart(DragonHead)
	if landed, _ := m.HurtByArrow(40, 1, 0, 3, 20, true, false); landed {
		t.Fatal("sitting dragon accepted an arrow")
	}
	if landed, _ := m.HurtByTridentFrom(40, 1, 0, 3, 20, true, false, nil); landed {
		t.Fatal("sitting dragon accepted a trident")
	}
	if landed, _ := m.HurtByWindCharge(40, 1, 0, 20, true, false); landed {
		t.Fatal("sitting dragon accepted a wind charge")
	}
	source := Player{EntityID: 20, X: 1, Y: 80}
	m.TakeDamage(50, source)
	if !m.DragonSitting() || m.Health != 150 {
		t.Fatalf("at threshold: phase=%d health=%v", m.DragonPhase(), m.Health)
	}
	m.InvulnTicks = 0
	m.TakeDamage(1, source)
	if m.DragonPhase() != DragonTakeoff || m.dragon.sittingDamage != 0 {
		t.Fatal("more than fifty damage did not force takeoff")
	}
	if m.VX != 0 || m.VY != 0 || m.VZ != 0 {
		t.Fatal("damage knocked the dragon back")
	}
}

// vanilla: DragonSittingScanningPhase, DragonSittingAttackingPhase, DragonSittingFlamingPhase
func TestADragonRunsFourSittingFlamesBeforeTakeoff(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeEnderDragon, 1, 0, 80, 0, 1)
	m.SetDragonPhase(DragonHolding, 0)
	players := []Player{{EntityID: 20, X: 0, Y: 80, Z: -15}}
	m.SetDragonPhase(DragonSittingScanning, 0)
	for flame := 1; flame <= 4; flame++ {
		for range 26 {
			m.dragonPhaseTick(w, players, nil)
		}
		if m.DragonPhase() != DragonSittingAttacking {
			t.Fatalf("scan ended in phase %d", m.DragonPhase())
		}
		for range 41 {
			m.dragonPhaseTick(w, players, nil)
		}
		if m.DragonPhase() != DragonSittingFlaming {
			t.Fatalf("roar ended in phase %d", m.DragonPhase())
		}
		for tick := 1; tick <= 200; tick++ {
			events := m.dragonPhaseTick(w, players, nil)
			cloud := false
			for _, ev := range events {
				cloud = cloud || ev.Kind == MobDragonFlame
			}
			if cloud != (tick == 10) {
				t.Fatalf("cloud at flame tick %d = %v", tick, cloud)
			}
		}
		want := DragonSittingScanning
		if flame == 4 {
			want = DragonTakeoff
		}
		if m.DragonPhase() != want {
			t.Fatalf("after flame %d: phase %d, want %d", flame, m.DragonPhase(), want)
		}
	}
}
