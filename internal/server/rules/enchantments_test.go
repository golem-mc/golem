package rules

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The enchantment effects, checked against vanilla's own numbers: the values
// the data pack's level-based values compute at the levels the game applies,
// the conditions the damage contexts answer, and the queries the server's
// features run - the damage a weapon adds, the protection armour raises, the
// wear unbreaking spares. The names are resolved through the enchantment
// registry the server was sent, none of these tests willing to guess at a
// number the generated table sits behind.

// enchName resolves one enchantment by name, the tests failing rather than
// guessing when the registry has none.
func enchName(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.EnchantmentID(name)
	if !ok {
		t.Fatalf("the enchantment registry has no %s", name)
	}
	return id
}

// enchantedStack is one stack of an item carrying the one enchantment named,
// at the level given - the shape every stack in these tests takes.
func enchantedStack(t *testing.T, item, enchantment string, level int32) protocol.ItemStack {
	t.Helper()
	id, ok := registry.ItemID(item)
	if !ok {
		t.Fatalf("the item registry has no %s", item)
	}
	return protocol.ItemStack{
		ItemID:       id,
		Count:        1,
		Enchantments: map[int32]int32{enchName(t, enchantment): level},
	}
}

// meleeContext is the damage context of a player's own swing at the entity
// type named: direct, the player behind it and carrying it.
func meleeContext(victim int32) *EnchContext {
	return &EnchContext{
		DamageType:     DamageTypePlayerAttack,
		Direct:         true,
		Victim:         victim,
		Attacker:       protocol.EntityTypePlayer,
		DirectAttacker: protocol.EntityTypePlayer,
	}
}

// arrowContext is the damage context of a hit an arrow landed: not direct,
// the shooter behind it and the shaft carrying it, the body hit as the
// victim.
func arrowContext(victim int32) *EnchContext {
	return &EnchContext{
		DamageType:     DamageTypeArrow,
		Direct:         false,
		Victim:         victim,
		DirectAttacker: protocol.EntityTypeArrow,
	}
}

// TestSharpnessAddsItsLinearLevels checks the values sharpness's add effect
// computes, the vanilla linear's base of one and half a point a level above
// the first.
func TestSharpnessAddsItsLinearLevels(t *testing.T) {
	levels := map[int32]float64{1: 1, 2: 1.5, 3: 2, 4: 2.5, 5: 3}
	for level, want := range levels {
		stack := enchantedStack(t, "minecraft:iron_sword", "minecraft:sharpness", level)
		got := EnchantedDamage(stack, meleeContext(protocol.EntityTypeZombie), 6)
		if math.Abs(got-(6+want)) > 1e-9 {
			t.Errorf("sharpness %d on a six-point hit = %v, want %v", level, got, 6+want)
		}
	}
}

// TestSmiteAndBaneAnswerTheirEntityTags checks the conditions the two
// bane-damage enchantments carry: smite on the undead, bane of arthropods on
// the spiders, both at two and a half a level, and neither on a body outside
// the tag.
func TestSmiteAndBaneAnswerTheirEntityTags(t *testing.T) {
	smite := enchantedStack(t, "minecraft:iron_sword", "minecraft:smite", 5)
	if got := EnchantedDamage(smite, meleeContext(protocol.EntityTypeZombie), 6); got != 6+12.5 {
		t.Errorf("smite V on a zombie = %v, want six plus the five levels' twelve and a half", got)
	}
	if got := EnchantedDamage(smite, meleeContext(protocol.EntityTypeCow), 6); got != 6 {
		t.Errorf("smite V on a cow = %v, want the blow unchanged, the cow no undead", got)
	}
	bane := enchantedStack(t, "minecraft:iron_sword", "minecraft:bane_of_arthropods", 5)
	if got := EnchantedDamage(bane, meleeContext(protocol.EntityTypeSpider), 6); got != 6+12.5 {
		t.Errorf("bane of arthropods V on a spider = %v, want six plus twelve and a half", got)
	}
	if got := EnchantedDamage(bane, meleeContext(protocol.EntityTypeZombie), 6); got != 6 {
		t.Errorf("bane of arthropods V on a zombie = %v, want the blow unchanged", got)
	}
}

// TestPowerRidesTheArrow checks the damage context the bow's damage
// enchantment answers: power's add applies where the blow's direct attacker
// is an arrow, which a shaft's own hit is and a melee swing is not.
func TestPowerRidesTheArrow(t *testing.T) {
	bow := enchantedStack(t, "minecraft:bow", "minecraft:power", 5)
	if got := EnchantedDamage(bow, arrowContext(protocol.EntityTypeZombie), 2); got != 2+3 {
		t.Errorf("power V under an arrow = %v, want the two plus its three", got)
	}
	if got := EnchantedDamage(bow, meleeContext(protocol.EntityTypeZombie), 2); got != 2 {
		t.Errorf("power V under a swing = %v, want the two unchanged", got)
	}
}

// TestKnockbackAndPunchAddTheirStrength checks the knockback component: the
// melee knockback's add, and the punch that adds only where an arrow carried
// the blow. Both come back halved at the call, vanilla's getKnockback, and
// the fold the push makes of the two knockback calls is checked beside them.
func TestKnockbackAndPunchAddTheirStrength(t *testing.T) {
	sword := enchantedStack(t, "minecraft:iron_sword", "minecraft:knockback", 2)
	if got := EnchantedKnockback(sword, meleeContext(protocol.EntityTypePlayer), 0) / 2; got != 1.0 {
		t.Errorf("knockback II's extra = %v, want its two levels' value halved to one", got)
	}
	bow := enchantedStack(t, "minecraft:bow", "minecraft:punch", 2)
	if got := EnchantedKnockback(bow, arrowContext(protocol.EntityTypePlayer), 0) / 2; got != 1.0 {
		t.Errorf("punch II's extra = %v, want its two levels' value halved to one", got)
	}
	if got := EnchantedKnockback(bow, meleeContext(protocol.EntityTypePlayer), 0) / 2; got != 0 {
		t.Errorf("punch II under a swing = %v, want nothing, the arrow not carrying the blow", got)
	}
	// The fold: a hit from the west at a standing body, the attacker facing
	// the victim (yaw -90 runs the extra along +X too), the plain push's
	// four tenths, and the same hit with knockback I's extra of a half
	// folding to the two tenths the first shove's halving leaves plus the
	// extra - the seven tenths the vanilla packet carries.
	vx, vy, vz := KnockbackExtra(1, 0, -90, 0, 1, true, dice())
	if math.Abs(vx-0.4) > 1e-9 || vy != 0.4 || math.Abs(vz) > 1e-9 {
		t.Errorf("a plain push = (%v, %v, %v), want (0.4, 0.4, 0)", vx, vy, vz)
	}
	vx, vy, vz = KnockbackExtra(1, 0, -90, 0.5, 1, true, dice())
	if math.Abs(vx-0.7) > 1e-9 || vy != 0.4 || math.Abs(vz) > 1e-9 {
		t.Errorf("a knockback I push = (%v, %v, %v), want (0.7, 0.4, 0)", vx, vy, vz)
	}
	// The resistance's share comes off each call, not the pair's result:
	// against a body that keeps six tenths of a shove, the knockback I hit
	// folds to (0.2+0.5)*0.6 horizontally, its lift staying at the cap the
	// vanilla knockback keeps.
	vx, vy, vz = KnockbackExtra(1, 0, -90, 0.5, 0.6, true, dice())
	if math.Abs(vx-0.42) > 1e-9 || math.Abs(vy-0.4) > 1e-9 || math.Abs(vz) > 1e-9 {
		t.Errorf("a knockback I push on a resisting body = (%v, %v, %v), want (0.42, 0.4, 0)", vx, vy, vz)
	}
}

// TestKnockbackExtraShovesAlongTheAttackerFacing checks that the extra shove
// does not ride the away line but the attacker's facing: a hit from the west
// at a body the attacker looks south from gets the hurt's two tenths east
// and the attack's half south, the two adds vanilla's two knockback calls
// make.
func TestKnockbackExtraShovesAlongTheAttackerFacing(t *testing.T) {
	// Victim one east, attacker at yaw 0 (facing +Z).
	vx, vy, vz := KnockbackExtra(1, 0, 0, 0.5, 1, true, dice())
	if math.Abs(vx-0.2) > 1e-9 || math.Abs(vz-0.5) > 1e-9 || vy != 0.4 {
		t.Errorf("the folded push = (%v, %v, %v), want (0.2, 0.4, 0.5)", vx, vy, vz)
	}
	// The extra alone is the facing's own half, whatever way the two stand.
	vx, vy, vz = KnockbackExtraOnly(0, 0.5, 1, true)
	if math.Abs(vx) > 1e-9 || math.Abs(vz-0.5) > 1e-9 || vy != 0.4 {
		t.Errorf("the extra alone = (%v, %v, %v), want (0, 0.4, 0.5)", vx, vy, vz)
	}
}

// TestKnockbackExtraOnlyDealsJustTheExtra checks the single knockback call a
// softened hit still deals: the default knockback skipped, only the extra's
// own strength shoves along the attacker's facing, not the fold the two
// calls make together.
func TestKnockbackExtraOnlyDealsJustTheExtra(t *testing.T) {
	vx, vy, vz := KnockbackExtraOnly(-90, 0.5, 1, true)
	if math.Abs(vx-0.5) > 1e-9 || vy != 0.4 || math.Abs(vz) > 1e-9 {
		t.Errorf("the extra alone = (%v, %v, %v), want (0.5, 0.4, 0)", vx, vy, vz)
	}
	// A small extra lifts only as far as its own strength, under the cap.
	vx, vy, vz = KnockbackExtraOnly(-90, 0.1, 1, true)
	if math.Abs(vx-0.1) > 1e-9 || math.Abs(vy-0.1) > 1e-9 || math.Abs(vz) > 1e-9 {
		t.Errorf("a small extra alone = (%v, %v, %v), want (0.1, 0.1, 0)", vx, vy, vz)
	}
}

// TestProtectionSumsTheWornPieces checks the damage-protection component:
// the points the worn pieces raise against a damage type their conditions
// pass, the tags answering the way the damage types carry them.
func TestProtectionSumsTheWornPieces(t *testing.T) {
	protection := enchantedStack(t, "minecraft:diamond_chestplate", "minecraft:protection", 4)
	pieces := []EnchPiece{{Stack: protection, Slot: "chest"}}
	ctx := meleeContext(protocol.EntityTypePlayer)

	// Protection IV raises four of its one a level against a sword's
	// blow, whose damage type is neither the void nor the kill.
	if got := DamageProtection(pieces, ctx, nil); got != 4 {
		t.Errorf("protection IV's points = %v, want four", got)
	}
	// The same piece raises nothing against the void, the tag the
	// condition refuses the damage types of.
	voidCtx := &EnchContext{DamageType: DamageTypeOutOfWorld, Victim: protocol.EntityTypePlayer}
	if got := DamageProtection(pieces, voidCtx, nil); got != 0 {
		t.Errorf("protection IV against the void = %v, want nothing", got)
	}

	// Feather falling answers only the falls, and only on the feet.
	feather := enchantedStack(t, "minecraft:diamond_boots", "minecraft:feather_falling", 4)
	fallCtx := &EnchContext{DamageType: DamageTypeFall, Victim: protocol.EntityTypePlayer}
	if got := DamageProtection([]EnchPiece{{Stack: feather, Slot: "feet"}}, fallCtx, nil); got != 12 {
		t.Errorf("feather falling IV's points against a fall = %v, want twelve", got)
	}
	if got := DamageProtection([]EnchPiece{{Stack: feather, Slot: "feet"}}, ctx, nil); got != 0 {
		t.Errorf("feather falling IV against a sword = %v, want nothing, no fall in the blow", got)
	}

	// Fire protection answers the fire damage types and no others; the
	// blast and the projectile protections their own.
	fire := enchantedStack(t, "minecraft:diamond_helmet", "minecraft:fire_protection", 4)
	fireCtx := &EnchContext{DamageType: DamageTypeInFire, Victim: protocol.EntityTypePlayer}
	if got := DamageProtection([]EnchPiece{{Stack: fire, Slot: "head"}}, fireCtx, nil); got != 8 {
		t.Errorf("fire protection IV's points against fire = %v, want eight", got)
	}
	if got := DamageProtection([]EnchPiece{{Stack: fire, Slot: "head"}}, fallCtx, nil); got != 0 {
		t.Errorf("fire protection IV against a fall = %v, want nothing", got)
	}
	blast := enchantedStack(t, "minecraft:diamond_leggings", "minecraft:blast_protection", 4)
	blastCtx := &EnchContext{DamageType: DamageTypeExplosion, Victim: protocol.EntityTypePlayer}
	if got := DamageProtection([]EnchPiece{{Stack: blast, Slot: "legs"}}, blastCtx, nil); got != 8 {
		t.Errorf("blast protection IV's points against a blast = %v, want eight", got)
	}
	projectile := enchantedStack(t, "minecraft:diamond_helmet", "minecraft:projectile_protection", 2)
	arrowCtx := &EnchContext{DamageType: DamageTypeArrow, Victim: protocol.EntityTypePlayer}
	if got := DamageProtection([]EnchPiece{{Stack: projectile, Slot: "head"}}, arrowCtx, nil); got != 4 {
		t.Errorf("projectile protection II's points against an arrow = %v, want four", got)
	}
}

// TestEnchantmentProtectionIsCappedAtTwenty checks the cap vanilla's
// CombatRules puts the points under: twenty of them the most any blow is cut
// by, a twenty-fifth a point.
func TestEnchantmentProtectionIsCappedAtTwenty(t *testing.T) {
	// A full suit of protection IV raises sixty-four points, the cap
	// taking twenty of the twenty-fifths off.
	suit := make([]EnchPiece, 0, 4)
	for _, item := range []string{
		"minecraft:diamond_helmet", "minecraft:diamond_chestplate",
		"minecraft:diamond_leggings", "minecraft:diamond_boots",
	} {
		suit = append(suit, EnchPiece{Stack: enchantedStack(t, item, "minecraft:protection", 4), Slot: "chest"})
	}
	suit[0].Slot, suit[1].Slot, suit[2].Slot, suit[3].Slot = "head", "chest", "legs", "feet"
	if got := DamageProtection(suit, meleeContext(protocol.EntityTypePlayer), nil); got != 16 {
		t.Errorf("a protection IV suit's points = %v, want sixteen, its one a level a piece", got)
	}
	if got := DamageAfterEnchantmentProtection(10, 16); math.Abs(got-3.6) > 1e-5 {
		t.Errorf("a ten-point blow under a full protection IV suit = %v, want the ten minus its sixteen twenty-fifths, three and six tenths", got)
	}
	// The cap: a suit each piece of which raises its twenty takes the
	// twenty of twenty-fifths whatever the sum was.
	if got := DamageAfterEnchantmentProtection(10, 64); math.Abs(got-2) > 1e-5 {
		t.Errorf("a ten-point blow under sixty-four points of protection = %v, want the capped two", got)
	}
}

// TestUnbreakingSparesTheWearByItsRoll checks the item_damage component:
// unbreaking's remove_binomial, the tool's chance of level over level-plus-
// one and the armour's of two levels over five levels plus five, each point
// of wear kept only where the roll keeps it.
func TestUnbreakingSparesTheWearByItsRoll(t *testing.T) {
	pickaxe := enchantedStack(t, "minecraft:iron_pickaxe", "minecraft:unbreaking", 3)
	helmet := enchantedStack(t, "minecraft:diamond_helmet", "minecraft:unbreaking", 3)

	// The rolls are the test's to place. Vanilla's remove_binomial drops
	// each point of wear where its roll comes up under the chance - the
	// roll sparing the point, the wear arriving only where the roll said
	// keep. The tool's chance is level over level-plus-one, three
	// quarters at level three, the armour's two levels over five levels
	// plus five, three tenths there. A seed whose first roll lands under
	// three tenths drops both their points, one at or above three
	// quarters keeps both.
	spared := findSeedBelow(t, []float64{0.3})
	taken := findSeedAbove(t, []float64{0.75})

	if got := ProcessDurabilityChange(pickaxe, 1, rand.New(rand.NewSource(spared))); got != 0 {
		t.Errorf("unbreaking III's wear on a spared roll = %v, want the point dropped", got)
	}
	if got := ProcessDurabilityChange(pickaxe, 1, rand.New(rand.NewSource(taken))); got != 1 {
		t.Errorf("unbreaking III's wear on a kept roll = %v, want the point taken", got)
	}
	// The armour's chance is the smaller one; the same dice, the same two
	// outcomes, the roll the only difference between the two.
	if got := ProcessDurabilityChange(helmet, 1, rand.New(rand.NewSource(spared))); got != 0 {
		t.Errorf("unbreaking III's wear on spared armour = %v, want the point dropped", got)
	}
	if got := ProcessDurabilityChange(helmet, 1, rand.New(rand.NewSource(taken))); got != 1 {
		t.Errorf("unbreaking III's wear on kept armour = %v, want the point taken", got)
	}
}

// TestUnbreakableStacksTakeNoDurabilityChange checks the damageable gate on
// the other side: an unbreakable stack, enchanted or not, comes back with no
// wear to take whatever the dice would have kept.
func TestUnbreakableStacksTakeNoDurabilityChange(t *testing.T) {
	kept := findSeedAbove(t, []float64{0.75})
	pickaxe := enchantedStack(t, "minecraft:iron_pickaxe", "minecraft:unbreaking", 3)
	pickaxe.Unbreakable = true
	if got := ProcessDurabilityChange(pickaxe, 1, rand.New(rand.NewSource(kept))); got != 0 {
		t.Errorf("unbreaking III's wear on an unbreakable pickaxe = %v, want none", got)
	}
	plain := protocol.ItemStack{ItemID: pickaxe.ItemID, Count: 1, Unbreakable: true}
	if got := ProcessDurabilityChange(plain, 1, rand.New(rand.NewSource(kept))); got != 0 {
		t.Errorf("wear on a plain unbreakable pickaxe = %v, want none", got)
	}
}

// findSeedBelow walks the seeds until the first roll of one lands below
// every bound given, the first Float64 of the source being the roll a single
// point of wear is kept or taken by. The seeds are stable, the algorithm
// behind the dice being the library's own.
func findSeedBelow(t *testing.T, bounds []float64) int64 {
	t.Helper()
	for seed := range int64(100000) {
		r := rand.New(rand.NewSource(seed))
		ok := true
		for _, bound := range bounds {
			if r.Float64() >= bound {
				ok = false
				break
			}
		}
		if ok {
			return seed
		}
	}
	t.Fatal("no seed rolls below the bounds")
	return 0
}

// findSeedAbove is findSeedBelow the other way round.
func findSeedAbove(t *testing.T, bounds []float64) int64 {
	t.Helper()
	for seed := range int64(100000) {
		r := rand.New(rand.NewSource(seed))
		ok := true
		for _, bound := range bounds {
			if r.Float64() < bound {
				ok = false
				break
			}
		}
		if ok {
			return seed
		}
	}
	t.Fatal("no seed rolls above the bounds")
	return 0
}

// TestAmmoUseSpendsTheArrow checks the ammo_use component: infinity's set
// bringing a plain arrow's cost to nothing, the match_tool condition seeing
// the ammunition and nothing else.
func TestAmmoUseSpendsTheArrow(t *testing.T) {
	infinity := enchantedStack(t, "minecraft:bow", "minecraft:infinity", 1)
	arrowID, ok := registry.ItemID("minecraft:arrow")
	if !ok {
		t.Fatal("the item registry has no arrow")
	}
	if got := ProcessAmmoUse(infinity, protocol.ItemStack{ItemID: arrowID, Count: 1}); got != 0 {
		t.Errorf("infinity's cost of a plain arrow = %v, want nothing", got)
	}
	spectralID, ok := registry.ItemID("minecraft:spectral_arrow")
	if ok {
		if got := ProcessAmmoUse(infinity, protocol.ItemStack{ItemID: spectralID, Count: 1}); got != 1 {
			t.Errorf("infinity's cost of a spectral arrow = %v, want the plain one", got)
		}
	}
	if got := ProcessAmmoUse(enchantedStack(t, "minecraft:bow", "minecraft:power", 1), protocol.ItemStack{ItemID: arrowID, Count: 1}); got != 1 {
		t.Errorf("a bow without infinity's cost of an arrow = %v, want the plain one", got)
	}
}

// TestFireAspectIgnitesForItsSeconds checks the post-attack ignite: four
// seconds a level, floored to the ticks of burn the vanilla igniteForSeconds
// grants, and only on the direct blow the condition asks for.
func TestFireAspectIgnitesForItsSeconds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		level int32
		ctx   *EnchContext
		want  int32
	}{
		{"I", 1, meleeContext(protocol.EntityTypeZombie), 80},
		{"II", 2, meleeContext(protocol.EntityTypeZombie), 160},
		{"indirect", 2, arrowContext(protocol.EntityTypeZombie), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			weapon := enchantedStack(t, "minecraft:iron_sword", "minecraft:fire_aspect", tc.level)
			actions := PostAttackEffects(weapon, nil, tc.ctx, nil)
			if tc.want == 0 {
				if len(actions) != 0 {
					t.Fatalf("indirect blow produced %+v", actions)
				}
				return
			}
			if len(actions) != 1 {
				t.Fatalf("fire aspect produced %+v, want one action", actions)
			}
			a := actions[0]
			if a.Target != "victim" || a.Piece != -1 || a.Effect.Duration == nil {
				t.Fatalf("fire aspect action = %+v, want weapon ignition of victim", a)
			}
			if got := IgniteTicks(enchLevelValue(a.Effect.Duration, a.Level)); got != tc.want {
				t.Errorf("ignite = %d ticks, want %d", got, tc.want)
			}
		})
	}
}

// TestFlameIgnitesTheShaft checks the projectile-spawned ignite: the hundred
// seconds of burning the flame bow's arrow arrives in the world with.
func TestFlameIgnitesTheShaft(t *testing.T) {
	bow := enchantedStack(t, "minecraft:bow", "minecraft:flame", 1)
	if got := ProjectileIgniteTicks(bow); got != 2000 {
		t.Errorf("flame's ignite = %v ticks, want the hundred seconds' two thousand", got)
	}
	if got := ProjectileIgniteTicks(enchantedStack(t, "minecraft:bow", "minecraft:power", 1)); got != 0 {
		t.Errorf("a power bow's ignite = %v ticks, want none", got)
	}
}

// TestBaneOfArthropodsAppliesItsSlowness checks the post-attack
// apply_mob_effect bane of arthropods III carries: a Slowness IV on the
// spider its blow lands on, thirty to fifty ticks at the level, the instance
// a three-argument MobEffectInstance builds; nothing on a body outside the
// tag, nothing where the blow was not direct, and no fire from an effect
// that carries none.
func TestBaneOfArthropodsAppliesItsSlowness(t *testing.T) {
	sword := enchantedStack(t, "minecraft:iron_sword", "minecraft:bane_of_arthropods", 3)
	spider := meleeContext(protocol.EntityTypeSpider)
	slowness := registry.MustMobEffect("minecraft:slowness")
	for seed := range int64(25) {
		rng := rand.New(rand.NewSource(seed))
		actions := PostAttackEffects(sword, nil, spider, rng)
		if len(actions) != 1 || actions[0].Target != "victim" || actions[0].Piece != -1 {
			t.Fatalf("bane III actions = %+v, want one weapon action on victim", actions)
		}
		a := actions[0]
		if a.Effect.Duration != nil {
			t.Errorf("bane III has an ignite duration: %+v", a.Effect.Duration)
		}
		got := a.MobEffects(rng)
		if len(got) != 1 {
			t.Fatalf("bane III on a spider with seed %d gave %+v, want the one effect", seed, got)
		}
		if got[0].ID != slowness || got[0].Amplifier != 3 ||
			got[0].Duration < 30 || got[0].Duration > 50 {
			t.Errorf("bane III's slowness with seed %d = %+v, want Slowness IV for the 30 to 50 of a level III roll",
				seed, got[0])
		}
		if got[0].Ambient || !got[0].Visible || !got[0].ShowIcon {
			t.Errorf("bane III's slowness with seed %d = %+v, want the plain instance, its particles and icon shown",
				seed, got[0])
		}
	}
	if got := PostAttackEffects(sword, nil, meleeContext(protocol.EntityTypeZombie), rand.New(rand.NewSource(0))); len(got) != 0 {
		t.Errorf("bane III on a zombie gave %+v, want nothing, the zombie no arthropod", got)
	}
	if got := PostAttackEffects(sword, nil, arrowContext(protocol.EntityTypeSpider), rand.New(rand.NewSource(0))); len(got) != 0 {
		t.Errorf("bane III under an arrow gave %+v, want nothing, the blow not direct", got)
	}
}

// TestSweepingEdgeRaisesTheRatio checks the sweeping_damage_ratio the
// held blade carries Sweeping Edge's levels at: vanilla's fraction, half
// the share of the blow at the first level and three quarters at the
// third — the mainhand the only slot it works in.
func TestSweepingEdgeRaisesTheRatio(t *testing.T) {
	edge := enchantedStack(t, "minecraft:iron_sword", "minecraft:sweeping_edge", 1)
	if got := EquipmentAttributeValue([]EnchPiece{{Stack: edge, Slot: "mainhand"}}, registry.AttributeSweepingDamageRatio); got != 0.5 {
		t.Errorf("sweeping edge I's ratio = %v, want half the blow", got)
	}
	edge = enchantedStack(t, "minecraft:iron_sword", "minecraft:sweeping_edge", 3)
	if got := EquipmentAttributeValue([]EnchPiece{{Stack: edge, Slot: "mainhand"}}, registry.AttributeSweepingDamageRatio); got != 0.75 {
		t.Errorf("sweeping edge III's ratio = %v, want three quarters of the blow", got)
	}
	// The off hand's edge raises no sweep of the main hand's blow.
	if got := EquipmentAttributeValue([]EnchPiece{{Stack: edge, Slot: "offhand"}}, registry.AttributeSweepingDamageRatio); got != 0 {
		t.Errorf("sweeping edge III in the off hand = %v, want nothing, the main hand carrying the blow", got)
	}
}

// TestEnchantmentAttributesSum checks the attribute modifiers: respiration's
// oxygen bonus a level a piece, efficiency's level squared plus one, and the
// multipliers the aqua affinity carries.
func TestEnchantmentAttributesSum(t *testing.T) {
	respiration := enchantedStack(t, "minecraft:diamond_helmet", "minecraft:respiration", 3)
	got := EquipmentAttributeValue([]EnchPiece{{Stack: respiration, Slot: "head"}}, registry.AttributeOxygenBonus)
	if got != 3 {
		t.Errorf("respiration III's oxygen bonus = %v, want its three levels", got)
	}
	// The head slot is the only one the enchantment works in.
	if got := EquipmentAttributeValue([]EnchPiece{{Stack: respiration, Slot: "chest"}}, registry.AttributeOxygenBonus); got != 0 {
		t.Errorf("respiration III worn on the chest = %v, want nothing, no head to breathe with", got)
	}
	efficiency := enchantedStack(t, "minecraft:iron_pickaxe", "minecraft:efficiency", 5)
	got = EquipmentAttributeValue([]EnchPiece{{Stack: efficiency, Slot: "mainhand"}}, registry.AttributeMiningEfficiency)
	if got != 26 {
		t.Errorf("efficiency V's mining efficiency = %v, want the level squared plus one, twenty-six", got)
	}
	aqua := enchantedStack(t, "minecraft:diamond_helmet", "minecraft:aqua_affinity", 1)
	got = EquipmentAttributeValue([]EnchPiece{{Stack: aqua, Slot: "head"}}, registry.AttributeSubmergedMiningSpeed)
	if math.Abs(got-1) > 1e-6 {
		t.Errorf("aqua affinity's submerged mining speed = %v, want the fourfold multiplier over the base's two tenths, one", got)
	}
}

// TestTheEnchantmentTableCoversTheRegistry checks the generated table
// against the registry it sits behind: every enchantment the registry sends
// has an effects entry, and every entry names an enchantment the registry
// carries - a regeneration that dropped one, or grew one the server does
// not register, failing here rather than on the wire.
func TestTheEnchantmentTableCoversTheRegistry(t *testing.T) {
	r, ok := registry.Get("minecraft:enchantment")
	if !ok {
		t.Fatal("no minecraft:enchantment registry")
	}
	for _, name := range r.Entries {
		id, ok := registry.EnchantmentID(name)
		if !ok {
			t.Errorf("the registry's %s has no effects entry", name)
			continue
		}
		if _, ok := registry.EnchantmentEffectsByID(id); !ok {
			t.Errorf("the registry's %s has no effects entry", name)
		}
	}
	for i := range registry.EnchantmentEffectsTable {
		name := registry.EnchantmentEffectsTable[i].Name
		if id, ok := registry.EnchantmentID(name); !ok || id != int32(i) {
			t.Errorf("the table's %s is at %d, the registry carrying it at %d", name, i, id)
		}
	}
}

func TestChannelingEffectsRequireAThrownTrident(t *testing.T) {
	weapon := enchantedStack(t, "minecraft:trident", "minecraft:channeling", 1)
	for _, tc := range []struct {
		name  string
		block bool
		ctx   EnchContext
		want  bool
	}{
		{name: "entity", ctx: EnchContext{Thundering: true, CanSeeSky: true, Victim: protocol.EntityTypeCow, DirectAttacker: protocol.EntityTypeTrident}, want: true},
		{name: "melee", ctx: EnchContext{Thundering: true, CanSeeSky: true, Victim: protocol.EntityTypeCow, DirectAttacker: protocol.EntityTypePlayer}},
		{name: "arrow", ctx: EnchContext{Thundering: true, CanSeeSky: true, Victim: protocol.EntityTypeCow, DirectAttacker: protocol.EntityTypeArrow}},
		{name: "clear", ctx: EnchContext{CanSeeSky: true, Victim: protocol.EntityTypeCow, DirectAttacker: protocol.EntityTypeTrident}},
		{name: "shade", ctx: EnchContext{Thundering: true, Victim: protocol.EntityTypeCow, DirectAttacker: protocol.EntityTypeTrident}},
		{name: "rod", block: true, ctx: EnchContext{Thundering: true, CanSeeSky: true, Victim: protocol.EntityTypeTrident, Block: registry.BlockLightningRod}, want: true},
		{name: "waxed rod", block: true, ctx: EnchContext{Thundering: true, CanSeeSky: true, Victim: protocol.EntityTypeTrident, Block: registry.BlockWaxedOxidizedLightningRod}, want: true},
		{name: "stone", block: true, ctx: EnchContext{Thundering: true, CanSeeSky: true, Victim: protocol.EntityTypeTrident, Block: registry.BlockStone}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := TridentHitEffects(weapon, &tc.ctx, tc.block)
			if (len(got) > 0) != tc.want {
				t.Fatalf("effects=%v, want match %v", got, tc.want)
			}
			if tc.want && (len(got) != 1 || got[0].SummonEntity != "minecraft:lightning_bolt" || got[0].PlaySound == nil || got[0].PlaySound.Volume != 5 || got[0].PlaySound.Pitch != 1) {
				t.Errorf("effects=%+v, want lightning and thunder at volume 5, pitch 1", got)
			}
		})
	}
}

func TestPostAttackWindBurstRequirementsAndLevels(t *testing.T) {
	for _, tc := range []struct {
		name   string
		level  int32
		fall   float64
		flying bool
		want   float64
	}{
		{"I", 1, 3, false, float64(float32(1.2))},
		{"II", 2, 3, false, 1.75},
		{"III", 3, 3, false, float64(float32(2.2))},
		{"fallback", 4, 3, false, float64(float32(1.5) + float32(float32(0.35)*3))},
		{"threshold", 1, 1.5, false, float64(float32(1.2))},
		{"short", 1, 1.49, false, 0},
		{"flying", 1, 3, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := meleeContext(protocol.EntityTypeZombie)
			ctx.DirectAttackerFlying, ctx.DirectAttackerFallDistance = tc.flying, tc.fall
			actions := PostAttackEffects(enchantedStack(t, "minecraft:mace", "minecraft:wind_burst", tc.level), nil, ctx, nil)
			if tc.want == 0 {
				if len(actions) != 0 {
					t.Fatalf("unmet requirements produced %+v", actions)
				}
				return
			}
			if len(actions) != 1 || actions[0].Target != "attacker" || actions[0].Piece != -1 {
				t.Fatalf("wind burst actions = %+v, want one weapon action on attacker", actions)
			}
			got, ok := actions[0].Explosion()
			if !ok || got.Radius != 3.5 || got.Knockback != tc.want || got.Interaction != ExplosionTrigger {
				t.Fatalf("explosion = %+v (supported=%v), want radius 3.5, knockback %v, trigger", got, ok, tc.want)
			}
		})
	}
}

// TestChargingAndTridentSoundsPickTheEnchantmentsLevel checks the sound lists
// the stack's highest enchantment picks by level, and the plain defaults.
// vanilla: EnchantmentHelper.pickHighestLevel
func TestChargingAndTridentSoundsPickTheEnchantmentsLevel(t *testing.T) {
	plain := protocol.ItemStack{}
	if got := ChargingSounds(plain); got.Start != registry.SoundItemCrossbowLoadingStart.Holder() || got.Mid != registry.SoundItemCrossbowLoadingMiddle.Holder() {
		t.Errorf("plain charging sounds = %+v", got)
	}
	if got := TridentSound(plain); got != registry.SoundItemTridentThrow.Holder() {
		t.Errorf("plain trident sound = %+v", got)
	}
	quick := enchantedStack(t, "minecraft:crossbow", "minecraft:quick_charge", 2)
	if got := ChargingSounds(quick); got.Start != registry.SoundItemCrossbowQuickCharge2.Holder() || got.Mid.ID != -1 || got.End != registry.SoundItemCrossbowLoadingEnd.Holder() {
		t.Errorf("quick charge 2 sounds = %+v", got)
	}
	if got := TridentSound(enchantedStack(t, "minecraft:trident", "minecraft:riptide", 3)); got != registry.SoundItemTridentRiptide3.Holder() {
		t.Errorf("riptide 3 sound = %+v", got)
	}
}
