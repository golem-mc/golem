package rules

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

func consumableOf(t *testing.T, name string) registry.Consumable {
	t.Helper()
	c, ok := registry.ConsumableOf(itemID(t, name))
	if !ok {
		t.Fatalf("%s is not consumable", name)
	}
	return c
}

// TestGoldenAppleAbsorbsAndHoneyCures eats a golden apple - four extra
// hearts' worth of absorption that a hit spends first - and drinks honey
// against poison.
func TestGoldenAppleAbsorbsAndHoneyCures(t *testing.T) {
	v := FreshVitals(0)
	var s effect.Set
	Consume(&v, &s, nil, consumableOf(t, "minecraft:golden_apple").Effects, rand.New(rand.NewSource(1)))
	if v.Absorption != 4 {
		t.Fatalf("absorption = %v, want 4", v.Absorption)
	}
	if left := AbsorbDamage(&v, 6); left != 2 || v.Absorption != 0 {
		t.Errorf("a hit of 6 left %v for the health and %v absorption, want 2 and 0", left, v.Absorption)
	}
	// With the hearts spent, the effect ends on its next tick.
	var hurts []Hurt
	tickEffects(&v, &s, nil, &hurts)
	if _, ok := s.Get(effectAbsorption); ok {
		t.Error("absorption outlived its hearts")
	}

	AddEffect(&v, &s, effect.New(effectPoison, 100, 0, false, true, true))
	out := Consume(&v, &s, nil, consumableOf(t, "minecraft:honey_bottle").Effects, nil)
	if _, ok := s.Get(effectPoison); ok || len(out.Changes) != 1 || out.Changes[0].Kind != effect.Removed {
		t.Errorf("honey left poison on, changes %+v", out.Changes)
	}
}

// TestHungerAndRegenerationTicks runs the per-tick effects on the vitals.
func TestHungerAndRegenerationTicks(t *testing.T) {
	v := player.Vitals{Health: 10, Food: 20}
	var s effect.Set
	AddEffect(&v, &s, effect.New(effectHunger, 600, 1, false, true, true))
	AddEffect(&v, &s, effect.New(effectRegeneration, 100, 0, false, true, true))
	var hurts []Hurt
	tickEffects(&v, &s, nil, &hurts) // regeneration is due at 100
	if v.Health != 11 || v.Exhaustion != float64(float32(0.005)*2) {
		t.Errorf("after one tick: health %v, exhaustion %v; want 11 and 0.01", v.Health, v.Exhaustion)
	}
}

// TestHealthBoostBarsAndClamps is Health Boost's side of the max-health
// attribute: the four a level only raise the bar the heals cap at, and a
// weaker hidden boost taking over, or the boost ending, takes the health
// above the bar it leaves behind with it.
// vanilla: MobEffects.HEALTH_BOOST, LivingEntity.onAttributeUpdated
func TestHealthBoostBarsAndClamps(t *testing.T) {
	v := FreshVitals(0)
	var s effect.Set
	var hurts []Hurt

	AddEffect(&v, &s, effect.New(effectHealthBoost, 100, 1, false, true, true))
	if v.Health != MaxHealth {
		t.Fatalf("Health Boost II alone left the health at %v, want the twenty it started at", v.Health)
	}
	v.Health = 10
	Heal(&v, &s, nil, 30)
	if v.Health != 28 {
		t.Errorf("a heal under Health Boost II reached %v, want the raised bar's 28", v.Health)
	}

	// A longer Health Boost I hides behind the active one; when the II runs
	// out, the I takes over and the bar drops to 24 with the health above it.
	AddEffect(&v, &s, effect.New(effectHealthBoost, 200, 0, false, true, true))
	for range 105 {
		tickEffects(&v, &s, nil, &hurts)
	}
	if v.Health != 24 {
		t.Errorf("health after the hidden Health Boost I took over = %v, want its bar's 24", v.Health)
	}
	for range 105 {
		tickEffects(&v, &s, nil, &hurts)
	}
	if v.Health != MaxHealth {
		t.Errorf("health after the boost ended = %v, want %v", v.Health, MaxHealth)
	}
}

// TestAirSupplyEffectRules checks the two effect predicates the breath
// reads: water_breathing, conduit_power and breath_of_the_nautilus all spare
// the drain, but the nautilus on its own freezes the breath instead of
// letting it refill (vanilla: MobEffectUtil.hasWaterBreathing and
// shouldEffectsRefillAirsupply).
func TestAirSupplyEffectRules(t *testing.T) {
	effectList := func(ids ...int32) []effect.Instance {
		var out []effect.Instance
		for _, id := range ids {
			out = append(out, effect.New(id, 100, 0, false, true, true))
		}
		return out
	}
	if HasWaterBreathing(nil) || !ShouldRefillAirSupply(nil) {
		t.Error("a body with no effects should not breathe water and should refill")
	}
	if !HasWaterBreathing(effectList(effectNautilus)) {
		t.Error("breath_of_the_nautilus should spare the drain")
	}
	if ShouldRefillAirSupply(effectList(effectNautilus)) {
		t.Error("breath_of_the_nautilus alone should freeze the breath")
	}
	for _, id := range []int32{effectWaterBreathing, effectConduitPower} {
		if !HasWaterBreathing(effectList(id)) || !ShouldRefillAirSupply(effectList(id)) {
			t.Errorf("effect %d should both spare the drain and refill", id)
		}
		if !ShouldRefillAirSupply(effectList(effectNautilus, id)) {
			t.Errorf("the nautilus with effect %d should refill", id)
		}
	}
}

// TestResistanceAndFireResistance checks the damage pipeline's two effects.
func TestResistanceAndFireResistance(t *testing.T) {
	active := []effect.Instance{
		effect.New(effectResistance, 100, 1, false, true, true),
		effect.New(effectFireResistance, 100, 0, false, true, true),
	}
	if got := Resist(active, DamageTypeMobAttack, 10); got != 6 {
		t.Errorf("resistance II took 10 to %v, want 6", got)
	}
	if got := Resist(active, DamageTypeStarve, 1); got != 1 {
		t.Errorf("resistance took starvation to %v, want it bypassed", got)
	}
	if !FireImmune(active, DamageTypeLava) || FireImmune(active, DamageTypeMagic) {
		t.Error("fire resistance should turn lava away and nothing else")
	}
}

// TestCanBeAffected checks the immunity a body of a type carries into the
// effects it accepts: the undead — the ignores-poison-and-regen tag — keep
// neither poison nor regeneration, the spider and the cave spider keep no
// poison, the silverfish no infested, the slime no oozing, and every body
// takes the rest, a cow every effect there is (vanilla:
// LivingEntity.canBeAffected, Spider.canBeAffected).
func TestCanBeAffected(t *testing.T) {
	zombie, _ := registry.EntityTypeID("minecraft:zombie")
	cow, _ := registry.EntityTypeID("minecraft:cow")
	spider, _ := registry.EntityTypeID("minecraft:spider")
	caveSpider, _ := registry.EntityTypeID("minecraft:cave_spider")
	silverfish, _ := registry.EntityTypeID("minecraft:silverfish")
	slime, _ := registry.EntityTypeID("minecraft:slime")
	for _, id := range []int32{effectPoison, effectRegeneration} {
		if CanBeAffected(zombie, id) {
			t.Errorf("the zombie takes effect %d, which its tags refuse", id)
		}
		if !CanBeAffected(cow, id) {
			t.Errorf("the cow refuses effect %d, which nothing refuses it", id)
		}
	}
	for _, kind := range []struct {
		name string
		id   int32
	}{{"spider", spider}, {"cave spider", caveSpider}} {
		if CanBeAffected(kind.id, effectPoison) {
			t.Errorf("the %s takes poison, which its class refuses", kind.name)
		}
		if !CanBeAffected(kind.id, effectRegeneration) {
			t.Errorf("the %s refuses regeneration, which only poison it refuses", kind.name)
		}
	}
	if CanBeAffected(silverfish, effectInfested) {
		t.Error("the silverfish takes infested, which its immunity refuses")
	}
	if !CanBeAffected(silverfish, effectPoison) {
		t.Error("the silverfish refuses poison, which only infested it refuses")
	}
	if CanBeAffected(slime, effectOozing) {
		t.Error("the slime takes oozing, which its immunity refuses")
	}
	if !CanBeAffected(slime, effectPoison) {
		t.Error("the slime refuses poison, which only oozing it refuses")
	}
	for _, id := range []int32{effectInstantHealth, effectInstantDamage, effectWither, effectHunger} {
		if !CanBeAffected(zombie, id) || !CanBeAffected(cow, id) {
			t.Errorf("effect %d should land on both the zombie and the cow", id)
		}
	}
}

// TestHealOrHarm checks the instant pair's two directions: the
// inverted-healing-and-harm tag swapping the way each lands on the undead, and
// the amount following the outcome, four health or six harm a level
// (vanilla: HealOrHarmMobEffect.applyEffectTick).
func TestHealOrHarm(t *testing.T) {
	zombie, _ := registry.EntityTypeID("minecraft:zombie")
	for _, tc := range []struct {
		effect    int32
		amplifier int32
		amount    float64
		hurt      bool
	}{
		{effectInstantHealth, 0, 6, true},
		{effectInstantHealth, 1, 12, true},
		{effectInstantHealth, 2, 24, true},
		{effectInstantDamage, 0, 4, false},
		{effectInstantDamage, 1, 8, false},
		{effectInstantDamage, 3, 32, false},
	} {
		if got, hurt := HealOrHarm(zombie, tc.effect, tc.amplifier); got != tc.amount || hurt != tc.hurt {
			t.Errorf("effect %d at amplifier %d on a zombie = (%v, %v), want (%v, %v)", tc.effect, tc.amplifier, got, hurt, tc.amount, tc.hurt)
		}
	}
	if got, hurt := HealOrHarm(protocol.EntityTypePlayer, effectInstantDamage, 0); !hurt || got != 6 {
		t.Errorf("instant damage on a player = %v hurt %v, want 6 hurt", got, hurt)
	}
	if got, hurt := HealOrHarm(protocol.EntityTypePlayer, effectInstantHealth, 0); hurt || got != 4 {
		t.Errorf("instant health on a player = %v hurt %v, want 4 healed", got, hurt)
	}
}
