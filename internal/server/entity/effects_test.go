package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The mob's status effects as ordinary unit tests: the doors the immunity
// checks stand at, the bite a due poison tick takes, and the instant pair
// the undead take the other way round. The proof of them on the wire is
// the black-box tests in internal/server/*_test.go; these run the body's own step.

// TestAMobRefusesTheEffectsItsTypeRefuses checks AddEffect against the
// tags: a cow takes what it is given, the zombie keeps neither poison nor
// regeneration, the spider and the cave spider keep no poison, and each
// still takes the instant pair.
// vanilla: LivingEntity.canBeAffected, Spider.canBeAffected,
// EntityTypeTags.IGNORES_POISON_AND_REGEN
func TestAMobRefusesTheEffectsItsTypeRefuses(t *testing.T) {
	poison := registry.MustMobEffect("minecraft:poison")
	for _, id := range []int32{poison, registry.MustMobEffect("minecraft:regeneration")} {
		cow := NewMob(TypeCow, 1, 0, 0, 0, 1)
		if _, ok := cow.AddEffect(effect.New(id, 100, 0, false, true, true)); !ok {
			t.Errorf("the cow refused effect %d, which nothing refuses it", id)
		}
		zombie := NewMob(TypeZombie, 2, 0, 0, 0, 1)
		if _, ok := zombie.AddEffect(effect.New(id, 100, 0, false, true, true)); ok {
			t.Errorf("the zombie took effect %d, which its tags refuse", id)
		}
		if len(zombie.AllEffects()) != 0 {
			t.Errorf("the zombie that refused effect %d carries %v", id, zombie.AllEffects())
		}
	}
	for _, kind := range []MobType{TypeSpider, TypeCaveSpider} {
		spider := NewMob(kind, 1, 0, 0, 0, 1)
		if _, ok := spider.AddEffect(effect.New(poison, 100, 0, false, true, true)); ok {
			t.Errorf("the %s took poison, which its class refuses", kind)
		}
		if _, ok := spider.AddEffect(effect.New(registry.MustMobEffect("minecraft:regeneration"), 100, 0, false, true, true)); !ok {
			t.Errorf("the %s refused regeneration, which only poison it refuses", kind)
		}
	}
}

// TestPoisonBitesAMobEveryTwentyFifthTick runs a poisoned cow's own step,
// one tick at a time: a heart on the first tick, none for the next
// twenty-four, one again on the twenty-sixth, each told as a hurt of magic
// with nobody behind it, and nothing once the body is down to the single
// heart the poison leaves it at.
// vanilla: PoisonMobEffect.applyEffectTick, ShouldApplyEffectTickThisTick
func TestPoisonBitesAMobEveryTwentyFifthTick(t *testing.T) {
	cow := NewMob(TypeCow, 1, 0, 0, 0, 1)
	if _, ok := cow.AddEffect(effect.New(registry.MustMobEffect("minecraft:poison"), 200, 0, false, true, true)); !ok {
		t.Fatal("the cow refused its poison")
	}
	// The invulnerability window a bite opens is the plain hurt path's,
	// and its countdown is the whole tick's; the step here steps over
	// it by hand, one tick at a time.
	var total float64
	for tick := range 26 {
		if cow.InvulnTicks > 0 {
			cow.InvulnTicks--
		}
		cow.Age++
		events := cow.effectsStep(nil)
		if tick != 0 && tick != 25 {
			if len(events) != 0 {
				t.Fatalf("tick %d: events %v, want none", tick+1, events)
			}
			continue
		}
		if len(events) != 1 || events[0].Kind != MobBurned || events[0].DamageType != rules.DamageTypeMagic {
			t.Fatalf("tick %d: events %v, want the poison's magic bite alone", tick+1, events)
		}
		total += 1
	}
	if cow.Health != cow.MaxHealth-total {
		t.Errorf("health after two bites = %v, want %v", cow.Health, cow.MaxHealth-total)
	}
	for range 20 {
		cow.Health = 1.5
		cow.Age++
		events := cow.effectsStep(nil)
		if len(events) != 0 || cow.Health != 1.5 {
			t.Fatalf("the poison bit at the single heart it leaves: %v, health %v", events, cow.Health)
		}
	}
}

// TestTheInstantPairTheUndeadWay checks a zombie's own step against the
// inverted-healing-and-harm tag: the health it is given takes six off it,
// and the harm II gives eight back, capped at its twenty. Both bites
// land as magic, and each effect is spent with its tick.
// vanilla: HealOrHarmMobEffect.applyEffectTick, INVERTED_HEALING_AND_HARM
func TestTheInstantPairTheUndeadWay(t *testing.T) {
	zombie := NewMob(TypeZombie, 1, 0, 0, 0, 1)
	health := registry.MustMobEffect("minecraft:instant_health")
	if _, ok := zombie.AddEffect(effect.New(health, 1, 0, false, true, true)); !ok {
		t.Fatal("the zombie refused its instant health")
	}
	zombie.Age++
	events := zombie.effectsStep(nil)
	bitten, removed := false, false
	for _, ev := range events {
		if ev.Kind == MobBurned && ev.DamageType == rules.DamageTypeMagic {
			bitten = true
		}
		if ev.Kind == MobEffectChanged && ev.EffectChange.Kind == effect.Removed && ev.EffectChange.Instance.ID == health {
			removed = true
		}
	}
	if !bitten || zombie.Health != 14 {
		t.Errorf("the instant health bit %v and left %v, want magic to 14", bitten, zombie.Health)
	}
	if !removed {
		t.Errorf("the instant health its tick spent left events %v", events)
	}
	harm := registry.MustMobEffect("minecraft:instant_damage")
	if _, ok := zombie.AddEffect(effect.New(harm, 1, 1, false, true, true)); !ok {
		t.Fatal("the zombie refused its instant damage")
	}
	zombie.Age++
	zombie.effectsStep(nil)
	if zombie.Health != 20 {
		t.Errorf("the instant damage II healed to %v, want the cap of 20", zombie.Health)
	}
}

// TestMovementAndDamageEffectsScaleAMob checks the pace and the blow of a
// zombie under speed, slowness, strength and weakness.
func TestMovementAndDamageEffectsScaleAMob(t *testing.T) {
	give := func(z *Mob, name string, amp int32) {
		z.AddEffect(effect.New(registry.MustMobEffect("minecraft:"+name), 1000, amp, false, true, true))
	}
	base := NewMob(TypeZombie, 1, 0, 0, 0, 1)
	pace, dmg := base.pace(), base.attackDamage()
	cases := []struct {
		name      string
		effects   map[string]int32
		pace, hit float64
	}{
		{"speed I", map[string]int32{"speed": 0}, pace * 1.2, dmg},
		{"slowness II", map[string]int32{"slowness": 1}, pace * 0.7, dmg},
		{"speed I and slowness I", map[string]int32{"speed": 0, "slowness": 0}, pace * 1.2 * 0.85, dmg},
		{"slowness VII", map[string]int32{"slowness": 6}, 0, dmg},
		{"strength I", map[string]int32{"strength": 0}, pace, dmg + 3},
		{"weakness I", map[string]int32{"weakness": 0}, pace, 0},
		{"strength I and weakness I", map[string]int32{"strength": 0, "weakness": 0}, pace, dmg - 1},
	}
	for _, tc := range cases {
		z := NewMob(TypeZombie, 1, 0, 0, 0, 1)
		for n, amp := range tc.effects {
			give(z, n, amp)
		}
		// The pace comes off the map, whose modifiers are float32 amounts.
		if math.Abs(z.pace()-tc.pace) > 1e-7 || !closeEnough(z.attackDamage(), tc.hit) {
			t.Errorf("%s: pace %v damage %v, want %v and %v", tc.name, z.pace(), z.attackDamage(), tc.pace, tc.hit)
		}
	}
}

// TestAbsorptionSpendsBeforeHealthAndEndsWithTheEffect checks a zombie with
// Absorption II: eight extra hearts, a five-point hurt leaving health alone
// and three hearts, a bigger one spilling into health, and the extra hearts
// going with the effect.
// vanilla: AbsorptionMobEffect.onEffectStarted, LivingEntity.actuallyHurt
func TestAbsorptionSpendsBeforeHealthAndEndsWithTheEffect(t *testing.T) {
	absorption := registry.MustMobEffect("minecraft:absorption")
	zombie := NewMob(TypeZombie, 1, 0, 0, 0, 1)
	if _, ok := zombie.AddEffect(effect.New(absorption, 100, 1, false, true, true)); !ok {
		t.Fatal("the zombie refused absorption")
	}
	if zombie.Absorption != 8 {
		t.Fatalf("absorption = %v, want 8", zombie.Absorption)
	}
	if landed, _ := zombie.HurtByWorld(5, rules.DamageTypeMagic); !landed {
		t.Fatal("the hurt did not land")
	}
	if zombie.Health != zombie.MaxHealth || zombie.Absorption != 3 {
		t.Errorf("health %v absorption %v, want full health and 3", zombie.Health, zombie.Absorption)
	}
	zombie.InvulnTicks = 0
	zombie.HurtByWorld(5, rules.DamageTypeMagic)
	if zombie.Health != zombie.MaxHealth-2 || zombie.Absorption != 0 {
		t.Errorf("health %v absorption %v, want 2 off and 0", zombie.Health, zombie.Absorption)
	}
	zombie.Absorption = 4
	zombie.RemoveEffect(absorption)
	if zombie.Absorption != 0 {
		t.Errorf("absorption after removal = %v, want 0", zombie.Absorption)
	}
}
