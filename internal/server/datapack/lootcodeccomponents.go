package datapack

import (
	"encoding/json"
	"math"
	"slices"
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The functions that write a data component read its values through the
// component's own file codec (registry.ReadComponent), so a function takes
// what a saved stack would and refuses the rest.

// componentOf reads v as the named component onto an empty stack.
func componentOf(name string, v any) protocol.ItemStack {
	tag, n, err := nbt.ParseValueTag(snbt(v))
	if err != nil || n == 0 {
		fail("Failed to read %s", name)
	}
	var stack protocol.ItemStack
	if !registry.ReadComponent(&stack, name, tag) {
		fail("Failed to decode %s: %s", name, snbt(v))
	}
	return stack
}

// componentFunction reads the functions whose value is a component's.
// vanilla: SetBannerPatternFunction, SetBookCoverFunction,
// SetCustomModelDataFunction, SetFireworkExplosionFunction,
// SetFireworksFunction, SetContainerLootTable, SetWritableBookPagesFunction,
// SetWrittenBookPagesFunction, FillPlayerHead, ModifyContainerContents
func (d lootDecoder) componentFunction(kind string, f obj, conds []registry.LootCondition) (registry.LootFunction, bool) {
	switch kind {
	case "minecraft:set_banner_pattern":
		layers, _ := componentOf("minecraft:banner_patterns", required(f, "patterns")).BannerPatternsOf()
		return registry.LootSetBannerPattern{Patterns: layers, Append: requireFlag(f, "append"), Conditions: conds}, true
	case "minecraft:set_book_cover":
		out := registry.LootSetBookCover{Conditions: conds}
		if v, ok := f["title"]; ok {
			book, _ := componentOf("minecraft:written_book_content", obj{"title": v, "author": ""}).WrittenBookOf()
			out.Title = &protocol.FilterablePage{Raw: book.Title, Filtered: book.FilteredTitle}
		}
		if _, ok := f["author"]; ok {
			out.Author = new(requireStr(f, "author"))
		}
		if _, ok := f["generation"]; ok {
			g := requireNum(f, "generation")
			if g < 0 || g > 3 || g != math.Trunc(g) {
				fail("Value %v outside of range [0:3]", g)
			}
			out.Generation = new(int32(g))
		}
		return out, true
	case "minecraft:set_custom_model_data":
		return d.setCustomModelData(f, conds), true
	case "minecraft:set_firework_explosion":
		out := registry.LootSetFireworkExplosion{Conditions: conds}
		explosion := func(key string, v any) protocol.FireworkExplosion {
			e, _ := componentOf("minecraft:firework_explosion", obj{"shape": "small_ball", key: v}).FireworkExplosionOf()
			return e
		}
		if v, ok := f["shape"]; ok {
			e, _ := componentOf("minecraft:firework_explosion", obj{"shape": v}).FireworkExplosionOf()
			out.Shape = &e.Shape
		}
		if v, ok := f["colors"]; ok {
			out.Colors = new(explosion("colors", v).Colors)
		}
		if v, ok := f["fade_colors"]; ok {
			out.FadeColors = new(explosion("fade_colors", v).FadeColors)
		}
		if _, ok := f["trail"]; ok {
			out.Trail = new(requireFlag(f, "trail"))
		}
		if _, ok := f["twinkle"]; ok {
			out.Twinkle = new(requireFlag(f, "twinkle"))
		}
		return out, true
	case "minecraft:set_fireworks":
		out := registry.LootSetFireworks{Conditions: conds}
		if v, ok := f["explosions"]; ok {
			o := object(v, "explosions")
			_, explosions, _ := componentOf("minecraft:fireworks", obj{"explosions": required(o, "values")}).FireworksComponentOf()
			out.Explosions = &registry.LootStandAlone[protocol.FireworkExplosion]{Values: explosions, Operation: listOperation(o, 256)}
		}
		if _, ok := f["flight_duration"]; ok {
			n := requireNum(f, "flight_duration")
			if n < 0 || n > 255 || n != math.Trunc(n) {
				fail("Value %v outside of range [0:255]", n)
			}
			out.Flight = new(int32(n))
		}
		return out, true
	case "minecraft:set_loot_table":
		id, ok := required(f, "loot_table_id").(string)
		if !ok {
			fail("Only tag names supported")
		}
		return registry.LootSetContainerLoot{Table: registry.Qualify(id), Seed: longAt(f, "seed"), Conditions: conds}, true
	case "minecraft:set_writable_book_pages":
		book, _ := componentOf("minecraft:writable_book_content", obj{"pages": required(f, "pages")}).WritableBookOf()
		return registry.LootSetWritableBookPages{Pages: book.Pages, Operation: listOperation(f, 100), Conditions: conds}, true
	case "minecraft:set_written_book_pages":
		book, _ := componentOf("minecraft:written_book_content",
			obj{"title": "", "author": "", "pages": required(f, "pages")}).WrittenBookOf()
		return registry.LootSetWrittenBookPages{Pages: book.Pages, Operation: listOperation(f, math.MaxInt32), Conditions: conds}, true
	case "minecraft:fill_player_head":
		entity := requireStr(f, "entity")
		if !slices.Contains(lootEntityTargets, entity) {
			fail("Invalid entity target %q", entity)
		}
		return registry.LootFillPlayerHead{Entity: entity, Conditions: conds}, true
	case "minecraft:modify_contents":
		component := registry.Qualify(requireStr(f, "component"))
		switch component {
		case "minecraft:container", "minecraft:bundle_contents", "minecraft:charged_projectiles":
		default:
			fail("No items in component")
		}
		// vanilla: LootItemFunctions.CODEC
		modifier := d.modifierHolder(required(f, "modifier"))
		return registry.LootModifyContents{Component: component, Modifier: modifier, Conditions: conds}, true
	}
	return nil, false
}

// setCustomModelData reads the four optional list operations; a colour is a
// context int provider or an RGB colour.
// vanilla: SetCustomModelDataFunction.MAP_CODEC
func (d lootDecoder) setCustomModelData(f obj, conds []registry.LootCondition) registry.LootSetCustomModelData {
	out := registry.LootSetCustomModelData{Conditions: conds}
	values := func(key string) (obj, []any, bool) {
		v, ok := f[key]
		if !ok {
			return nil, nil, false
		}
		o := object(v, key)
		return o, list(required(o, "values"), "values"), true
	}
	if o, vs, ok := values("floats"); ok {
		op := &registry.LootStandAlone[registry.LootNumber]{Operation: listOperation(o, math.MaxInt32)}
		for _, v := range vs {
			op.Values = append(op.Values, d.floatProvider(v))
		}
		out.Floats = op
	}
	if o, vs, ok := values("flags"); ok {
		op := &registry.LootStandAlone[bool]{Operation: listOperation(o, math.MaxInt32)}
		for _, v := range vs {
			n, isNum := num(v) // a bool, or an SNBT byte
			if !isNum {
				fail("Not a boolean: %v", v)
			}
			op.Values = append(op.Values, n != 0)
		}
		out.Flags = op
	}
	if o, vs, ok := values("strings"); ok {
		op := &registry.LootStandAlone[string]{Operation: listOperation(o, math.MaxInt32)}
		for _, v := range vs {
			op.Values = append(op.Values, str(v, "string"))
		}
		out.Strings = op
	}
	if o, vs, ok := values("colors"); ok {
		op := &registry.LootStandAlone[registry.LootNumber]{Operation: listOperation(o, math.MaxInt32)}
		for _, v := range vs {
			if _, isList := v.([]any); !isList {
				op.Values = append(op.Values, d.intProvider(v))
				continue
			}
			tag, _, err := nbt.ParseValueTag(snbt(v))
			rgb, ok := registry.RGBColorOf(tag)
			if err != nil || !ok {
				fail("Not an RGB colour: %s", snbt(v))
			}
			op.Values = append(op.Values, registry.LootConstant{Value: float64(rgb)})
		}
		out.Colors = op
	}
	return out
}

// longAt reads an optional Codec.LONG field, zero when absent, exactly
// rather than through a float.
func longAt(o obj, key string) int64 {
	v, ok := o[key]
	if !ok {
		return 0
	}
	if n, isNum := v.(json.Number); isNum {
		if i, err := strconv.ParseInt(n.String(), 10, 64); err == nil {
			return i
		}
	}
	f, ok := num(v)
	if !ok {
		fail("%q is not a number", key)
	}
	return int64(f)
}
