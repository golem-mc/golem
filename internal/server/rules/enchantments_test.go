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
