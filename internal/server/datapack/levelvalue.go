package datapack

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// decodeLevelValue reads a level-based value: a bare number or a compound
// with a type.
// vanilla: LevelBasedValue.CODEC
func decodeLevelValue(tag nbt.Tag) (*registry.EnchLevelValue, string) {
	if v, ok := tagFloat(tag); ok {
		return &registry.EnchLevelValue{Kind: registry.EnchValueConstant, Constant: v}, ""
	}
	c, ok := tag.(nbt.Compound)
	if !ok {
		return nil, "Not a map: " + nbt.Stringify(tag)
	}
	typeTag, msg := fieldOf(c, "type")
	if msg != "" {
		return nil, msg
	}
	s, ok := typeTag.(nbt.String)
	if !ok {
		return nil, "Not a string: " + nbt.Stringify(typeTag)
	}
	num := func(key string) (float32, string) {
		t, msg := fieldOf(c, key)
		if msg != "" {
			return 0, msg
		}
		v, ok := tagFloat(t)
		if !ok {
			return 0, "Not a number: " + nbt.Stringify(t)
		}
		return v, ""
	}
	sub := func(key string) (*registry.EnchLevelValue, string) {
		t, msg := fieldOf(c, key)
		if msg != "" {
			return nil, msg
		}
		return decodeLevelValue(t)
	}
	out := &registry.EnchLevelValue{}
	switch registry.Qualify(string(s)) {
	case "minecraft:constant":
		out.Kind = registry.EnchValueConstant
		out.Constant, msg = num("value")
	case "minecraft:linear":
		out.Kind = registry.EnchValueLinear
		if out.Base, msg = num("base"); msg == "" {
			out.PerLevel, msg = num("per_level_above_first")
		}
	case "minecraft:levels_squared":
		out.Kind = registry.EnchValueLevelsSquared
		out.Added, msg = num("added")
	case "minecraft:clamped":
		out.Kind = registry.EnchValueClamped
		if out.Value, msg = sub("value"); msg == "" {
			if out.Min, msg = num("min"); msg == "" {
				out.Max, msg = num("max")
			}
		}
	case "minecraft:fraction":
		out.Kind = registry.EnchValueFraction
		if out.Num, msg = sub("numerator"); msg == "" {
			out.Den, msg = sub("denominator")
		}
	case "minecraft:lookup":
		out.Kind = registry.EnchValueLookup
		out.Values, msg = listOf(c, "values", func(t nbt.Tag) (float32, string) {
			v, ok := tagFloat(t)
			if !ok {
				return 0, "Not a number: " + nbt.Stringify(t)
			}
			return v, ""
		})
		if msg == "" {
			out.Fallback, msg = sub("fallback")
		}
	case "minecraft:exponent":
		out.Kind = registry.EnchValueExponent
		if out.ExpBase, msg = sub("base"); msg == "" {
			out.ExpPower, msg = sub("power")
		}
	default:
		return nil, "Unknown registry key in ResourceKey[minecraft:root / minecraft:enchantment_level_based_value_type]: " + registry.Qualify(string(s))
	}
	if msg != "" {
		return nil, msg
	}
	return out, ""
}
