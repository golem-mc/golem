package rules

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// namedKiller is a killer behind a blow, named by a plain word and holding
// nothing, the way most of these deaths come.
func namedKiller(name string) *Killer {
	return &Killer{Name: protocol.Text{Text: name}}
}

// itemID resolves one item by name, none of the combat and wear tests
// willing to guess at a number the registry owns.
func itemID(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("the item registry has no %s", name)
	}
	return id
}

// TestArmourReductionAgainstVanilla checks the formula by vanilla's own
// arithmetic - the reduction is min(20, max(points/5, points -
// 4*damage/(toughness+8))) of twenty-fifths - worked out by hand for a
// spread of suits and blows.
func TestArmourReductionAgainstVanilla(t *testing.T) {
	cases := []struct {
		what                            string
		damage, points, toughness, want float64
	}{
		// A diamond suit against a sword's hit: the toughness side wins,
		// 17.5 of twenty-fifths off.
		{"a diamond suit against a sword", 10, 20, 8, 10 * (1 - 17.5/25)},
		// Half a suit, no toughness: ten points take a straight fifth of
		// the blow.
		{"half a suit", 10, 10, 0, 10 * (1 - 5.0/25)},
		// A light chestpiece against a heavy blow: the points term is the
		// smaller, 1.6 of twenty-fifths.
		{"light armour, heavy blow", 100, 20, 8, 100 * (1 - 4.0/25)},
		// Points only, five of them, small damage: 3/25ths off a fistful.
		{"a leather chestpiece", 4, 5, 0, 4 * (1 - 3.0/25)},
		// Zero points reduce nothing, whatever the toughness beside them.
		{"no armour at all", 7, 0, 3, 7},
	}
	for _, c := range cases {
		got := DamageAfterArmour(c.damage, c.points, c.toughness)
		if math.Abs(got-c.want) > 1e-5 {
			t.Errorf("DamageAfterArmour of %v (%s) = %v, want %v",
				c.damage, c.what, got, c.want)
		}
	}
}

// TestArmourBypassedByDamage checks which of the hurts walk straight
// through armour, the minecraft:bypasses_armor tag the registry's own tag
// table carries: every member of it bypasses, the types this server deals
// and the ones it only names alike — a magic blow is as much outside the
// armour's business as a fall is — and a type outside it does not.
func TestArmourBypassedByDamage(t *testing.T) {
	bypassed := map[registry.DamageType]bool{
		DamageTypeFall:         true,
		DamageTypeOutOfWorld:   true,
		DamageTypeStarve:       true,
		DamageTypeGenericKill:  true,
		DamageTypeOnFire:       true,
		DamageTypeInWall:       true,
		DamageTypeDrown:        true,
		DamageTypePlayerAttack: false,
	}
	for damageType, want := range bypassed {
		if got := ArmourBypassed(damageType); got != want {
			t.Errorf("ArmourBypassed(%s) = %v, want %v", damageType, got, want)
		}
	}
	if !ArmourBypassed(registry.DamageTypeMagic) {
		t.Error("a member of the bypasses_armor tag did not bypass")
	}
	if ArmourBypassed(registry.DamageTypeLightningBolt) {
		t.Error("a damage type outside the tag was bypassed")
	}
}

// TestCriticalHit prices the falling attacker's critical: half again the
// damage - the fist's one and a sword's six alike - and no critical for a
// hit planted on the ground, nor for a swing recovered short of the nine
// tenths the extras wait for.
func TestCriticalHit(t *testing.T) {
	sword := 1.0 + float64(registry.AttackDamage(itemID(t, "minecraft:iron_sword")))

	if got, want := AttackDamage(sword, false, 1), 6.0; got != want {
		t.Errorf("a grounded sword hit carries %v, want %v", got, want)
	}
	if got, want := AttackDamage(sword, true, 1), 9.0; got != want {
		t.Errorf("a falling sword hit carries %v, want the critical's %v", got, want)
	}
	if got, want := AttackDamage(1, false, 1), 1.0; got != want {
		t.Errorf("a bare fist carries %v, want %v", got, want)
	}
	if got, want := AttackDamage(1, true, 1), 1.5; got != want {
		t.Errorf("a falling bare fist carries %v, want the critical's %v", got, want)
	}
	if got, want := AttackDamage(sword, true, 0.8), 6.0*(0.2+0.8*0.8*0.8); math.Abs(got-want) > 1e-9 {
		t.Errorf("a falling sword hit at recovery 0.8 carries %v, want the scaled %v", got, want)
	}
}

// TestKnockbackVector runs vanilla's push by where the two stand: away from
// the attacker at the strength of 0.4, plus the same upward. A hit whose two
// stand at the same x and z has no direction to push along, so vanilla's
// knockback draws one.
func TestKnockbackVector(t *testing.T) {
	rng := dice()

	// The victim three east and four south of the attacker: the unit of
	// that line at the strength's four tenths.
	vx, vy, vz := KnockbackExtra(3, 4, 0, 0, 1, true, rng)
	if math.Abs(vx-0.24) > 1e-12 || math.Abs(vz-0.32) > 1e-12 || vy != 0.4 {
		t.Errorf("the push is (%v, %v, %v), want (0.24, 0.4, 0.32)", vx, vy, vz)
	}
	if got := math.Hypot(vx, vz); math.Abs(got-0.4) > 1e-12 {
		t.Errorf("the push's horizontal speed is %v, want the strength's 0.4", got)
	}

	// Players at the same spot have no direction to be pushed along, so
	// vanilla's knockback draws one, still with the upward half.
	vx, vy, vz = KnockbackExtra(0, 0, 0, 0, 1, true, rng)
	if math.Hypot(vx, vz) == 0 || vy != 0.4 {
		t.Errorf("the push of a same-spot hit is (%v, %v, %v), want a random horizontal and the upward 0.4", vx, vy, vz)
	}
}

// TestArrowPunchScalesOnlyItsHorizontal checks the Punch extra's shape under
// partial knockback resistance: only the horizontal push is scaled by what
// the resistance leaves, while the tenth of lift stays whole.
func TestArrowPunchScalesOnlyItsHorizontal(t *testing.T) {
	// Punch II west at half resistance: 2*0.6*0.5 of horizontal, the lift
	// untouched.
	vx, vy, vz := ArrowPunch(-2, 0, 2, 0.5)
	if math.Abs(vx-(-0.6)) > 1e-12 {
		t.Errorf("the punch push's x = %v, want the resistance-scaled -0.6", vx)
	}
	if vy != arrowPunchLift {
		t.Errorf("the punch push's lift = %v, want the whole tenth %v", vy, arrowPunchLift)
	}
	if vz != 0 {
		t.Errorf("the punch push's z = %v, want none", vz)
	}
}

// TestDeathMessageByType names each death: the victim by their killer, or
// the fall, the void and the empty stomach by what they were.
func TestDeathMessageByType(t *testing.T) {
	if got := DeathMessage(DamageTypeFall, protocol.Text{Text: "Alex"}, nil); got.Translate != "death.fell.accident.generic" {
		t.Errorf("a fall died as %q", got.Translate)
	}
	if got := DeathMessage(DamageTypeStarve, protocol.Text{Text: "Alex"}, nil); got.Translate != "death.attack.starve" {
		t.Errorf("hunger died as %q", got.Translate)
	}
	if got := DeathMessage(DamageTypeOutOfWorld, protocol.Text{Text: "Alex"}, nil); got.Translate != "death.attack.outOfWorld" {
		t.Errorf("the void died as %q", got.Translate)
	}
	if got := DeathMessage(DamageTypeGenericKill, protocol.Text{Text: "Alex"}, nil); got.Translate != "death.attack.genericKill" {
		t.Errorf("a kill died as %q", got.Translate)
	}

	// A hit names its killer after the victim.
	got := DeathMessage(DamageTypePlayerAttack, protocol.Text{Text: "Alex"}, namedKiller("Steve"))
	if got.Translate != "death.attack.player" || len(got.With) != 2 ||
		got.With[0].Text != "Alex" || got.With[1].Text != "Steve" {
		t.Errorf("a player attack died as %+v", got)
	}

	// A named weapon in the killer's hand swaps in the ".item" form, which
	// names the weapon after the killer.
	named := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_sword"), Count: 1, CustomName: &protocol.Text{Text: "Excalibur"}}
	got = DeathMessage(DamageTypePlayerAttack, protocol.Text{Text: "Alex"}, &Killer{Name: protocol.Text{Text: "Steve"}, Weapon: named})
	if got.Translate != "death.attack.player.item" || len(got.With) != 3 ||
		got.With[0].Text != "Alex" || got.With[1].Text != "Steve" || got.With[2].Translate != "chat.square_brackets" ||
		got.With[2].With[0].Extra[0].Text != "Excalibur" || !got.With[2].With[0].Extra[0].Italic {
		t.Errorf("a player attack with a named sword died as %+v", got)
	}

	// The explosion and the fireworks name a killer's named weapon too.
	killerWith := &Killer{Name: protocol.Text{Text: "Steve"}, Weapon: named}
	got = DeathMessage(DamageTypeExplosion, protocol.Text{Text: "Alex"}, killerWith)
	if got.Translate != "death.attack.explosion.player.item" || len(got.With) != 3 {
		t.Errorf("an explosion with a named weapon died as %+v", got)
	}
	got = DeathMessage(DamageTypeFireworks, protocol.Text{Text: "Alex"}, killerWith)
	if got.Translate != "death.attack.fireworks.item" || len(got.With) != 3 {
		t.Errorf("fireworks with a named weapon died as %+v", got)
	}

	// With nobody behind the blow, the key gets only the victim.
	got = DeathMessage(DamageTypePlayerAttack, protocol.Text{Text: "Alex"}, nil)
	if len(got.With) != 1 || got.With[0].Text != "Alex" {
		t.Errorf("a killer-less attack died as %+v", got)
	}

	// An arrow names its shooter, or the shooter's named bow.
	got = DeathMessage(DamageTypeArrow, protocol.Text{Text: "Alex"}, namedKiller("Steve"))
	if got.Translate != "death.attack.arrow" || len(got.With) != 2 ||
		got.With[0].Text != "Alex" || got.With[1].Text != "Steve" {
		t.Errorf("an arrow death died as %+v", got)
	}
	got = DeathMessage(DamageTypeArrow, protocol.Text{Text: "Alex"}, &Killer{Name: protocol.Text{Text: "Steve"}, Weapon: named})
	if got.Translate != "death.attack.arrow.item" || len(got.With) != 3 {
		t.Errorf("an arrow from a named bow died as %+v", got)
	}

	got = DeathMessage(DamageTypeMaceSmash, protocol.Text{Text: "Alex"}, &Killer{Name: protocol.Text{Text: "Steve"}})
	if got.Translate != "death.attack.mace_smash" || len(got.With) != 2 {
		t.Errorf("a mace smash died as %+v", got)
	}

	got = DeathMessage(DamageTypeThorns, protocol.Text{Text: "Alex"}, &Killer{Name: protocol.Text{Text: "Steve"}})
	if got.Translate != "death.attack.thorns" || len(got.With) != 2 {
		t.Errorf("a thorns death died as %+v", got)
	}

	// The trident's ".item" form is the same one.
	got = DeathMessage(DamageTypeTrident, protocol.Text{Text: "Alex"}, &Killer{Name: protocol.Text{Text: "Steve"}, Weapon: named})
	if got.Translate != "death.attack.trident.item" || len(got.With) != 3 {
		t.Errorf("a trident with a named weapon died as %+v", got)
	}

	// A monster's blow is said by the vanilla mob message, the killer's
	// translated kind carried through as the component it was given.
	got = DeathMessage(DamageTypeMobAttack, protocol.Text{Text: "Alex"}, &Killer{Name: protocol.Text{Translate: "entity.minecraft.zombie"}})
	if got.Translate != "death.attack.mob" || len(got.With) != 2 ||
		got.With[0].Text != "Alex" || got.With[1].Translate != "entity.minecraft.zombie" {
		t.Errorf("a mob attack died as %+v", got)
	}
	got = DeathMessage(DamageTypeMobAttack, protocol.Text{Text: "Alex"}, &Killer{
		Name:   protocol.Text{Translate: "entity.minecraft.zombie"},
		Weapon: named,
	})
	if got.Translate != "death.attack.mob.item" || len(got.With) != 3 {
		t.Errorf("a mob attack with a named weapon died as %+v", got)
	}

	// A falling body's source is the body itself, so the message names the
	// victim and the block both, though only the victim is ever shown.
	got = DeathMessage(DamageTypeFallingAnvil, protocol.Text{Text: "Alex"}, namedKiller("Anvil"))
	if got.Translate != "death.attack.anvil" || len(got.With) != 2 ||
		got.With[0].Text != "Alex" || got.With[1].Text != "Anvil" {
		t.Errorf("a falling anvil died as %+v", got)
	}
	got = DeathMessage(DamageTypeFallingBlock, protocol.Text{Text: "Alex"}, namedKiller("Sand"))
	if got.Translate != "death.attack.fallingBlock" || len(got.With) != 2 {
		t.Errorf("a falling block died as %+v", got)
	}
	got = DeathMessage(DamageTypeFallingStalactite, protocol.Text{Text: "Alex"}, namedKiller("Pointed Dripstone"))
	if got.Translate != "death.attack.fallingStalactite" || len(got.With) != 2 {
		t.Errorf("a falling stalactite died as %+v", got)
	}
}

// TestMobAttackScalesByDifficulty checks the scaling Player.hurt applies to
// a blow that comes scaled-with-difficulty off a living attacker: peaceful
// lands nothing, easy folds the small blows whole and halves the rest, hard
// adds half again, and normal leaves the blow as it came.
func TestMobAttackScalesByDifficulty(t *testing.T) {
	zombie := 3.0
	if got := MobAttackAfterDifficulty(zombie, DifficultyPeaceful); got != 0 {
		t.Errorf("peaceful scaled a zombie's blow to %v, want nothing", got)
	}
	if got := MobAttackAfterDifficulty(zombie, DifficultyEasy); got != 2.5 {
		t.Errorf("easy scaled a zombie's blow to %v, want 2.5", got)
	}
	if got := MobAttackAfterDifficulty(zombie, DifficultyNormal); got != 3.0 {
		t.Errorf("normal scaled a zombie's blow to %v, want 3", got)
	}
	if got := MobAttackAfterDifficulty(zombie, DifficultyHard); got != 4.5 {
		t.Errorf("hard scaled a zombie's blow to %v, want 4.5", got)
	}
	// The small blows easy folds whole: a half-heart hit lands a
	// half-heart, not a quarter.
	if got := MobAttackAfterDifficulty(1, DifficultyEasy); got != 1 {
		t.Errorf("easy scaled a small blow to %v, want it whole", got)
	}
}

// TestBlowFeedback checks the choice of a blow's sounds and animations
// against vanilla's Player.attack.
func TestBlowFeedback(t *testing.T) {
	cases := []struct {
		what string
		blow Blow
		want AttackFeedback
	}{
		{"a weak swing", Blow{Strength: 0.5, Critical: true, Sprinting: true, OnGround: true, Sword: true, Speed: 0.1}, AttackFeedback{}},
		{"a standing fist", Blow{Strength: 1, OnGround: true, Speed: 0.1}, AttackFeedback{Strong: true}},
		{"a falling blow", Blow{Strength: 1, Critical: true, Sword: true, Speed: 0.1}, AttackFeedback{Critical: true, Strong: true}},
		{"a sprinting blow", Blow{Strength: 1, Sprinting: true, OnGround: true, Sword: true, Speed: 0.1}, AttackFeedback{Knockback: true, Strong: true}},
		{"a standing sword", Blow{Strength: 1, OnGround: true, Sword: true, Speed: 0.1}, AttackFeedback{Sweep: true, Strong: true}},
		{"a walking sword", Blow{Strength: 1, OnGround: true, Sword: true, KnownSpeedSq: 0.07, Speed: 0.1}, AttackFeedback{Strong: true}},
		{"a walking sword at speed 0.1", Blow{Strength: 1, OnGround: true, Sword: true, KnownSpeedSq: 0.259 * 0.259, Speed: 0.1}, AttackFeedback{Strong: true}},
		{"a walking sword with Speed I", Blow{Strength: 1, OnGround: true, Sword: true, KnownSpeedSq: 0.259 * 0.259, Speed: 0.12}, AttackFeedback{Sweep: true, Strong: true}},
		{"an airborne sword", Blow{Strength: 1, Sword: true, Speed: 0.1}, AttackFeedback{Strong: true}},
		{"an enchanted weak swing", Blow{Strength: 0.2, MagicBoost: 0.1, Speed: 0.1}, AttackFeedback{Magic: true}},
	}
	for _, c := range cases {
		if got := c.blow.Feedback(); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.what, got, c.want)
		}
	}
}

// TestSweepDamage checks the hurt a swept body takes against vanilla's
// doSweepAttack: the plain one, and the raised share Sweeping Edge
// carries of the blow itself.
func TestSweepDamage(t *testing.T) {
	if got := SweepDamage(0, 6); got != 1 {
		t.Errorf("a sweep with no Sweeping Edge hurt %v, want the plain 1", got)
	}
	if got := SweepDamage(2.0/3.0, 6); math.Abs(got-5) > 1e-9 {
		t.Errorf("a sweep at two thirds of a six-point blow hurt %v, want 5", got)
	}
}

func TestBreachMakesArmourLessEffective(t *testing.T) {
	// 20 points against 10 damage and no toughness: 20 - 10/2 = 15 real
	// points, a fraction of 0.6.
	for _, c := range []struct{ delta, want float64 }{
		{0, 4},
		{-0.15, 5.5},
		{-0.6, 10},
		{-2, 10},
	} {
		if got := DamageAfterArmourEffective(10, 20, 0, c.delta); math.Abs(got-c.want) > 1e-6 {
			t.Errorf("delta %v = %v, want %v", c.delta, got, c.want)
		}
	}
}

func TestArmourFloat32Chain(t *testing.T) {
	cases := []struct {
		name string
		got  float64
		want float32
	}{
		{"netherite mob_attack", DamageAfterArmourEffective(15, 8, 3, 0), 6.527273},
		{"protection 4", DamageAfterEnchantmentProtection(10, 4), 11.599999},
		{"projectile protection 4", DamageAfterEnchantmentProtection(DamageAfterArmourEffective(12, 8, 2, 0), 8), 12.8844795},
		{"golden protection 2", DamageAfterEnchantmentProtection(DamageAfterArmourEffective(9, 5, 0, 0), 2), 12.051201},
	}
	for _, c := range cases {
		if got := float32(20 - c.got); got != c.want {
			t.Errorf("%s: health %v, want %v", c.name, got, c.want)
		}
	}
}
