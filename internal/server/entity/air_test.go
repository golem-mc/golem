package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

type airRollSource struct {
	value int64
	draws int
}

func (s *airRollSource) Seed(int64) {}
func (s *airRollSource) Int63() int64 {
	s.draws++
	return s.value
}

func TestMobOxygenBonusAirRoll(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		kind   MobType
		bonus  float64
		roll   int64
		helmet bool
		air    int
		draws  int
	}{
		{"zero bonus", TypePig, 0, 1 << 62, false, 99, 0},
		{"below threshold", TypePig, 3, 1 << 60, false, 99, 1},
		{"at threshold", TypePig, 3, 1 << 61, false, 100, 1},
		{"above threshold", TypePig, 3, 1 << 62, false, 100, 1},
		{"respiration drains", TypePig, 0, 1 << 60, true, 99, 1},
		{"respiration spares", TypePig, 0, 1 << 62, true, 100, 1},
		{"iron golem", TypeIronGolem, 0, 0, false, 100, 0},
		{"iron golem with bonus", TypeIronGolem, 3, 0, false, 100, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMob(tc.kind, 1, .5, 66, .5, 1)
			m.SetAttributes(m.BuildAttributes(nbt.List{}))
			m.attrs.SetBase(registry.AttributeOxygenBonus, tc.bonus)
			if tc.helmet {
				id, ok := registry.EnchantmentID("minecraft:respiration")
				if !ok {
					t.Fatal("missing respiration enchantment")
				}
				m.Worn[wornHead] = stackOf(t, "minecraft:diamond_helmet")
				m.Worn[wornHead].Enchantments = map[int32]int32{id: 3}
				m.SyncEquipmentModifiers(m.attrs)
				if got := m.attrOr(registry.AttributeOxygenBonus, 0); got != 3 {
					t.Fatalf("Respiration III oxygen bonus = %v, want 3", got)
				}
			}
			// Exact binary fractions put the roll below, at and above 1/4.
			source := &airRollSource{value: tc.roll}
			m.Rand = rand.New(source)
			m.waterFluid.eyesIn = true
			m.AirSupply = 100
			m.airStep(nil)
			if m.AirSupply != tc.air || source.draws != tc.draws {
				t.Fatalf("Air = %d, random draws = %d; want %d, %d", m.AirSupply, source.draws, tc.air, tc.draws)
			}
		})
	}
}

// TestAMobUnderWaterSpendsItsBreathAndDrowns runs the countdown: a cow's air
// falls a tick at a time with its eyes under water, nothing bites while a
// tick of breath is left, and the breath twenty ticks below nothing resets
// and drowns it for two half-hearts.
func TestAMobUnderWaterSpendsItsBreathAndDrowns(t *testing.T) {
	w, _ := pool(t)
	cow := aimless(NewMob(TypeCow, 1, 0.5, 66, 0.5, 1))
	// NoAI holds the body at the depth the pool's own water keeps it in, so
	// the eyes are under the surface every tick the countdown runs.
	cow.NoAI = true
	if cow.AirSupply != rules.MaxAirSupply {
		t.Fatalf("a fresh cow's breath = %d, want %d", cow.AirSupply, rules.MaxAirSupply)
	}

	cow.Tick(w, nil)
	if cow.AirSupply != rules.MaxAirSupply-1 {
		t.Fatalf("after one tick under water the breath = %d, want %d", cow.AirSupply, rules.MaxAirSupply-1)
	}

	// Down to a tick short of the floor: no bite yet.
	for range rules.MaxAirSupply - rules.AirDrownFloor - 2 {
		cow.Tick(w, nil)
	}
	if want := rules.AirDrownFloor + 1; cow.AirSupply != want {
		t.Fatalf("the breath at the floor = %d, want %d", cow.AirSupply, want)
	}
	if cow.Health != 10 {
		t.Fatalf("health a tick before the drowning = %v, want the whole ten", cow.Health)
	}

	events := cow.Tick(w, nil)
	if cow.AirSupply != 0 {
		t.Errorf("the drowned breath = %d, want it reset", cow.AirSupply)
	}
	if want := 10 - 2.0; !closeEnough(cow.Health, want) {
		t.Errorf("health at the drowning floor = %v, want %v", cow.Health, want)
	}
	if len(events) != 1 || events[0].Kind != MobBurned || events[0].DamageType != rules.DamageTypeDrown {
		t.Errorf("the drowning's events = %+v, want one drown bite", events)
	}
}

// TestASoftenedDrownIsMarkedSoftened checks a drown bite that lands inside a
// mob's window: the excess alone reaches the health and the bite is marked
// softened so the server can hold back the hurt telling, while the bubble
// burst it rides still goes out.
func TestASoftenedDrownIsMarkedSoftened(t *testing.T) {
	w, _ := pool(t)
	cow := aimless(NewMob(TypeCow, 1, 0.5, 66, 0.5, 1))
	cow.NoAI = true
	cow.HurtByBlast(1, 0, 0, 0, rules.DamageTypeExplosion, false)
	beforeVX, beforeVY := cow.VX, cow.VY
	cow.AirSupply = rules.AirDrownFloor + 1

	events := cow.Tick(w, nil)
	if want := 10.0 - 1 - 1; !closeEnough(cow.Health, want) {
		t.Errorf("the excess of one left %v health, want the %v", cow.Health, want)
	}
	if cow.VX != beforeVX || cow.VY != beforeVY {
		t.Errorf("a softened drown shoved to (%v, %v), want the velocity left at (%v, %v)",
			cow.VX, cow.VY, beforeVX, beforeVY)
	}
	var bite *MobEvent
	for i := range events {
		if events[i].Kind == MobBurned && events[i].DamageType == rules.DamageTypeDrown {
			bite = &events[i]
		}
	}
	if bite == nil {
		t.Fatal("the drowning sent no bite for the bubble burst to ride")
	}
	if !bite.Softened {
		t.Error("the softened drowning's bite was not marked softened")
	}
}

// TestAMobOutOfWaterBreathesBackUp checks the recovery: away from the water
// the air comes back four a tick and never past the full breath.
func TestAMobOutOfWaterBreathesBackUp(t *testing.T) {
	stone := defaultState(t, "minecraft:stone")
	w := newTestWorld()
	floor(w, -2, -2, 2, 2, 63, stone)

	cow := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	cow.NoAI = true
	cow.AirSupply = 100
	cow.Tick(w, nil)
	if cow.AirSupply != 104 {
		t.Errorf("a tick out of water breathed back to %d, want 104", cow.AirSupply)
	}

	cow.AirSupply = rules.MaxAirSupply - 2
	cow.Tick(w, nil)
	if cow.AirSupply != rules.MaxAirSupply {
		t.Errorf("the breath past the full = %d, want the %d cap", cow.AirSupply, rules.MaxAirSupply)
	}
}

// TestEveryUndeadMobKeepsItsBreathUnderWater checks canBreatheUnderwater on
// each undead kind here — the husk and drowned alongside the zombie, the
// stray and bogged alongside the skeleton: a breathing mob under water
// refills four a tick like out of it, never spends air, and takes no drown
// bite.
func TestEveryUndeadMobKeepsItsBreathUnderWater(t *testing.T) {
	kinds := []MobType{TypeZombie, TypeHusk, TypeDrowned, TypeSkeleton, TypeStray, TypeBogged}
	for _, kind := range kinds {
		w, _ := pool(t)
		m := aimless(NewMob(kind, 1, 0.5, 66, 0.5, 1))
		m.NoAI = true

		// A low breath climbs by the refill, not straight to the full.
		m.AirSupply = 100
		m.Tick(w, nil)
		if m.AirSupply != 104 {
			t.Errorf("%s with a low breath under water refilled to %d, want 104", kind, m.AirSupply)
		}

		m.AirSupply = rules.MaxAirSupply
		for range rules.MaxAirSupply + 40 {
			for _, ev := range m.Tick(w, nil) {
				if ev.Kind == MobBurned && ev.DamageType == rules.DamageTypeDrown {
					t.Fatalf("%s took a drown bite", kind)
				}
			}
		}
		if m.AirSupply != rules.MaxAirSupply {
			t.Errorf("%s's breath under water = %d, want the full %d", kind, m.AirSupply, rules.MaxAirSupply)
		}
		if m.Health != m.MaxHealth {
			t.Errorf("%s's health under water = %v, want the whole %v", kind, m.Health, m.MaxHealth)
		}
	}
}

// TestAMobWithWaterBreathingEffectsBreathesUnderWater checks the effect door
// on the breath: water_breathing and conduit_power spare the drain and let
// the air refill, while breath_of_the_nautilus alone spares the drain but
// freezes the air there (vanilla: MobEffectUtil.hasWaterBreathing and
// shouldEffectsRefillAirsupply).
func TestAMobWithWaterBreathingEffectsBreathesUnderWater(t *testing.T) {
	breath := func(name string) effect.Instance {
		return effect.New(registry.MustMobEffect(name), 6000, 0, false, true, true)
	}
	cases := []struct {
		name    string
		effects []effect.Instance
		refill  bool
	}{
		{"water breathing", []effect.Instance{breath("minecraft:water_breathing")}, true},
		{"conduit power", []effect.Instance{breath("minecraft:conduit_power")}, true},
		{"the nautilus alone", []effect.Instance{breath("minecraft:breath_of_the_nautilus")}, false},
		{"the nautilus with water breathing", []effect.Instance{
			breath("minecraft:breath_of_the_nautilus"),
			breath("minecraft:water_breathing"),
		}, true},
	}
	for _, tc := range cases {
		w, _ := pool(t)
		cow := aimless(NewMob(TypeCow, 1, 0.5, 66, 0.5, 1))
		cow.NoAI = true
		for _, in := range tc.effects {
			cow.AddEffect(in)
		}

		// A part-spent breath under water refills four a tick, except the
		// nautilus alone, which freezes it.
		cow.AirSupply = 100
		cow.Tick(w, nil)
		want := 100
		if tc.refill {
			want = 104
		}
		if cow.AirSupply != want {
			t.Errorf("%s: the breath after a tick = %d, want %d", tc.name, cow.AirSupply, want)
		}

		cow.AirSupply = rules.MaxAirSupply
		for range rules.MaxAirSupply + 40 {
			for _, ev := range cow.Tick(w, nil) {
				if ev.Kind == MobBurned && ev.DamageType == rules.DamageTypeDrown {
					t.Fatalf("%s took a drown bite", tc.name)
				}
			}
		}
		if cow.Health != cow.MaxHealth {
			t.Errorf("%s's health under water = %v, want the whole %v", tc.name, cow.Health, cow.MaxHealth)
		}
	}
}

// TestARefusedDrownBiteStillBursts checks a drown bite the window refuses
// outright: no health goes, yet the bubble burst event is still told, marked
// softened so no hurt telling follows.
func TestARefusedDrownBiteStillBursts(t *testing.T) {
	w, _ := pool(t)
	cow := aimless(NewMob(TypeCow, 1, 0.5, 66, 0.5, 1))
	cow.NoAI = true
	cow.HurtByBlast(5, 0, 0, 0, rules.DamageTypeExplosion, false)
	health := cow.Health
	cow.AirSupply = rules.AirDrownFloor + 1

	events := cow.Tick(w, nil)
	if cow.Health != health {
		t.Errorf("a refused bite left %v health, want %v", cow.Health, health)
	}
	var bite *MobEvent
	for i := range events {
		if events[i].Kind == MobBurned && events[i].DamageType == rules.DamageTypeDrown {
			bite = &events[i]
		}
	}
	if bite == nil {
		t.Fatal("the refused drowning told no bubble burst")
	}
	if !bite.Softened {
		t.Error("the refused bite was not marked softened, so its hurt telling would go out")
	}
}

// TestAWindowSoftenedKillPlaysNoDeathSound checks that a kill the window cut
// to its excess is marked softened, while a full-damage kill is not.
func TestAWindowSoftenedKillPlaysNoDeathSound(t *testing.T) {
	soft := aimless(NewMob(TypeCow, 1, 0.5, 66, 0.5, 1))
	soft.HurtByBlast(1, 0, 0, 0, rules.DamageTypeExplosion, false)
	soft.HurtByBlast(20, 0, 0, 0, rules.DamageTypeExplosion, false)
	if !soft.Dead {
		t.Fatal("the larger blow inside the window did not kill")
	}
	if !soft.DeathSoftened() {
		t.Error("a window-softened kill was not marked softened")
	}

	full := aimless(NewMob(TypeCow, 2, 0.5, 66, 0.5, 1))
	full.HurtByBlast(20, 0, 0, 0, rules.DamageTypeExplosion, false)
	if !full.Dead || full.DeathSoftened() {
		t.Errorf("a full-damage kill: dead=%v softened=%v, want dead and not softened", full.Dead, full.DeathSoftened())
	}
}

// TestOnlyABabyHappyGhastBreathesUnderWater checks HappyGhast's own
// canBreatheUnderwater: the ghastling breathes, the grown ghast, outside the
// tag, spends its air like a cow.
func TestOnlyABabyHappyGhastBreathesUnderWater(t *testing.T) {
	adult := NewMob(TypeHappyGhast, 1, 0.5, 66, 0.5, 1)
	if adult.canBreatheUnderwater() {
		t.Error("an adult happy ghast breathes under water, want it to spend its air")
	}
	baby := NewMob(TypeHappyGhast, 2, 0.5, 66, 0.5, 1)
	baby.BabyAge = -24000
	if !baby.canBreatheUnderwater() {
		t.Error("a baby happy ghast spends its air under water, want it to breathe")
	}
}
