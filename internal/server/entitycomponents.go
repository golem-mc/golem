package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// mobComponent is the value of the entity component name that m holds, false
// for a component its kind does not read.
// vanilla: Entity.get, the applyImplicitComponent overrides
func mobComponent(m *entity.Mob, name string) (string, bool) {
	enum := func(id int32) (string, bool) { return registry.EnumComponentName(name, id) }
	switch name {
	case "minecraft:wolf/variant":
		return mobVariantOf(m, entity.TypeWolf, m.Variant)
	case "minecraft:wolf/sound_variant":
		return mobVariantOf(m, entity.TypeWolf, m.SoundVariant)
	case "minecraft:cat/variant":
		return mobVariantOf(m, entity.TypeCat, m.Variant)
	case "minecraft:cat/sound_variant":
		return mobVariantOf(m, entity.TypeCat, m.SoundVariant)
	case "minecraft:pig/variant":
		return mobVariantOf(m, entity.TypePig, m.Variant)
	case "minecraft:pig/sound_variant":
		return mobVariantOf(m, entity.TypePig, m.SoundVariant)
	case "minecraft:cow/variant":
		return mobVariantOf(m, entity.TypeCow, m.Variant)
	case "minecraft:cow/sound_variant":
		return mobVariantOf(m, entity.TypeCow, m.SoundVariant)
	case "minecraft:chicken/variant":
		return mobVariantOf(m, entity.TypeChicken, m.Variant)
	case "minecraft:chicken/sound_variant":
		return mobVariantOf(m, entity.TypeChicken, m.SoundVariant)
	case "minecraft:frog/variant":
		return mobVariantOf(m, entity.TypeFrog, m.Variant)
	case "minecraft:zombie_nautilus/variant":
		return mobVariantOf(m, entity.TypeZombieNautilus, m.Variant)
	case "minecraft:villager/variant":
		if m.Type == entity.TypeVillager || m.Type == entity.TypeZombieVillager {
			return registry.Qualify(m.Villager.Type), true
		}
	case "minecraft:wolf/collar":
		if m.Type == entity.TypeWolf {
			return m.Collar, true
		}
	case "minecraft:cat/collar":
		if m.Type == entity.TypeCat {
			return m.Collar, true
		}
	case "minecraft:sheep/color":
		if m.Type == entity.TypeSheep {
			return m.Colour, true
		}
	case "minecraft:mooshroom/variant":
		if m.Type == entity.TypeMooshroom {
			return m.Mushroom, true
		}
	case "minecraft:fox/variant":
		if m.Type == entity.TypeFox {
			return enum(m.Fox.Variant)
		}
	case "minecraft:parrot/variant":
		if m.Type == entity.TypeParrot {
			return enum(int32(m.ParrotVariant()))
		}
	case "minecraft:axolotl/variant":
		if m.Type == entity.TypeAxolotl {
			return enum(int32(m.AxolotlVariant()))
		}
	case "minecraft:salmon/size":
		if m.Type == entity.TypeSalmon {
			return enum(int32(m.SalmonSize))
		}
	case "minecraft:rabbit/variant":
		if m.Type == entity.TypeRabbit {
			return enum(m.RabbitType)
		}
	case "minecraft:horse/variant":
		if m.Type == entity.TypeHorse {
			return enum(m.Horse.Variant & 0xFF)
		}
	case "minecraft:llama/variant":
		if entity.IsLlama(m.Type) {
			return enum(m.Horse.Variant)
		}
	case "minecraft:shulker/color":
		// An undyed shulker holds no colour (DATA_COLOR_ID's NO_COLOR, 16).
		if m.Type == entity.TypeShulker && m.ShulkerColor() < 16 {
			return enum(int32(m.ShulkerColor()))
		}
	case "minecraft:tropical_fish/pattern":
		if m.Type == entity.TypeTropicalFish {
			return enum(m.TropicalVariant & 0xFFFF)
		}
	case "minecraft:tropical_fish/base_color":
		if m.Type == entity.TypeTropicalFish {
			return enum(m.TropicalVariant >> 16 & 0xFF)
		}
	case "minecraft:tropical_fish/pattern_color":
		if m.Type == entity.TypeTropicalFish {
			return enum(m.TropicalVariant >> 24 & 0xFF)
		}
	}
	return "", false
}

// mobVariantOf is a registry-entry variant, held by the one kind only.
func mobVariantOf(m *entity.Mob, kind entity.MobType, variant string) (string, bool) {
	return variant, m.Type == kind && variant != ""
}

// mobLootEntity is m's loot snapshot with its components readable; the
// caller holds the lock that guards m for as long as the snapshot is used.
func mobLootEntity(m *entity.Mob) rules.LootEntity {
	le := m.LootEntity()
	le.Component = func(name string) (string, bool) { return mobComponent(m, name) }
	return le
}
