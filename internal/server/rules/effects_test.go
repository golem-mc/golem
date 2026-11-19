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

// TestRottenFleshRollsItsChance applies rotten flesh's hunger under its 0.8
// and not over it (vanilla: ApplyStatusEffectsConsumeEffect).
func TestRottenFleshRollsItsChance(t *testing.T) {
	flesh := consumableOf(t, "minecraft:rotten_flesh")
	for seed, want := range map[int64]bool{1: true, 5: false} { // rolls 0.60 and 0.80
		v := FreshVitals(0)
		var s effect.Set
		out := Consume(&v, &s, nil, flesh.Effects, rand.New(rand.NewSource(seed)))
		if _, got := s.Get(effectHunger); got != want || len(out.Changes) != map[bool]int{true: 1}[want] {
			t.Errorf("seed %d: hunger %v with %d changes, want %v", seed, got, len(out.Changes), want)
		}
	}
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

// TestHealthBoostCapsAtTheAttributeRange caps the raised bar at the
// max_health attribute's own 1024, which the modifier cannot push past.
func TestHealthBoostCapsAtTheAttributeRange(t *testing.T) {
	v := FreshVitals(0)
	var s effect.Set
	AddEffect(&v, &s, effect.New(effectHealthBoost, 100, 255, false, true, true))
	Heal(&v, &s, nil, 1e6)
	if v.Health != 1024 {
		t.Errorf("a heal under Health Boost 256 reached %v, want the attribute's 1024", v.Health)
	}
}

// TestRemoveAndClearEffectsClampHealth is the max_health cap on the paths a
// command takes: taking the boost off outside the tick takes the health above
// the bar it leaves behind with it.
func TestRemoveAndClearEffectsClampHealth(t *testing.T) {
	v := FreshVitals(0)
	var s effect.Set
	AddEffect(&v, &s, effect.New(effectHealthBoost, 100, 1, false, true, true))
	Heal(&v, &s, nil, 30)
	if v.Health != 28 {
		t.Fatalf("a heal under Health Boost II reached %v, want 28", v.Health)
	}
	RemoveEffect(&v, &s, nil, effectHealthBoost)
	if v.Health != MaxHealth {
		t.Errorf("removing the boost left the health at %v, want %v", v.Health, MaxHealth)
	}

	AddEffect(&v, &s, effect.New(effectHealthBoost, 100, 0, false, true, true))
	Heal(&v, &s, nil, 30)
	if v.Health != 24 {
		t.Fatalf("a heal under Health Boost I reached %v, want 24", v.Health)
	}
	ClearEffects(&v, &s, nil)
	if v.Health != MaxHealth {
		t.Errorf("clearing the effects left the health at %v, want %v", v.Health, MaxHealth)
	}
}

// TestRegenerationHealsBeforeTheFoodTick heals in place, so the food tick
// that follows reads a healed body: no saturated heal, no exhaustion
// (vanilla: tickEffects in super.tick, then foodData.tick).
func TestRegenerationHealsBeforeTheFoodTick(t *testing.T) {
	v := player.Vitals{Health: 19, Food: 20, Saturation: 5, FoodTicks: 9, AirSupply: MaxAirSupply}
	var s effect.Set
	AddEffect(&v, &s, effect.New(effectRegeneration, 100, 0, false, true, true))
	TickVitals(&v, &s, nil, DifficultyNormal, false, Habitat{}, nil, 0, true)
	if v.Health != 20 || v.Exhaustion != 0 || v.FoodTicks != 0 {
		t.Errorf("health %v, exhaustion %v, food ticks %v; want 20, 0, 0", v.Health, v.Exhaustion, v.FoodTicks)
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

// TestInvisibilityAndGlowingRules checks the two effect flags: each bit follows
// its own effect alone, and neither stands on a body with no effects.
func TestInvisibilityAndGlowingRules(t *testing.T) {
	if Invisible(nil) || Glowing(nil) {
		t.Error("a body with no effects should be neither invisible nor glowing")
	}
	invisible := []effect.Instance{effect.New(effectInvisibility, 100, 0, false, true, true)}
	if !Invisible(invisible) || Glowing(invisible) {
		t.Error("invisibility should hide a body without outlining it")
	}
	glowing := []effect.Instance{effect.New(effectGlowing, 100, 0, false, true, true)}
	if !Glowing(glowing) || Invisible(glowing) {
		t.Error("glowing should outline a body without hiding it")
	}
}

// TestEffectHurtsComeBeforeTheBlocks orders a due poison ahead of the magma
// underfoot: tickEffects runs before aiStep's block hurts, and the first hurt
// is the one the invulnerability window lets land.
func TestEffectHurtsComeBeforeTheBlocks(t *testing.T) {
	v := FreshVitals(0)
	var s effect.Set
	AddEffect(&v, &s, effect.New(effectPoison, 100, 0, false, true, true))
	_, hurts, _, _ := TickVitals(&v, &s, nil, DifficultyNormal, false,
		Habitat{Ground: stateOf(t, "minecraft:magma_block"), OnGround: true}, nil, 0, true)
	if len(hurts) != 2 || hurts[0].DamageType != DamageTypeMagic || hurts[1].DamageType != DamageTypeHotFloor {
		t.Errorf("hurts = %v, want the poison's magic before the magma's hot_floor", hurts)
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

// TestAParchedKeepsNoWeakness checks the parched's own canBeAffected: it
// refuses weakness alone, over the poison and regeneration the undead tag
// already keeps from it, and takes the rest, the zombie taking weakness
// (vanilla: Parched.canBeAffected).
func TestAParchedKeepsNoWeakness(t *testing.T) {
	parched, _ := registry.EntityTypeID("minecraft:parched")
	zombie, _ := registry.EntityTypeID("minecraft:zombie")
	if CanBeAffected(parched, effectWeakness) {
		t.Error("the parched takes weakness, which its class refuses")
	}
	for _, id := range []int32{effectPoison, effectRegeneration} {
		if CanBeAffected(parched, id) {
			t.Errorf("the parched takes effect %d, which the undead tag refuses", id)
		}
	}
	for _, id := range []int32{effectHunger, effectWither, effectInstantDamage, effectInfested, effectOozing} {
		if !CanBeAffected(parched, id) {
			t.Errorf("the parched refuses effect %d, which only weakness and the undead pair it refuses", id)
		}
	}
	if !CanBeAffected(zombie, effectWeakness) {
		t.Error("the zombie refuses weakness, which only the parched refuses")
	}
}

// TestInstantEffectAmountsAreRounded checks the strength a scaled instant
// effect lands at: the base scaled and rounded at a half, so a splash
// Harming I at three-fifths strength deals the rounded 4 and one at a
// twentieth strength deals 0, and a scaled Healing I gives back the rounded
// 2.
// vanilla: HealOrHarmMobEffect.applyInstantaneousEffect
func TestInstantEffectAmountsAreRounded(t *testing.T) {
	for _, c := range []struct {
		id     int32
		scale  float64
		amount float64
		hurt   bool
	}{
		{effectInstantDamage, 0.6, 4, true},
		{effectInstantDamage, 0.05, 0, true},
		{effectInstantHealth, 0.6, 2, false},
	} {
		amount, hurt := InstantEffect(protocol.EntityTypePlayer, c.id, 0, c.scale)
		if amount != c.amount || hurt != c.hurt {
			t.Errorf("effect %d at scale %v = (%v, %v), want (%v, %v)", c.id, c.scale, amount, hurt, c.amount, c.hurt)
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

func TestSpectatorBodyVisible(t *testing.T) {
	cases := []struct {
		viewerSpec, bodySpec, ownCamera, want bool
	}{
		{false, false, true, true},
		{false, true, true, false},
		{true, true, true, true},
		{true, true, false, false},
		{true, false, true, true},
	}
	for _, c := range cases {
		if got := SpectatorBodyVisible(c.viewerSpec, c.bodySpec, c.ownCamera); got != c.want {
			t.Errorf("SpectatorBodyVisible(%v, %v, %v) = %v, want %v", c.viewerSpec, c.bodySpec, c.ownCamera, got, c.want)
		}
	}
}

// TestAWitherSkeletonKeepsNoWither checks WitherSkeleton.canBeAffected.
func TestAWitherSkeletonKeepsNoWither(t *testing.T) {
	skeleton, _ := registry.EntityTypeID("minecraft:wither_skeleton")
	zombie, _ := registry.EntityTypeID("minecraft:zombie")
	if CanBeAffected(skeleton, effectWither) {
		t.Error("the wither skeleton takes wither")
	}
	if !CanBeAffected(zombie, effectWither) {
		t.Error("the zombie refuses wither")
	}
}
