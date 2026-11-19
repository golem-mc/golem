package entity

import (
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// AllowedAt is EntityType.canSpawn for the kinds simulated here: a monster
// is not allowed in peaceful.
func AllowedAt(t MobType, difficulty uint8) bool {
	return !GoneInPeaceful(t) || difficulty != rules.DifficultyPeaceful
}

// TakesBabyEgg says whether a spawn egg used on a grown mob of the kind
// makes it a baby: SpawnEggItem.spawnOffspringFromSpawnEgg gives up when
// Mob.setBaby does nothing for the kind or getBreedOffspring makes none.
func (m *Mob) TakesBabyEgg() bool {
	// vanilla: WanderingTrader.getBreedOffspring
	return m.kind().babyable && m.Type != TypeWanderingTrader
}

// InheritInto gives an egg's offspring what getBreedOffspring takes from the
// parent bred with itself: a sheep's colour (DyeColor.getMixedColor of one
// colour twice is that colour), a mooshroom's variant, and a cow's, pig's or
// chicken's variant, and a goat's screaming roll.
func (m *Mob) InheritInto(baby *Mob) {
	if IsNautilus(m.Type) && m.Tamable.Tame {
		baby.SetMetadataTamableTame(true)
		baby.SetMetadataTamableOwner(m.Tamable.Owner)
	}
	if m.Type == TypeAxolotl {
		m.breedAxolotl(baby, m)
	}
	if m.Type == TypeGoat {
		m.breedGoat(baby, m)
	}
	if m.Type == TypeSheep {
		baby.SetMetadataColour(m.Colour)
	}
	if m.Type == TypeMooshroom {
		baby.SetMetadataMushroom(m.Mushroom)
	}
	if m.Type == TypeRabbit {
		baby.SetMetadataRabbitType(m.RabbitType)
	}
	if m.Type == TypePanda {
		m.breedPanda(baby, m)
	}
	if _, _, ok := VariantRegistries(m.Type); ok {
		baby.Variant = m.Variant
	}
}

// BreedInto gives a bred child what getBreedOffspring takes from its
// parents: a lamb the mixed colour of its two — DyeColor.getMixedColor,
// the data pack's recipe for the pair or the dice's pick of a parent —
// a calf, piglet or chick the variant of the parent the mother's coin
// lands on, and a mooshroom either parent's variant, or the other of a
// same-variant pair at MushroomCow.getOffspringVariant's 1-in-1024 flip.
// vanilla: Cow.getBreedOffspring, Pig.getBreedOffspring,
// Sheep.getBreedOffspring, Chicken.getBreedOffspring,
// MushroomCow.getBreedOffspring
func (m *Mob) BreedInto(baby *Mob, partner *Mob) {
	if IsNautilus(m.Type) && m.Tamable.Tame {
		baby.SetMetadataTamableTame(true)
		baby.SetMetadataTamableOwner(m.Tamable.Owner)
	}
	if m.Type == TypeAxolotl {
		m.breedAxolotl(baby, partner)
	}
	if m.Type == TypeGoat {
		m.breedGoat(baby, partner)
	}
	if m.Type == TypeSheep {
		baby.SetMetadataColour(rules.MixedWoolColour(m.Colour, partner.Colour, m.Rand))
	}
	if m.Type == TypeMooshroom {
		baby.SetMetadataMushroom(mooshroomOffspringVariant(m, partner))
	}
	if _, _, ok := VariantRegistries(m.Type); ok && m.Type != TypeCat {
		winner := partner
		if m.Rand.Intn(2) == 1 {
			winner = m
		}
		baby.Variant = winner.Variant
	}
	if m.Type == TypeWolf {
		m.breedWolf(baby, partner)
	}
	if IsEquine(m.Type) {
		m.breedHorse(baby, partner)
	}
	if m.Type == TypePanda {
		m.breedPanda(baby, partner)
	}
	if m.Type == TypeHoglin {
		baby.Persistence = true // vanilla: Hoglin.getBreedOffspring
	}
	if m.Type == TypeFox {
		// vanilla: Fox.getBreedOffspring
		baby.SetMetadataFoxVariant(partner.Fox.Variant)
		if m.Rand.Intn(2) == 0 {
			baby.SetMetadataFoxVariant(m.Fox.Variant)
		}
	}
}

// BreedRabbit is Rabbit.getBreedOffspring: the variant the biome the mother
// stands in rolls, kept on one breeding in twenty, else either parent's.
// vanilla: Rabbit.getBreedOffspring
func (m *Mob) BreedRabbit(baby, partner *Mob, biome registry.Biome) {
	variant := rabbitTypeFor(biome, GoDice{m.Rand})
	if m.Rand.Intn(20) != 0 {
		variant = m.RabbitType
		if partner.Type == TypeRabbit && m.Rand.Intn(2) == 1 {
			variant = partner.RabbitType
		}
	}
	baby.SetMetadataRabbitType(variant)
}

// mooshroomOffspringVariant is MushroomCow.getOffspringVariant: the mother
// and mate pick the calf's variant by coin when they differ, and a
// same-variant pair turns the other variant out of a 1-in-1024 roll.
func mooshroomOffspringVariant(mother, mate *Mob) string {
	if mother.Mushroom == mate.Mushroom {
		if mother.Rand.Intn(1024) == 0 {
			if mother.Mushroom == "brown" {
				return "red"
			}
			return "brown"
		}
		return mother.Mushroom
	}
	if mother.Rand.Intn(2) == 1 {
		return mother.Mushroom
	}
	return mate.Mushroom
}
