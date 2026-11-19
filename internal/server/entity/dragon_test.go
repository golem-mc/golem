package entity

import (
	"fmt"
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

// The window weighs what is left of a blow after the part's reduction: a
// blast of 8 on the body is 3.
// vanilla: EnderDragon.reallyHurt, LivingEntity.hurtServer
func TestDragonHurtWindowRefusesARepeatHit(t *testing.T) {
	m := NewMob(TypeEnderDragon, 1, 0, 128, 0, 1)
	m.SetDragonPhase(DragonHolding, 0)
	m.NoAI = true
	w := newTestWorld()
	blast := func(amount float64) { m.HurtByBlast(amount, 0, 0, 0, registry.DamageTypeExplosion, false) }
	blast(8)
	blast(8)
	if m.Health != 197 {
		t.Fatalf("two blasts in one tick left health=%v, want 197", m.Health)
	}
	if !m.TakeDamage(8, Player{EntityID: 7, X: 5, Y: 128}) || m.Health != 197 {
		t.Fatalf("a player's accepted blow in the blast's tick changed health: health=%v", m.Health)
	}
	for range 9 {
		m.Tick(w, nil)
	}
	if blast(8); m.Health != 197 {
		t.Fatalf("a blast 9 ticks after the first landed: health=%v", m.Health)
	}
	// A larger blow inside the window lands its excess: 16/4+1 = 5, less 3.
	if blast(16); m.Health != 195 {
		t.Fatalf("a larger blast inside the window left health=%v, want 195", m.Health)
	}
	m.Tick(w, nil)
	if blast(8); m.Health != 192 {
		t.Fatalf("a blast 10 ticks after the first left health=%v, want 192", m.Health)
	}
	// The crystal's blast on the head goes through the same window.
	m.dragon.crystal = 20
	m.DragonCrystalDestroyed(20, 0, 10, &Player{EntityID: 7})
	if m.Health != 185 {
		t.Fatalf("crystal inside the window left health=%v, want 185, the excess of 10 over 3", m.Health)
	}
}

func TestDragonKillingBlowDefersRemoval(t *testing.T) {
	for _, softened := range []bool{false, true} {
		t.Run(fmt.Sprint(softened), func(t *testing.T) {
			m := NewMob(TypeEnderDragon, 1, 0, 128, 0, 1)
			m.SetDragonPhase(DragonHolding, 0)
			m.NoAI = true
			m.Health = 4
			if softened {
				m.HurtByCommand(8, rules.DamageTypePlayerAttack, 7, true, false, 5, 0)
			}
			landed, soft := m.HurtByCommand(2000, rules.DamageTypePlayerAttack, 7, true, false, 5, 0)
			if !landed || soft != softened || m.Health != 1 || m.Dead || !m.DragonDying() {
				t.Fatalf("landed=%v softened=%v health=%v dead=%v phase=%v", landed, soft, m.Health, m.Dead, m.DragonPhase())
			}
			events := m.Tick(newTestWorld(), nil)
			if len(events) != 1 || events[0].Kind != MobKillingBlow || events[0].Softened != softened || events[0].DamageType != rules.DamageTypePlayerAttack {
				t.Fatalf("killing blow events=%+v", events)
			}
			if !m.PlayerKillCredit() || !m.HurtByPlayer() || m.PlayerCreditID() != 7 || m.AttackerID() != 7 {
				t.Fatal("killing blow lost its player credit")
			}
			if events := m.Tick(newTestWorld(), nil); len(events) != 0 {
				t.Fatalf("killing blow repeated: %+v", events)
			}
		})
	}
}

func TestDragonHoldingFlightAndParts(t *testing.T) {
	m := NewMob(TypeEnderDragon, 1, 0, 128, 0, 1)
	m.SetDragonPhase(DragonHolding, 0)
	w := newTestWorld()
	// Walk through several graph paths; no walk physics may pull this flyer
	// onto the floor, and every part must keep its own vanilla dimensions.
	for range 3000 {
		m.Tick(w, nil)
		if math.IsNaN(m.X) || math.IsNaN(m.Y) || math.Abs(m.X) > 150 || math.Abs(m.Z) > 150 || m.Y < 50 || m.Y > 150 {
			t.Fatalf("flight escaped arena: %v,%v,%v", m.X, m.Y, m.Z)
		}
	}
	if m.X == 0 && m.Z == 0 {
		t.Fatal("holding dragon never moved")
	}
	sizes := [][2]float64{{1, 1}, {3, 3}, {5, 3}, {2, 2}, {2, 2}, {2, 2}, {4, 2}, {4, 2}}
	for i, size := range sizes {
		b := m.DragonPartBox(i)
		if math.Abs(b.MaxX-b.MinX-size[0]) > 1e-10 || math.Abs(b.MaxY-b.MinY-size[1]) > 1e-10 {
			t.Errorf("part %d box=%+v", i, b)
		}
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

func TestDragonCrystalFlightNodes(t *testing.T) {
	m := NewMob(TypeEnderDragon, 1, 60, 80, 0, 1)
	m.SetDragonPhase(DragonHolding, 0)
	m.dragonNodes(newTestWorld())
	for _, count := range []int{0, 10} {
		m.SetDragonCrystalCount(count)
		from := m.dragonClosestNode()
		if count == 0 && from < 12 || count > 0 && from >= 12 {
			t.Fatalf("count=%d closest=%d", count, from)
		}
		path := dragonPath(m.dragon.nodes, from, 1, m.dragonMinimumNode())
		if len(path) < 2 {
			t.Fatalf("count=%d path=%v", count, path)
		}
		for _, n := range path {
			if count == 0 && n < 12 {
				t.Fatalf("zero crystals used outer node: %v", path)
			}
		}
		if count > 0 && path[len(path)-1] != 1 {
			t.Fatalf("outer target not reached: %v", path)
		}
	}
}

func TestDragonProjectilePartSweep(t *testing.T) {
	m := NewMob(TypeEnderDragon, 10, 0, 128, 0, 1)
	m.SetDragonPhase(DragonHolding, 0)
	m.dragonParts()
	typ := int32(TypeEnderDragon)
	targets := MobProjectileTargets(nil, m, typ, 0)
	if len(targets) != 9 || !targets[0].Unhittable {
		t.Fatal("missing parts or pickable parent")
	}
	for _, target := range targets {
		if target.Type != typ {
			t.Fatalf("projectile target type = %d, want %d", target.Type, typ)
		}
	}
	for _, part := range []int{DragonHead, DragonBody, 6} {
		p := DragonPartTarget(m, typ, part)
		x, y, z := (p.MinX+p.MaxX)/2, p.MinY+0.5, (p.MinZ+p.MaxZ)/2
		// A vertical shot reaches a wing outside the parent's box, and distinguishes
		// the head from the broad parent that would otherwise intercept it.
		a := NewArrow(30, x, y+10, z, 0, -12, 0, false, 2, PickupAllowed, 40, 1)
		if part == DragonHead {
			a = NewArrow(30, x, y, z-10, 0, 0, 12, false, 2, PickupAllowed, 40, 1)
		}
		var hit *ArrowEvent
		for _, ev := range a.Step(newTestWorld(), nil, targets) {
			if ev.Kind == ArrowHitTarget {
				copy := ev
				hit = &copy
			}
		}
		if hit == nil || hit.Target.EntityID != m.ID+1+int32(part) {
			t.Fatalf("part=%d hit=%+v", part, hit)
		}
		m.Health, m.InvulnTicks = 200, 0
		m.AimDragonPart(part)
		m.HurtByArrow(hit.Damage, 0, 0, 0, 40, true, false)
		want := hit.Damage/4 + math.Min(hit.Damage, 1)
		if part == DragonHead {
			want = hit.Damage
		}
		if m.Health != 200-want {
			t.Fatalf("part=%d health=%v damage=%v", part, m.Health, hit.Damage)
		}
	}
}

func TestDragonAcceptedHurtsAndCooldownBypass(t *testing.T) {
	m := NewMob(TypeEnderDragon, 1, 0, 128, 0, 1)
	m.SetDragonPhase(DragonHolding, 0)
	if !m.HurtByMob(8, NewMob(TypeZombie, 2, 0, 128, 0, 1)) || m.Health != 200 {
		t.Fatal("ignored mob attack was not accepted")
	}
	if landed, _ := m.HurtByWorld(8, rules.DamageTypeCactus); !landed || m.Health != 200 {
		t.Fatal("ignored world attack was not accepted")
	}
	m.AimDragonPart(DragonHead)
	m.TakeDamage(8, Player{EntityID: 3})
	if !m.TakeDamage(8, Player{EntityID: 3}) || m.Health != 192 {
		t.Fatal("window should accept without dealing damage")
	}
	// The built-in cooldown tag is empty; a data pack can opt explosions in.
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	registry.ApplyPackTags(registry.PackTags{"minecraft:damage_type": {"minecraft:bypasses_cooldown": {Entries: []registry.PackTagEntry{{ID: "minecraft:explosion", Required: true}}}}})
	m.hurtByDamage(8, registry.DamageTypeExplosion, 2, false, false, 0, 0)
	if m.Health != 184 {
		t.Fatalf("cooldown bypass health=%v", m.Health)
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

// vanilla: DragonSittingScanningPhase uses combat targeting with line of sight.
func TestADragonOnlyScansPlayersInSight(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeEnderDragon, 1, 0, 80, 0, 1)
	m.SetDragonPhase(DragonHolding, 0)
	m.SetDragonPhase(DragonSittingScanning, 0)
	players := []Player{{EntityID: 20, X: 0, Y: 80, Z: -15}}
	for y := int32(80); y <= 88; y++ {
		w.set(0, y, -7, registry.BlockStone.DefaultState())
	}
	for range 26 {
		m.dragonPhaseTick(w, players, nil)
	}
	if m.DragonPhase() != DragonSittingScanning {
		t.Fatal("dragon scanned a player through a wall")
	}
	for y := int32(80); y <= 88; y++ {
		w.set(0, y, -7, registry.BlockAir.DefaultState())
	}
	m.dragonPhaseTick(w, players, nil)
	if m.DragonPhase() != DragonSittingAttacking {
		t.Fatal("dragon did not attack the visible player")
	}
}

// A dragon that is not the fight's starts hovering where it was summoned.
// vanilla: EnderDragonPhaseManager.<init>, DragonHoverPhase
func TestAFreshDragonHoversInPlace(t *testing.T) {
	m := NewMob(TypeEnderDragon, 1, 30, 100, 30, 1)
	if m.DragonPhase() != DragonHovering {
		t.Fatalf("phase=%d", m.DragonPhase())
	}
	w := newTestWorld()
	for range 200 {
		m.dragonTick(w, nil, nil, nil)
	}
	if math.Abs(m.X-30) > 8 || math.Abs(m.Y-100) > 3 || math.Abs(m.Z-30) > 8 || m.DragonPhase() != DragonHovering {
		t.Fatalf("drifted to %v,%v,%v phase %d", m.X, m.Y, m.Z, m.DragonPhase())
	}
	m.AimDragonPart(DragonHead)
	m.TakeDamage(60, Player{EntityID: 7, X: 31, Y: 100})
	if m.DragonPhase() != DragonTakeoff {
		t.Fatalf("phase after damage=%d", m.DragonPhase())
	}
	k := NewMob(TypeEnderDragon, 2, 30, 100, 30, 1)
	k.AimDragonPart(DragonHead)
	k.TakeDamage(500, Player{EntityID: 7, X: 31, Y: 100})
	if k.Health > 0 || k.DragonPhase() == dragonDying {
		t.Fatalf("health=%v phase=%d", k.Health, k.DragonPhase())
	}
}

func TestDragonSittingHeadYOffset(t *testing.T) {
	for _, phase := range []int{DragonSittingScanning, DragonHovering} {
		m := NewMob(TypeEnderDragon, 1, 0, 128, 0, 1)
		m.SetDragonPhase(phase, 0)
		m.dragonParts()
		m.dragon.historyReady = true
		for i := range m.dragon.history {
			m.dragon.history[i] = [2]float64{128 + float64(i%7), 0}
		}
		m.dragonParts()
		p5, p10 := m.dragon.history[(m.dragon.historyIndex-5)&63], m.dragon.history[(m.dragon.historyIndex-10)&63]
		st := math.Sin((p5[0] - p10[0]) * 10 * math.Pi / 180)
		if got, want := m.dragon.parts[0].MinY, m.Y-1+st*6.5; math.Abs(got-want) > 1e-9 {
			t.Errorf("phase %v head MinY=%v, want %v", phase, got, want)
		}
		if got, want := m.dragon.parts[1].MinY, m.Y-1+st*5.5; math.Abs(got-want) > 1e-9 {
			t.Errorf("phase %v neck MinY=%v, want %v", phase, got, want)
		}
	}
}

func TestDragonPartsUnpositionedWithoutAI(t *testing.T) {
	m := NewMob(TypeEnderDragon, 1, 8, 128, 8, 1)
	m.NoAI = true
	m.Tick(nil, nil)
	if b := m.DragonPartBox(2); b.MinX != -2.5 || b.MaxX != 2.5 || b.MinY != 0 || b.MaxY != 3 {
		t.Fatalf("fresh NoAI body box=%+v", b)
	}
}
