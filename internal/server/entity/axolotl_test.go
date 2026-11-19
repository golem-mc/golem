package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestAxolotlVariantsAndPackBabies(t *testing.T) {
	seen := map[int]bool{}
	group := &SpawnGroup{}
	for i := range 100 {
		m := NewMob(TypeAxolotl, int32(i+1), 0, 64, 0, int64(i))
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:lush_caves"), group, rules.LocalDifficulty{})
		if m.AxolotlVariant() == AxolotlBlue {
			t.Fatal("blue spawned without breeding")
		}
		if m.IsBaby() != (i >= 2) {
			t.Fatalf("pack member %d baby=%v", i, m.IsBaby())
		}
		seen[m.AxolotlVariant()] = true
	}
	if len(seen) > 2 {
		t.Fatalf("pack has %d variants, want at most two", len(seen))
	}
	m := NewMob(TypeAxolotl, 1, 0, 64, 0, 1)
	for _, v := range []int{-1, 5, 100} {
		m.SetAxolotlVariant(v)
		if m.AxolotlVariant() != AxolotlLucy {
			t.Fatalf("unknown variant %d did not fall back to lucy", v)
		}
	}
	m.SetAxolotlVariant(AxolotlBlue)
	m.FromBucket = true
	saved := nbt.Compound{}
	m.SaveTo(saved, nil)
	back := NewMob(TypeAxolotl, 2, 0, 64, 0, 2)
	back.LoadFrom(saved, nil, nil)
	if back.AxolotlVariant() != AxolotlBlue || !back.FromBucket {
		t.Fatal("save lost variant or bucket origin")
	}
}

func TestAxolotlBreedingInheritsOrRollsBlue(t *testing.T) {
	m := NewMob(TypeAxolotl, 1, 0, 64, 0, 42)
	partner := NewMob(TypeAxolotl, 2, 0, 64, 0, 43)
	m.SetAxolotlVariant(AxolotlGold)
	partner.SetAxolotlVariant(AxolotlCyan)
	baby := NewMob(TypeAxolotl, 3, 0, 64, 0, 44)
	counts := map[int]int{}
	for range 12000 {
		m.BreedInto(baby, partner)
		if !baby.Persistence {
			t.Fatal("bred axolotl is not persistent")
		}
		counts[baby.AxolotlVariant()]++
	}
	if len(counts) != 3 || counts[AxolotlBlue] < 1 || counts[AxolotlBlue] > 40 || counts[AxolotlGold] < 5000 || counts[AxolotlCyan] < 5000 {
		t.Fatalf("12000 offspring variants: %v", counts)
	}
}

func TestAxolotlAirDryOutAndRehydrate(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeAxolotl, 1, 0, 64, 0, 1)
	if m.AirSupply != 6000 {
		t.Fatalf("initial air=%d", m.AirSupply)
	}
	m.AirSupply = 0
	m.waterFluid.height = 0.1
	m.axolotlAirStep(w, nil)
	m.waterFluid.height = 0
	if m.AirSupply != 6000 {
		t.Fatal("water did not refill full air")
	}
	for range 6019 {
		if ev := m.axolotlAirStep(w, nil); len(ev) != 0 {
			t.Fatal("dried out before air reached -20")
		}
	}
	ev := m.axolotlAirStep(w, nil)
	if len(ev) != 1 || ev[0].DamageType != registry.DamageTypeDryOut || !ev[0].NoBubble || m.Health != 12 || m.AirSupply != 0 {
		t.Fatalf("dry out: hp=%v air=%d events=%+v", m.Health, m.AirSupply, ev)
	}
	m.Rehydrate()
	if m.AirSupply != 1800 {
		t.Fatalf("rehydrated air=%d", m.AirSupply)
	}
	m.AirSupply = 5900
	m.Rehydrate()
	if m.AirSupply != 6000 {
		t.Fatal("rehydration exceeded cap")
	}
	m.AirSupply = 0
	m.waterFluid.height = 0.1
	m.axolotlAirStep(w, nil)
	if m.AirSupply != 6000 {
		t.Fatal("water did not replenish air")
	}
	m.NoAI = true
	m.AirSupply = 12
	m.waterFluid.height = 0
	m.axolotlAirStep(w, nil)
	if m.AirSupply != 12 {
		t.Fatal("NoAI spent air")
	}
}

func TestAxolotlPlayingDeadRegeneratesAndReleasesGuardianTarget(t *testing.T) {
	w := tank(t, rules.DifficultyNormal)
	m := NewMob(TypeAxolotl, 2, 5.5, 61, 0.5, 2)
	m.Health = 6
	m.updateMobFluids(w)
	for range 100 {
		m.noteAxolotlHurt(2, true)
		if m.axolotl.playDead > 0 {
			break
		}
	}
	if m.axolotl.playDead != 200 {
		t.Fatal("never rolled playing dead")
	}
	m.setRetaliate(42)
	m.noteLastDamage(rules.DamageTypePlayerAttack)
	m.axolotlBrainStep()
	if !m.PlayingDead() || len(m.AllEffects()) == 0 {
		t.Fatal("playing dead did not grant regeneration")
	}
	g := NewMob(TypeGuardian, 1, 0.5, 61, 0.5, 1)
	g.TargetID = m.ID
	goal := &guardianTargetGoal{m: g}
	view := &tickView{w: w, others: []*Mob{g, m}}
	if goal.ShouldContinue(view) {
		t.Fatal("guardian kept a playing-dead target")
	}
	for range 199 {
		m.axolotlBrainStep()
	}
	if m.PlayingDead() {
		t.Fatal("still playing dead after 200 ticks")
	}
	if !goal.ShouldContinue(view) {
		t.Fatal("guardian cannot target recovered axolotl")
	}
	for range 100 {
		g.TargetID = 0
		if goal.CanStart(view) {
			break
		}
	}
	if g.TargetID != m.ID {
		t.Fatal("guardian never acquired axolotl")
	}
	m.axolotl.playDead = 0
	for range 100 {
		m.noteAxolotlHurt(m.Health, true)
	}
	if m.axolotl.playDead != 0 {
		t.Fatal("lethal blow triggered playing dead")
	}
}

func TestAxolotlHuntsWaterTargetsAndKeepsBucketCooldown(t *testing.T) {
	w := tank(t, rules.DifficultyNormal)
	m := NewMob(TypeAxolotl, 1, 0.5, 61, 0.5, 1)
	prey := NewMob(TypeCod, 2, 1.3, 61, 0.5, 2)
	prey.updateMobFluids(w)
	goal := &axolotlHuntGoal{m: m}
	view := &tickView{w: w, others: []*Mob{m, prey}}
	m.axolotlSense(view)
	if !goal.CanStart(view) {
		t.Fatal("cod in water was not hunted")
	}
	goal.Start(view)
	goal.Tick(view)
	if len(view.events) != 2 || view.events[0].Kind != MobAttackedPlayer || view.events[0].Player.EntityID != prey.ID || view.events[0].Damage != 2 {
		t.Fatalf("hunt did not emit melee attack: %+v", view.events)
	}
	w.set(1, 61, 0, registry.BlockStone.DefaultState())
	m.axolotl.attackCooldown = 0
	view.events = nil
	goal.Tick(view)
	if len(view.events) != 0 {
		t.Fatal("melee attack crossed a wall")
	}
	w.set(1, 61, 0, registry.BlockWater.DefaultState())
	goal.Stop(view)
	m.axolotl.senseIn = 0
	m.axolotlSense(view)
	if goal.CanStart(view) {
		t.Fatal("hunted cod during hunting cooldown")
	}
	hostile := NewMob(TypeGuardian, 3, 3.5, 61, 0.5, 3)
	hostile.updateMobFluids(w)
	view.others = append(view.others, hostile)
	m.axolotl.senseIn = 0
	m.axolotlSense(view)
	if !goal.CanStart(view) || goal.target != hostile {
		t.Fatal("cooldown suppressed always-hostile target")
	}
	m.BabyAge = -1234
	m.AgeLocked = true
	m.Health = 8
	saved := nbt.Compound{}
	m.SaveTo(saved, nil)
	loaded := NewMob(TypeAxolotl, 5, 0, 64, 0, 5)
	loaded.LoadFrom(saved, nil, nil)
	if loaded.axolotl.huntingCooldown != 2400 {
		t.Fatal("world save lost hunting cooldown")
	}
	back := NewMob(TypeAxolotl, 4, 0, 64, 0, 4)
	back.LoadBucketTag(m.BucketTag())
	if back.BabyAge != -1234 || !back.AgeLocked || back.Health != 8 || back.axolotl.huntingCooldown != 2400 || !back.FromBucket {
		t.Fatalf("bucket lost state: age=%d health=%v cooldown=%d", back.BabyAge, back.Health, back.axolotl.huntingCooldown)
	}
	if back.removeWhenFarAway() || !back.requiresCustomPersistence() {
		t.Fatal("bucketed axolotl can despawn")
	}
	prey.waterFluid.height = 0
	view.others = []*Mob{prey}
	m.axolotl.senseIn = 0
	m.axolotlSense(view)
	if goal.CanStart(view) {
		t.Fatal("hunted cod out of water")
	}
}

func TestAxolotlSwimsWithoutSinkingOrFluidPush(t *testing.T) {
	w := tank(t, rules.DifficultyNormal)
	m := NewMob(TypeAxolotl, 1, 0.5, 61, 0.5, 1)
	m.updateMobFluids(w)
	if !m.seekTo(w, 4.5, 62, 0.5, 0.5, navReach) {
		t.Fatal("no amphibious path through water")
	}
	startX := m.X
	for range 30 {
		m.updateMobFluids(w)
		m.axolotlMoveControl(w)
		m.axolotlTravelInWater(w)
	}
	if m.X <= startX+0.2 || m.Y <= 61 {
		t.Fatalf("did not swim up path: (%v,%v,%v)", m.X, m.Y, m.Z)
	}
	m.stopSeeking()
	m.VX, m.VY, m.VZ = 0, 0, 0
	y := m.Y
	m.axolotlMoveControl(w)
	m.axolotlTravelInWater(w)
	if math.Abs(m.Y-y) > 1e-9 {
		t.Fatal("idle axolotl sank")
	}
}

func TestAxolotlSupportRequiresTheNearbyKillingPlayer(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		x                         float64
		worldDamage, alive, stale bool
		want                      bool
	}{
		{name: "near", x: 20, want: true},
		{name: "bounding boxes overlap", x: 20.6, want: true},
		{name: "outside", x: 21},
		{name: "world final blow", worldDamage: true},
		{name: "survived", alive: true},
		{name: "stale source", stale: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMob(TypeAxolotl, 1, 0, 61, 0, 1)
			o := NewMob(TypeCod, 2, 1, 61, 0, 2)
			p := Player{EntityID: 42, X: tc.x, Y: 61}
			damage := 100.0
			if tc.alive {
				damage = 1
			}
			o.takeAttack(damage, rules.DamageTypePlayerAttack, p, 0, false)
			if tc.worldDamage {
				o.noteLastDamage(registry.DamageTypeDrown)
			}
			if tc.stale {
				o.Age += lastDamageExpiry + 1
			}
			g := &axolotlHuntGoal{m: m, target: o}
			view := &tickView{players: []Player{p}}
			g.Stop(view)
			if got := len(view.events) == 1; got != tc.want {
				t.Fatalf("support=%v, want %v: %+v", got, tc.want, view.events)
			}
		})
	}
}

func TestAxolotlBrainCountdownsSurviveReload(t *testing.T) {
	m := NewMob(TypeAxolotl, 1, 0, 61, 0, 1)
	m.axolotl.playDead = 150
	m.axolotl.temptationCooldown = 75
	m.axolotl.huntingCooldown = 1234
	tag := nbt.Compound{}
	m.SaveTo(tag, nil)
	back := NewMob(TypeAxolotl, 2, 0, 61, 0, 2)
	back.LoadFrom(tag, nil, nil)
	back.axolotlBrainStep()
	if back.axolotl.playDead != 149 || back.axolotl.temptationCooldown != 74 || back.axolotl.huntingCooldown != 1233 {
		t.Fatalf("loaded countdowns: %+v", back.axolotl)
	}
	for _, goal := range back.ai.goals {
		if gated, ok := goal.(*gatedGoal); ok {
			choice, ok := gated.Goal.(*runOneGoal)
			if !ok {
				continue
			}
			g, ok := choice.choices[0].(*temptGoal)
			if !ok {
				continue
			}
			for range 10 {
				if g.CanStart(&tickView{}) {
					t.Fatal("temptation started during its saved cooldown")
				}
			}
			if back.axolotl.temptationCooldown != 74 {
				t.Fatal("goal selection consumed brain cooldown ticks")
			}
			g.Stop(nil)
			if back.axolotl.temptationCooldown != 100 {
				t.Fatal("stopping temptation did not set its brain memory")
			}
		}
	}
}

func TestAxolotlBrainActivitiesAndSensors(t *testing.T) {
	w := tank(t, rules.DifficultyNormal)
	m := NewMob(TypeAxolotl, 1, 0.5, 61, 0.5, 1)
	m.BabyAge = -100
	m.Age = 4 // An alternate GoalSelector tick must still select brain behaviors.
	m.updateMobFluids(w)
	prey := NewMob(TypeCod, 2, 3.5, 61, 0.5, 2)
	prey.updateMobFluids(w)
	adult := NewMob(TypeAxolotl, 3, 7.5, 61, 0.5, 3)
	food, _ := registry.ItemID("minecraft:tropical_fish_bucket")
	view := &tickView{w: w, others: []*Mob{m, prey, adult}, players: []Player{{EntityID: 42, X: 2.5, Y: 61, Z: 0.5, HeldItem: protocol.ItemStack{ItemID: food, Count: 1}}}}
	m.axolotlSense(view)
	if m.axolotl.attackable != prey || m.axolotl.adult != adult || m.axolotl.tempting != 42 {
		t.Fatalf("sensor memories: %+v", m.axolotl)
	}
	for _, g := range m.ai.goals {
		if gate, ok := g.(*gatedGoal); ok {
			if choice, ok := gate.Goal.(*runOneGoal); ok {
				seen := map[Goal]bool{}
				for range 20 {
					if choice.CanStart(view) {
						seen[choice.chosen] = true
					}
				}
				if len(seen) != 2 {
					t.Fatal("RunOne did not offer both temptation and following")
				}
			}
		}
	}
	m.ai.run(m, view)
	if m.TargetID != prey.ID {
		t.Fatal("temptation or parent following prevented the fight activity")
	}
	for _, g := range m.ai.running {
		if _, ok := g.(*gatedGoal); ok {
			t.Fatal("idle behavior ran during fight")
		}
	}
	prey.waterFluid.height = 0
	for range 19 {
		m.axolotlSense(view)
	}
	if m.axolotl.attackable != prey {
		t.Fatal("sensor refreshed before twenty ticks")
	}
	m.axolotlSense(view)
	if m.axolotl.attackable != nil {
		t.Fatal("sensor failed to refresh during fight")
	}
	// Leaving water alone does not erase ATTACK_TARGET.
	for _, g := range m.ai.running {
		if hunt, ok := g.(*axolotlHuntGoal); ok {
			if !hunt.ShouldContinue(view) {
				t.Fatal("pursuit stopped when prey left water")
			}
			hunt.unreachable = 201
			if hunt.ShouldContinue(view) {
				t.Fatal("pursuit ignored its unreachable timeout")
			}
		}
	}
	m.setRetaliate(42)
	m.noteLastDamage(rules.DamageTypePlayerAttack)
	m.axolotl.playDead = 100
	m.axolotlBrainStep()
	m.ai.run(m, view)
	if m.TargetID != 0 || m.axolotl.huntingCooldown != 2400 || !m.PlayingDead() {
		t.Fatalf("fight did not yield to playing dead: %+v", m.axolotl)
	}
	prey.updateMobFluids(w)
	m.axolotl.playDead, m.axolotl.huntingCooldown, m.axolotl.senseIn = 0, 1, 0
	herd := Mobs{m, prey}
	herd.Tick(w, nil, nil)
	if m.TargetID != prey.ID {
		t.Fatal("expiring hunting cooldown missed the sensor pulse")
	}
}

func TestAxolotlIdleWaterAndSwimming(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeAxolotl, 1, 0.5, 61, 0.5, 1)
	water := registry.BlockWater.DefaultState()
	w.set(1, 61, 0, water)
	w.set(1, 62, 0, registry.BlockStone.DefaultState())
	w.set(5, 61, 0, water)
	if p, ok := axolotlWater(w, m); !ok || p != (Waypoint{5, 61, 0}) {
		t.Fatalf("surface water was not preferred: %v (%v)", p, ok)
	}
	w.set(5, 61, 0, 0)
	if _, ok := axolotlWater(w, m); ok {
		t.Fatal("submerged fallback inside 1.5 blocks was selected")
	}
	w.set(3, 61, 0, water)
	w.set(3, 62, 0, registry.BlockStone.DefaultState())
	if p, ok := axolotlWater(w, m); !ok || p != (Waypoint{3, 61, 0}) {
		t.Fatalf("submerged fallback: %v (%v)", p, ok)
	}
	pool := tank(t, rules.DifficultyNormal)
	found := false
	for range 20 {
		p, ok := axolotlSwimPos(pool, m)
		if ok {
			found = true
			if !rules.IsWater(pool.BlockAt(floorI(p[0]), floorI(p[1]), floorI(p[2]))) {
				t.Fatal("swim destination is dry")
			}
		}
	}
	if !found {
		t.Fatal("ordered swim behavior never found a water destination")
	}
	m.axolotl.playDead = 100
	m.axolotlBrainStep()
	if len(m.AllEffects()) != 0 {
		t.Fatal("restored play-dead memory granted regeneration without HURT_BY_ENTITY")
	}
}
