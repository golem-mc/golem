package datapack

import (
	"github.com/golem-mc/golem/registry"
)

// slotSourceTargets are the context arguments a slot_range source may name.
// vanilla: RangeSlotSource.SOURCE_CODEC
var slotSourceTargets = map[string]bool{
	"this": true, "attacker": true, "direct_attacker": true, "attacking_player": true,
	"target_entity": true, "interacting_entity": true, "block_entity": true, "container": true,
}

// DecodeSlotSource reads a slot source file.
// vanilla: RegistryDataLoader.RELOADABLE_REGISTRIES, SlotSources.DIRECT_CODEC
func DecodeSlotSource(data []byte) (s registry.SlotSource, err error) {
	defer recoverLoot(&err)
	return slotSource(parseJSON(data)), nil
}

// DecodeSlotSourceValue reads a slot source already parsed into the shapes
// encoding/json gives, as an inline command argument is.
// vanilla: SlotSources.CODEC
func DecodeSlotSourceValue(v any) (s registry.SlotSource, err error) {
	defer recoverLoot(&err)
	return slotSource(v), nil
}

// slotSource reads a data pack id, a typed compound, or a bare list that is
// a group.
// vanilla: SlotSources.CODEC, SlotSources.DIRECT_CODEC
func slotSource(v any) registry.SlotSource {
	switch k := v.(type) {
	case string:
		name := registry.Qualify(k)
		if _, ok := registry.FindSlotSource(name); !ok {
			fail("Failed to get element %s from registry minecraft:slot_source", name)
		}
		return registry.SlotSourceReference{Name: name}
	case []any:
		return registry.SlotGroupSource{Terms: slotTerms(k)}
	}
	o := object(v, "slot source")
	switch kind := registry.Qualify(str(o["type"], "slot source type")); kind {
	case "minecraft:empty":
		unknownKeys(o, "empty slot source", "type")
		return registry.SlotEmptySource{}
	case "minecraft:group":
		unknownKeys(o, "group slot source", "type", "terms")
		if terms, ok := o["terms"].([]any); ok {
			return registry.SlotGroupSource{Terms: slotTerms(terms)}
		}
		return registry.SlotGroupSource{Terms: []registry.SlotSource{slotSource(o["terms"])}}
	case "minecraft:slot_range":
		unknownKeys(o, "slot range source", "type", "source", "slots")
		out := registry.SlotRangeSource{Source: "container", Slots: requireStr(o, "slots")}
		if v, ok := o["source"]; ok {
			out.Source = str(v, "source")
			if !slotSourceTargets[out.Source] {
				fail("Unknown slot source target %s", out.Source)
			}
		}
		if SlotRangeKnown != nil && !SlotRangeKnown(out.Slots) {
			fail("Unknown slot range %s", out.Slots)
		}
		return out
	case "minecraft:filtered":
		unknownKeys(o, "filtered slot source", "type", "slot_source", "item_filter")
		return registry.SlotFilteredSource{From: slotSource(required(o, "slot_source")), Filter: itemPredicate(object(required(o, "item_filter"), "item filter"))}
	case "minecraft:limit_slots":
		unknownKeys(o, "limit slot source", "type", "slot_source", "limit")
		limit := int(optNum(o, "limit", 0))
		if limit <= 0 {
			fail("Value must be positive: %d", limit)
		}
		return registry.SlotLimitSource{From: slotSource(required(o, "slot_source")), Limit: limit}
	case "minecraft:contents":
		unknownKeys(o, "contents slot source", "type", "slot_source", "component")
		component := registry.Qualify(requireStr(o, "component"))
		switch component {
		case "minecraft:container", "minecraft:bundle_contents", "minecraft:charged_projectiles":
		default:
			fail("No items in component")
		}
		return registry.SlotContentsSource{From: slotSource(required(o, "slot_source")), Component: component}
	default:
		fail("Unknown registry key in ResourceKey[minecraft:root / minecraft:slot_source_type]: %s", kind)
	}
	return nil
}

func slotTerms(items []any) []registry.SlotSource {
	terms := make([]registry.SlotSource, len(items))
	for i, t := range items {
		terms[i] = slotSource(t)
	}
	return terms
}
