package world

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Bits of a pre-1.20.5 stack's HideFlags.
// vanilla: ItemStackComponentizationFix
const (
	hideEnchantments = 1
	hideModifiers    = 2
	hideUnbreakable  = 4
	hideCanDestroy   = 8
	hideCanPlace     = 16
	hideAdditional   = 32
	hideDye          = 64
	hideUpgrades     = 128
)

// tagComponentized are the "tag" keys componentizeTag moves; any other key of
// a stack's tag lands in minecraft:custom_data.
var tagComponentized = []string{
	"HideFlags", "Damage", "RepairCost", "CustomModelData", "BlockStateTag", "EntityTag",
	"Unbreakable", "Enchantments", "StoredEnchantments", "display", "AttributeModifiers",
}

// legacyItemEntities are the entities an item's id-less EntityTag describes.
// vanilla: V705.ITEM_TO_ENTITY
var legacyItemEntities = map[string]string{
	"minecraft:armor_stand":                "minecraft:armor_stand",
	"minecraft:painting":                   "minecraft:painting",
	"minecraft:armadillo_spawn_egg":        "minecraft:armadillo",
	"minecraft:allay_spawn_egg":            "minecraft:allay",
	"minecraft:axolotl_spawn_egg":          "minecraft:axolotl",
	"minecraft:bat_spawn_egg":              "minecraft:bat",
	"minecraft:bee_spawn_egg":              "minecraft:bee",
	"minecraft:blaze_spawn_egg":            "minecraft:blaze",
	"minecraft:bogged_spawn_egg":           "minecraft:bogged",
	"minecraft:breeze_spawn_egg":           "minecraft:breeze",
	"minecraft:cat_spawn_egg":              "minecraft:cat",
	"minecraft:camel_spawn_egg":            "minecraft:camel",
	"minecraft:cave_spider_spawn_egg":      "minecraft:cave_spider",
	"minecraft:chicken_spawn_egg":          "minecraft:chicken",
	"minecraft:cod_spawn_egg":              "minecraft:cod",
	"minecraft:cow_spawn_egg":              "minecraft:cow",
	"minecraft:creeper_spawn_egg":          "minecraft:creeper",
	"minecraft:dolphin_spawn_egg":          "minecraft:dolphin",
	"minecraft:donkey_spawn_egg":           "minecraft:donkey",
	"minecraft:drowned_spawn_egg":          "minecraft:drowned",
	"minecraft:elder_guardian_spawn_egg":   "minecraft:elder_guardian",
	"minecraft:ender_dragon_spawn_egg":     "minecraft:ender_dragon",
	"minecraft:enderman_spawn_egg":         "minecraft:enderman",
	"minecraft:endermite_spawn_egg":        "minecraft:endermite",
	"minecraft:evoker_spawn_egg":           "minecraft:evoker",
	"minecraft:fox_spawn_egg":              "minecraft:fox",
	"minecraft:frog_spawn_egg":             "minecraft:frog",
	"minecraft:ghast_spawn_egg":            "minecraft:ghast",
	"minecraft:glow_squid_spawn_egg":       "minecraft:glow_squid",
	"minecraft:goat_spawn_egg":             "minecraft:goat",
	"minecraft:guardian_spawn_egg":         "minecraft:guardian",
	"minecraft:hoglin_spawn_egg":           "minecraft:hoglin",
	"minecraft:horse_spawn_egg":            "minecraft:horse",
	"minecraft:husk_spawn_egg":             "minecraft:husk",
	"minecraft:iron_golem_spawn_egg":       "minecraft:iron_golem",
	"minecraft:llama_spawn_egg":            "minecraft:llama",
	"minecraft:magma_cube_spawn_egg":       "minecraft:magma_cube",
	"minecraft:mooshroom_spawn_egg":        "minecraft:mooshroom",
	"minecraft:mule_spawn_egg":             "minecraft:mule",
	"minecraft:ocelot_spawn_egg":           "minecraft:ocelot",
	"minecraft:panda_spawn_egg":            "minecraft:panda",
	"minecraft:parrot_spawn_egg":           "minecraft:parrot",
	"minecraft:phantom_spawn_egg":          "minecraft:phantom",
	"minecraft:pig_spawn_egg":              "minecraft:pig",
	"minecraft:piglin_spawn_egg":           "minecraft:piglin",
	"minecraft:piglin_brute_spawn_egg":     "minecraft:piglin_brute",
	"minecraft:pillager_spawn_egg":         "minecraft:pillager",
	"minecraft:polar_bear_spawn_egg":       "minecraft:polar_bear",
	"minecraft:pufferfish_spawn_egg":       "minecraft:pufferfish",
	"minecraft:rabbit_spawn_egg":           "minecraft:rabbit",
	"minecraft:ravager_spawn_egg":          "minecraft:ravager",
	"minecraft:salmon_spawn_egg":           "minecraft:salmon",
	"minecraft:sheep_spawn_egg":            "minecraft:sheep",
	"minecraft:shulker_spawn_egg":          "minecraft:shulker",
	"minecraft:silverfish_spawn_egg":       "minecraft:silverfish",
	"minecraft:skeleton_spawn_egg":         "minecraft:skeleton",
	"minecraft:skeleton_horse_spawn_egg":   "minecraft:skeleton_horse",
	"minecraft:slime_spawn_egg":            "minecraft:slime",
	"minecraft:sniffer_spawn_egg":          "minecraft:sniffer",
	"minecraft:snow_golem_spawn_egg":       "minecraft:snow_golem",
	"minecraft:spider_spawn_egg":           "minecraft:spider",
	"minecraft:squid_spawn_egg":            "minecraft:squid",
	"minecraft:stray_spawn_egg":            "minecraft:stray",
	"minecraft:strider_spawn_egg":          "minecraft:strider",
	"minecraft:tadpole_spawn_egg":          "minecraft:tadpole",
	"minecraft:trader_llama_spawn_egg":     "minecraft:trader_llama",
	"minecraft:tropical_fish_spawn_egg":    "minecraft:tropical_fish",
	"minecraft:turtle_spawn_egg":           "minecraft:turtle",
	"minecraft:vex_spawn_egg":              "minecraft:vex",
	"minecraft:villager_spawn_egg":         "minecraft:villager",
	"minecraft:vindicator_spawn_egg":       "minecraft:vindicator",
	"minecraft:wandering_trader_spawn_egg": "minecraft:wandering_trader",
	"minecraft:warden_spawn_egg":           "minecraft:warden",
	"minecraft:witch_spawn_egg":            "minecraft:witch",
	"minecraft:wither_spawn_egg":           "minecraft:wither",
	"minecraft:wither_skeleton_spawn_egg":  "minecraft:wither_skeleton",
	"minecraft:wolf_spawn_egg":             "minecraft:wolf",
	"minecraft:zoglin_spawn_egg":           "minecraft:zoglin",
	"minecraft:zombie_spawn_egg":           "minecraft:zombie",
	"minecraft:zombie_horse_spawn_egg":     "minecraft:zombie_horse",
	"minecraft:zombie_villager_spawn_egg":  "minecraft:zombie_villager",
	"minecraft:zombified_piglin_spawn_egg": "minecraft:zombified_piglin",
	"minecraft:item_frame":                 "minecraft:item_frame",
	"minecraft:boat":                       "minecraft:oak_boat",
	"minecraft:oak_boat":                   "minecraft:oak_boat",
	"minecraft:oak_chest_boat":             "minecraft:oak_chest_boat",
	"minecraft:spruce_boat":                "minecraft:spruce_boat",
	"minecraft:spruce_chest_boat":          "minecraft:spruce_chest_boat",
	"minecraft:birch_boat":                 "minecraft:birch_boat",
	"minecraft:birch_chest_boat":           "minecraft:birch_chest_boat",
	"minecraft:jungle_boat":                "minecraft:jungle_boat",
	"minecraft:jungle_chest_boat":          "minecraft:jungle_chest_boat",
	"minecraft:acacia_boat":                "minecraft:acacia_boat",
	"minecraft:acacia_chest_boat":          "minecraft:acacia_chest_boat",
	"minecraft:cherry_boat":                "minecraft:cherry_boat",
	"minecraft:cherry_chest_boat":          "minecraft:cherry_chest_boat",
	"minecraft:dark_oak_boat":              "minecraft:dark_oak_boat",
	"minecraft:dark_oak_chest_boat":        "minecraft:dark_oak_chest_boat",
	"minecraft:mangrove_boat":              "minecraft:mangrove_boat",
	"minecraft:mangrove_chest_boat":        "minecraft:mangrove_chest_boat",
	"minecraft:bamboo_raft":                "minecraft:bamboo_raft",
	"minecraft:bamboo_chest_raft":          "minecraft:bamboo_chest_raft",
	"minecraft:minecart":                   "minecraft:minecart",
	"minecraft:chest_minecart":             "minecraft:chest_minecart",
	"minecraft:furnace_minecart":           "minecraft:furnace_minecart",
	"minecraft:tnt_minecart":               "minecraft:tnt_minecart",
	"minecraft:hopper_minecart":            "minecraft:hopper_minecart",
}

// componentizeTag moves the tag of a legacy stack, already renamed, into
// entries["components"]; what is left of the tag becomes custom_data.
// vanilla: ItemStackComponentizationFix.fixItemStack
func componentizeTag(entries nbt.Compound, id string) {
	item := itemOrNone(id)
	tag, ok := nbt.CompoundView(entries["tag"])
	delete(entries, "tag")
	if !ok || len(tag) == 0 {
		return
	}
	c := nbt.Compound{}
	take := func(key string) (nbt.Tag, bool) { return takeKey(tag, key) }
	hide := 0
	if v, ok := take("HideFlags"); ok {
		n, _ := nbt.IntValue(v)
		hide = int(n)
	}
	moveNonDefault := func(key, component string) {
		if v, ok := take(key); ok {
			if i, isInt := v.(nbt.Int); isInt && i == 0 {
				return
			}
			c[component] = v
		}
	}
	moveNonDefault("Damage", "minecraft:damage")
	moveNonDefault("RepairCost", "minecraft:repair_cost")
	if v, ok := take("CustomModelData"); ok {
		c["minecraft:custom_model_data"] = v
	}
	if v, ok := take("BlockStateTag"); ok {
		c["minecraft:block_state"] = fixBlockStateTag(v)
	}
	if v, ok := take("EntityTag"); ok {
		if e, isMap := nbt.CompoundView(v); isMap {
			if _, has := e["id"]; !has && legacyItemEntities[id] != "" {
				e["id"] = nbt.String(legacyItemEntities[id]) // vanilla: V99.addNames
			}
		}
		c["minecraft:entity_data"] = v
	}
	addTagIDs(tag, item)
	componentizeBlockEntityTag(c, tag, id)
	if v, ok := take("Unbreakable"); ok && boolValue(v) {
		u := nbt.Compound{}
		if hide&hideUnbreakable != 0 {
			u["show_in_tooltip"] = nbt.Byte(0)
		}
		c["minecraft:unbreakable"] = u
	}
	componentizeEnchantments(c, tag, "Enchantments", "minecraft:enchantments", hide&hideEnchantments != 0)
	if item == registry.ItemEnchantedBook {
		componentizeEnchantments(c, tag, "StoredEnchantments", "minecraft:stored_enchantments", hide&hideAdditional != 0)
	}
	if display, ok := nbt.CompoundView(tag["display"]); ok {
		componentizeDisplay(c, display, id, hide)
		if len(display) == 0 {
			delete(tag, "display")
		}
	}
	if v, ok := take("AttributeModifiers"); ok {
		if list, isList := v.(nbt.List); isList {
			mods := make([]nbt.Tag, 0, len(list.Items))
			for _, m := range list.Items {
				mods = append(mods, fixLegacyModifier(m))
			}
			comp := nbt.Compound{"modifiers": nbt.List{Elem: nbt.TypeCompound, Items: mods}}
			if hide&hideModifiers != 0 {
				comp["show_in_tooltip"] = nbt.Byte(0)
			}
			c["minecraft:attribute_modifiers"] = comp
		}
	}
	componentizeBlockPredicates(c, tag, "CanDestroy", "minecraft:can_break", hide&hideCanDestroy != 0)
	componentizeBlockPredicates(c, tag, "CanPlaceOn", "minecraft:can_place_on", hide&hideCanPlace != 0)
	if trim, ok := take("Trim"); ok {
		if t, isMap := nbt.CompoundView(trim); isMap && hide&hideUpgrades != 0 {
			t["show_in_tooltip"] = nbt.Byte(0)
		}
		c["minecraft:trim"] = trim
	}
	if hide&hideAdditional != 0 {
		c["minecraft:hide_additional_tooltip"] = nbt.Compound{}
	}
	componentizeItemKind(c, tag, id)
	if len(tag) > 0 {
		c["minecraft:custom_data"] = tag
	}
	if len(c) > 0 {
		entries["components"] = c
	}
}

// addTagIDs gives a stack's BlockEntityTag and EntityTag the id of the kind
// the item places.
// vanilla: V99.addNames
func addTagIDs(tag nbt.Compound, item registry.Item) {
	if be, ok := nbt.CompoundView(tag["BlockEntityTag"]); ok && item >= 0 && (item == registry.ItemShield || item.InTag(registry.ItemTagIDBanners)) {
		be["id"] = nbt.String("minecraft:banner")
	}
	if entity, ok := nbt.CompoundView(tag["EntityTag"]); ok && entity["id"] == nil {
		switch item {
		case registry.ItemPainting:
			entity["id"] = nbt.String("minecraft:painting")
		case registry.ItemArmorStand:
			entity["id"] = nbt.String("minecraft:armor_stand")
		}
	}
}

func takeKey(tag nbt.Compound, key string) (nbt.Tag, bool) {
	v, ok := tag[key]
	delete(tag, key)
	return v, ok
}

func componentizeEnchantments(c, tag nbt.Compound, key, component string, hideInTooltip bool) {
	raw, present := tag[key]
	delete(tag, key)
	levels := nbt.Compound{}
	if list, ok := raw.(nbt.List); ok {
		for _, item := range list.Items {
			e, ok := nbt.CompoundView(item)
			if !ok {
				continue
			}
			id, hasID := e["id"].(nbt.String)
			lvl, hasLvl := nbt.IntValue(e["lvl"])
			if !hasID || !hasLvl {
				continue
			}
			if lvl = min(max(lvl, 0), 255); lvl > 0 {
				levels[string(id)] = nbt.Int(lvl)
			}
		}
	}
	if len(levels) > 0 || hideInTooltip {
		comp := nbt.Compound{"levels": levels}
		if hideInTooltip {
			comp["show_in_tooltip"] = nbt.Byte(0)
		}
		c[component] = comp
	}
	if present && len(levels) == 0 {
		c["minecraft:enchantment_glint_override"] = nbt.Byte(1)
	}
}

func componentizeDisplay(c, display nbt.Compound, id string, hide int) {
	if name, ok := display["Name"]; ok && strictJSON(name) {
		c["minecraft:custom_name"] = name
	}
	if lore, ok := display["Lore"].(nbt.List); ok {
		kept := []nbt.Tag{}
		for _, l := range lore.Items {
			if strictJSON(l) {
				kept = append(kept, l)
			}
		}
		c["minecraft:lore"] = nbt.List{Elem: nbt.TypeString, Items: kept}
	}
	color, hasColor := nbt.IntValue(display["color"])
	if hasColor || hide&hideDye != 0 {
		if !hasColor {
			color = 10511680
		}
		dyed := nbt.Compound{"rgb": nbt.Int(color)}
		if hide&hideDye != 0 {
			dyed["show_in_tooltip"] = nbt.Byte(0)
		}
		c["minecraft:dyed_color"] = dyed
	}
	if loc, ok := display["LocName"].(nbt.String); ok {
		b, _ := json.Marshal(map[string]string{"translate": string(loc)})
		c["minecraft:item_name"] = nbt.String(b)
	}
	if mc, ok := display["MapColor"]; ok && itemOrNone(id) == registry.ItemFilledMap {
		c["minecraft:map_color"] = mc
		delete(display, "MapColor")
	}
	for _, k := range []string{"Name", "Lore", "color", "LocName"} {
		delete(display, k)
	}
}

// strictJSON reports whether a legacy text component is a JSON string, array
// or object; anything else vanilla drops.
// vanilla: LegacyComponentDataFixUtils.isStrictlyValidJson
func strictJSON(tag nbt.Tag) bool {
	s, ok := tag.(nbt.String)
	if !ok {
		return false
	}
	t := strings.TrimSpace(string(s))
	return t != "" && strings.ContainsAny(t[:1], `{["`) && json.Valid([]byte(t))
}

// booleanBlockStateProperties are the block state keys whose stored byte
// vanilla turns into "true" or "false".
var booleanBlockStateProperties = map[string]bool{
	"attached": true, "bottom": true, "conditional": true, "disarmed": true, "drag": true, "enabled": true,
	"extended": true, "eye": true, "falling": true, "hanging": true, "has_bottle_0": true, "has_bottle_1": true,
	"has_bottle_2": true, "has_record": true, "has_book": true, "inverted": true, "in_wall": true, "lit": true,
	"locked": true, "occupied": true, "open": true, "persistent": true, "powered": true, "short": true,
	"signal_fire": true, "snowy": true, "triggered": true, "unstable": true, "waterlogged": true, "berries": true,
	"bloom": true, "shrieking": true, "can_summon": true, "up": true, "down": true, "north": true, "east": true,
	"south": true, "west": true, "slot_0_occupied": true, "slot_1_occupied": true, "slot_2_occupied": true,
	"slot_3_occupied": true, "slot_4_occupied": true, "slot_5_occupied": true, "cracked": true, "crafting": true,
}

// fixBlockStateTag stringifies the values of a BlockStateTag.
// vanilla: ItemStackComponentizationFix.fixBlockStateTag
func fixBlockStateTag(tag nbt.Tag) nbt.Tag {
	in, ok := nbt.CompoundView(tag)
	if !ok {
		return tag
	}
	out := nbt.Compound{}
	for k, v := range in {
		switch v.(type) {
		case nbt.Byte, nbt.Short, nbt.Int, nbt.Long, nbt.Float, nbt.Double:
			if booleanBlockStateProperties[k] {
				out[k] = nbt.String(strconv.FormatBool(boolValue(v)))
				continue
			}
			n, _ := nbt.IntValue(v)
			out[k] = nbt.String(strconv.Itoa(int(n)))
		default:
			out[k] = v
		}
	}
	return out
}

// fixLegacyModifier turns an AttributeModifiers entry into its component form.
// vanilla: ItemStackComponentizationFix.fixAttributeModifier
func fixLegacyModifier(tag nbt.Tag) nbt.Tag {
	in, _ := nbt.CompoundView(tag)
	out := nbt.Compound{"name": nbt.String(""), "amount": nbt.Double(0), "operation": nbt.String("add_value")}
	for from, to := range map[string]string{"AttributeName": "type", "Slot": "slot", "UUID": "uuid", "Name": "name", "Amount": "amount"} {
		if v, ok := in[from]; ok {
			out[to] = v
		}
	}
	if v, ok := in["Operation"]; ok {
		n, _ := nbt.IntValue(v)
		out["operation"] = nbt.String([]string{"add_value", "add_multiplied_base", "add_multiplied_total"}[max(0, min(n, 2))])
	}
	return out
}
