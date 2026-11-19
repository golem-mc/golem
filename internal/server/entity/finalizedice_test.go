package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// regionDice is chunk (0, 0)'s WorldGenRegion random at level seed 0.
func regionDice() levelgen.RandomSource {
	return levelgen.NewXoroshiroNamedRandom(0, "minecraft:worldgen_region_random").At(0, 0, 0)
}

// TestFinalizeDrawsFarmAnimalsFromTheSpawnDice finalizes a sheep, a cow and
// a pig pack's two members on one region random and checks each field
// against a second copy of that random, drawn in vanilla's order: the
// kind's own fields, then AgeableMob's baby roll (none for a pack's first
// member), then Mob's follow-range triangle and left-handed roll.
// vanilla: Sheep, Cow, Pig.finalizeSpawn, AgeableMob.finalizeSpawn, Mob.finalizeSpawn
func TestFinalizeDrawsFarmAnimalsFromTheSpawnDice(t *testing.T) {
	dice, want := regionDice(), regionDice()
	plains := testBiome("minecraft:plains")
	sheep := NewMob(TypeSheep, 1, 0.5, 64, 0.5, 1)
	sheep.FinalizeSpawn(dice, plains, nil, rules.LocalDifficulty{})
	colour := "white"
	switch roll := want.NextIntN(100); {
	case roll < 5:
		colour = "black"
	case roll < 10:
		colour = "gray"
	case roll < 15:
		colour = "light_gray"
	case roll < 18:
		colour = "brown"
	default:
		if want.NextIntN(500) == 499 {
			colour = "pink"
		}
	}
	if sheep.Colour != colour {
		t.Errorf("sheep colour = %s, want %s", sheep.Colour, colour)
	}
	checkMobStep(t, want, sheep)

	cow := NewMob(TypeCow, 2, 0.5, 64, 0.5, 2)
	cow.FinalizeSpawn(dice, plains, nil, rules.LocalDifficulty{})
	want.NextIntN(1) // the one temperate variant plains offers
	if cow.Variant != "minecraft:temperate" {
		t.Errorf("cow variant = %s, want minecraft:temperate", cow.Variant)
	}
	if sound := soundVariantOf(want, "minecraft:cow_sound_variant"); cow.SoundVariant != sound {
		t.Errorf("cow sound variant = %s, want %s", cow.SoundVariant, sound)
	}
	checkMobStep(t, want, cow)

	group := NewSpawnGroup(dice)
	for i, baby := range []bool{false, true} {
		pig := NewMob(TypePig, int32(3+i), 0.5, 64, 0.5, int64(3+i))
		pig.FinalizeSpawn(dice, plains, group, rules.LocalDifficulty{})
		want.NextIntN(1)
		if pig.Variant != "minecraft:temperate" {
			t.Errorf("pig %d variant = %s, want minecraft:temperate", i, pig.Variant)
		}
		if sound := soundVariantOf(want, "minecraft:pig_sound_variant"); pig.SoundVariant != sound {
			t.Errorf("pig %d sound variant = %s, want %s", i, pig.SoundVariant, sound)
		}
		if baby {
			if b := want.NextFloat() <= 0.05; pig.IsBaby() != b {
				t.Errorf("second pig baby = %v, want %v", pig.IsBaby(), b)
			}
		} else if pig.IsBaby() {
			t.Error("a pack's first pig came out a baby")
		}
		checkMobStep(t, want, pig)
	}
	if got, w := dice.NextLong(), want.NextLong(); got != w {
		t.Errorf("the finalizes left the dice at %d, want %d: a draw too many or too few", got, w)
	}
}

// TestRandomSpawnBonusSavesAndLoads holds the follow_range bonus through a
// save and a load, for an animal's list and beside a zombie's own bonus,
// without waking an attribute map, and checks it scales the follow range.
func TestRandomSpawnBonusSavesAndLoads(t *testing.T) {
	for _, kind := range []MobType{TypeCow, TypeZombie, TypeHorse} {
		m := NewMob(kind, 1, 0.5, 64, 0.5, 1)
		m.FinalizeSpawn(regionDice(), testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
		if m.followSpawnBonus == 0 {
			t.Fatalf("%v: finalize rolled no follow-range bonus", kind)
		}
		tag := nbt.Compound{}
		m.SaveTo(tag, nil)
		follow, ok := followRangeEntry(tag)
		if !ok {
			t.Fatalf("%v: saved attributes %v hold no follow_range entry", kind, tag["attributes"])
		}
		if got := modifierAmount(follow, modRandomSpawn); got != m.followSpawnBonus {
			t.Errorf("%v: saved %s = %v, want %v", kind, modRandomSpawn, got, m.followSpawnBonus)
		}
		back := NewMob(kind, 2, 0.5, 64, 0.5, 2)
		back.LoadFrom(tag, nil, nil)
		if back.followSpawnBonus != m.followSpawnBonus {
			t.Errorf("%v: loaded bonus %v, want %v", kind, back.followSpawnBonus, m.followSpawnBonus)
		}
		if back.Attributes() != nil {
			t.Errorf("%v: the saved bonus woke an attribute map", kind)
		}
		want := back.BaseFollowRange() * (1 + back.followSpawnBonus) * (1 + back.zombie.FollowBonus)
		if got := back.followRange(); math.Abs(got-want) > 1e-12 {
			t.Errorf("%v: follow range %v, want %v", kind, got, want)
		}
		if a := back.BuildAttributes(nbt.List{}); math.Abs(a.Value(registry.AttributeFollowRange)-want) > 1e-9 {
			t.Errorf("%v: map follow range %v, want %v", kind, a.Value(registry.AttributeFollowRange), want)
		}
	}
}

// checkMobStep checks Mob.finalizeSpawn's follow-range triangle and
// left-handed roll against want.
func checkMobStep(t *testing.T, want levelgen.RandomSource, m *Mob) {
	t.Helper()
	bonus := followSpawnSpread * (want.NextDouble() - want.NextDouble())
	if m.followSpawnBonus != bonus {
		t.Errorf("%v follow-range bonus = %v, want %v", m.Type, m.followSpawnBonus, bonus)
	}
	if left := want.NextFloat() < 0.05; m.LeftHanded != left {
		t.Errorf("%v left-handed = %v, want %v", m.Type, m.LeftHanded, left)
	}
}

// soundVariantOf draws a sound variant of the named registry from want.
// vanilla: VariantUtils.pickRandomSoundVariant
func soundVariantOf(want levelgen.RandomSource, name string) string {
	r, _ := registry.Get(name)
	return r.Entries[want.NextIntN(int32(len(r.Entries)))]
}

// TestFinalizeDrawsOverworldCreaturesFromTheSpawnDice finalizes a pack's
// first and second member of each remaining overworld creature on one region
// random, and checks every field each draws against a second copy of that
// random, in vanilla's order: the kind's own finalize, AgeableMob's baby roll
// at the pack's odds (none for a first member, a parrot, a fox or a wolf),
// then Mob's step.
// vanilla: each kind's finalizeSpawn, AbstractHorse.finalizeSpawn,
// AgeableMob.finalizeSpawn, Mob.finalizeSpawn
func TestFinalizeDrawsOverworldCreaturesFromTheSpawnDice(t *testing.T) {
	const noBaby = -1
	horseHealth := func(want levelgen.RandomSource) float64 {
		return 15 + float64(want.NextIntN(8)) + float64(want.NextIntN(9))
	}
	threeDoubles := func(want levelgen.RandomSource, base, step float64) float64 {
		return base + want.NextDouble()*step + want.NextDouble()*step + want.NextDouble()*step
	}
	var firstRabbit, firstHorseColour, firstLlama int32
	for _, c := range []struct {
		name  string
		kind  MobType
		biome string
		baby  float32 // AgeableMob's odds for the second member, or noBaby
		// own checks the kind's own finalize of member i against want.
		own func(t *testing.T, want levelgen.RandomSource, m *Mob, i int)
	}{
		{"rabbit", TypeRabbit, "minecraft:flower_forest", 1, func(t *testing.T, want levelgen.RandomSource, m *Mob, i int) {
			// vanilla: Rabbit.getRandomRabbitVariant draws before the group check
			v := int32(rabbitBlack)
			switch roll := want.NextIntN(100); {
			case roll < 50:
				v = rabbitBrown
			case roll < 90:
				v = rabbitSalt
			}
			if i == 0 {
				firstRabbit = v
			} else {
				v = firstRabbit
			}
			if m.RabbitType != v {
				t.Errorf("rabbit %d type = %d, want %d", i, m.RabbitType, v)
			}
		}},
		{"wolf", TypeWolf, "minecraft:forest", noBaby, func(t *testing.T, want levelgen.RandomSource, m *Mob, i int) {
			if i == 0 {
				want.NextIntN(1) // the one woods variant forest offers
			}
			if m.Variant != "minecraft:woods" {
				t.Errorf("wolf %d variant = %s, want minecraft:woods", i, m.Variant)
			}
			if s := soundVariantOf(want, "minecraft:wolf_sound_variant"); m.SoundVariant != s {
				t.Errorf("wolf %d sound variant = %s, want %s", i, m.SoundVariant, s)
			}
		}},
		{"fox", TypeFox, "minecraft:taiga", noBaby, func(t *testing.T, want levelgen.RandomSource, m *Mob, i int) {
			if m.Fox.Variant != foxRed {
				t.Errorf("fox %d variant = %d, want red", i, m.Fox.Variant)
			}
			var item int32
			if want.NextFloat() < 0.2 {
				switch odds := want.NextFloat(); {
				case odds < 0.05:
					item = int32(registry.ItemEmerald)
				case odds < 0.2:
					item = int32(registry.ItemEgg)
				case odds < 0.4:
					item = int32(registry.ItemRabbitHide)
					if want.NextBoolean() {
						item = int32(registry.ItemRabbitFoot)
					}
				case odds < 0.6:
					item = int32(registry.ItemWheat)
				case odds < 0.8:
					item = int32(registry.ItemLeather)
				default:
					item = int32(registry.ItemFeather)
				}
			}
			if m.MainHand.ItemID != item {
				t.Errorf("fox %d holds %d, want %d", i, m.MainHand.ItemID, item)
			}
		}},
		{"horse", TypeHorse, "minecraft:plains", 0.05, func(t *testing.T, want levelgen.RandomSource, m *Mob, i int) {
			if i == 0 {
				firstHorseColour = want.NextIntN(7)
			}
			if v := firstHorseColour | want.NextIntN(5)<<8; m.Horse.Variant != v {
				t.Errorf("horse %d variant = %#x, want %#x", i, m.Horse.Variant, v)
			}
			if h := horseHealth(want); m.MaxHealth != h {
				t.Errorf("horse %d max health = %v, want %v", i, m.MaxHealth, h)
			}
			if s := threeDoubles(want, float64(float32(0.45)), 0.3) * 0.25; m.Horse.Speed != s {
				t.Errorf("horse %d speed = %v, want %v", i, m.Horse.Speed, s)
			}
			if j := threeDoubles(want, float64(float32(0.4)), 0.2); m.Horse.Jump != j {
				t.Errorf("horse %d jump = %v, want %v", i, m.Horse.Jump, j)
			}
		}},
		{"donkey", TypeDonkey, "minecraft:plains", 0.2, func(t *testing.T, want levelgen.RandomSource, m *Mob, i int) {
			if h := horseHealth(want); m.MaxHealth != h {
				t.Errorf("donkey %d max health = %v, want %v", i, m.MaxHealth, h)
			}
		}},
		{"llama", TypeLlama, "minecraft:savanna_plateau", 0.05, func(t *testing.T, want levelgen.RandomSource, m *Mob, i int) {
			bound := int32(3)
			if want.NextFloat() < 0.04 {
				bound = 5
			}
			if s := 1 + want.NextIntN(bound); m.Horse.Strength != s {
				t.Errorf("llama %d strength = %d, want %d", i, m.Horse.Strength, s)
			}
			if i == 0 {
				firstLlama = want.NextIntN(4)
			}
			if m.Horse.Variant != firstLlama {
				t.Errorf("llama %d variant = %d, want %d", i, m.Horse.Variant, firstLlama)
			}
			if h := horseHealth(want); m.MaxHealth != h {
				t.Errorf("llama %d max health = %v, want %v", i, m.MaxHealth, h)
			}
		}},
		{"armadillo", TypeArmadillo, "minecraft:savanna", 0.05, nil},
		{"polar bear", TypePolarBear, "minecraft:snowy_plains", 1, nil},
		{"goat", TypeGoat, "minecraft:snowy_slopes", 0.05, func(t *testing.T, want levelgen.RandomSource, m *Mob, i int) {
			// vanilla: GoatAi.initMemories, long jump then ram
			if c := 600 + int(want.NextIntN(601)); m.LongJumpCooldown() != c {
				t.Errorf("goat %d long jump cooldown = %d, want %d", i, m.LongJumpCooldown(), c)
			}
			if c := 600 + int(want.NextIntN(5401)); m.RamCooldown() != c {
				t.Errorf("goat %d ram cooldown = %d, want %d", i, m.RamCooldown(), c)
			}
			if s := want.NextDouble() < 0.02; m.Goat.Screaming != s {
				t.Errorf("goat %d screaming = %v, want %v", i, m.Goat.Screaming, s)
			}
			var noLeft, noRight bool
			if float64(want.NextFloat()) < float64(float32(0.1)) {
				left := want.NextBoolean()
				noLeft, noRight = left, !left
			}
			if m.Goat.NoLeftHorn != noLeft || m.Goat.NoRightHorn != noRight {
				t.Errorf("goat %d horns missing left %v right %v, want %v %v", i, m.Goat.NoLeftHorn, m.Goat.NoRightHorn, noLeft, noRight)
			}
		}},
		{"parrot", TypeParrot, "minecraft:jungle", noBaby, func(t *testing.T, want levelgen.RandomSource, m *Mob, i int) {
			if v := int(want.NextIntN(5)); m.ParrotVariant() != v {
				t.Errorf("parrot %d variant = %d, want %d", i, m.ParrotVariant(), v)
			}
		}},
		{"panda", TypePanda, "minecraft:bamboo_jungle", 0.2, func(t *testing.T, want levelgen.RandomSource, m *Mob, i int) {
			main := pandaRandomGene(int(want.NextIntN(16)))
			hidden := pandaRandomGene(int(want.NextIntN(16)))
			if m.panda.main != main || m.panda.hidden != hidden {
				t.Errorf("panda %d genes = %v/%v, want %v/%v", i, m.panda.main, m.panda.hidden, main, hidden)
			}
		}},
		{"ocelot", TypeOcelot, "minecraft:jungle", 1, nil},
		{"frog", TypeFrog, "minecraft:mangrove_swamp", 0.05, func(t *testing.T, want levelgen.RandomSource, m *Mob, i int) {
			want.NextIntN(1) // warm, the one priority-1 variant mangrove swamps offer
			if m.Variant != "minecraft:warm" {
				t.Errorf("frog %d variant = %s, want minecraft:warm", i, m.Variant)
			}
			// vanilla: FrogAi.initMemories
			if c := 100 + int(want.NextIntN(41)); m.LongJumpCooldown() != c {
				t.Errorf("frog %d long jump cooldown = %d, want %d", i, m.LongJumpCooldown(), c)
			}
		}},
		{"turtle", TypeTurtle, "minecraft:beach", 0.05, nil},
		{"mooshroom", TypeMooshroom, "minecraft:mushroom_fields", 0.05, nil},
		{"camel", TypeCamel, "minecraft:desert", 0.2, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			dice, want := regionDice(), regionDice()
			group := NewSpawnGroup(dice)
			for i := range 2 {
				m := NewMob(c.kind, int32(1+i), 0.5, 64, 0.5, int64(1+i))
				m.FinalizeSpawn(dice, testBiome(c.biome), group, rules.LocalDifficulty{})
				if c.own != nil {
					c.own(t, want, m, i)
				}
				baby := false
				if i == 1 && c.baby != noBaby {
					baby = want.NextFloat() <= c.baby
				}
				if m.IsBaby() != baby {
					t.Errorf("member %d baby = %v, want %v", i, m.IsBaby(), baby)
				}
				checkMobStep(t, want, m)
			}
			if got, w := dice.NextLong(), want.NextLong(); got != w {
				t.Errorf("the finalizes left the dice at %d, want %d: a draw too many or too few", got, w)
			}
		})
	}
}

// TestEquinePackBabyOddsByKind: a roll of 0.1 grows a pack's second horse
// (HorseGroupData, 0.05) but makes a second donkey or camel a baby
// (AbstractHorse's AgeableMobGroupData, 0.2).
// vanilla: Horse.finalizeSpawn, AbstractHorse.finalizeSpawn
func TestEquinePackBabyOddsByKind(t *testing.T) {
	for _, c := range []struct {
		kind MobType
		baby bool
	}{{TypeHorse, false}, {TypeDonkey, true}, {TypeMule, true}, {TypeCamel, true}} {
		dice := regionDice()
		group := NewSpawnGroup(&scriptedDice{floats: []float32{0.1}})
		var second *Mob
		for i := range 2 {
			second = NewMob(c.kind, int32(1+i), 0.5, 64, 0.5, int64(1+i))
			second.FinalizeSpawn(dice, testBiome("minecraft:plains"), group, rules.LocalDifficulty{})
		}
		if second.IsBaby() != c.baby {
			t.Errorf("type %v: second member baby = %v, want %v", c.kind, second.IsBaby(), c.baby)
		}
	}
}
