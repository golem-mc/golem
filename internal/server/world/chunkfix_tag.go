package world

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// Bits of a pre-1.20.5 stack's HideFlags.
// vanilla: ItemStackComponentizationFix
const (
	hideEnchantments = 1
	hideModifiers    = 2
	hideUnbreakable  = 4
	hideAdditional   = 32
	hideDye          = 64
)

// tagComponentized are the "tag" keys componentizeTag moves; any other key of
// a stack's tag lands in minecraft:custom_data.
var tagComponentized = []string{
	"HideFlags", "Damage", "RepairCost", "CustomModelData", "BlockStateTag", "EntityTag",
	"Unbreakable", "Enchantments", "StoredEnchantments", "display", "AttributeModifiers",
}

// tagNeedsFix are the "tag" keys whose conversion is not ported.
var tagNeedsFix = []string{
	"BlockEntityTag", "CanDestroy", "CanPlaceOn", "Trim", "Charged", "ChargedProjectiles",
	"Items", "map", "Decorations", "Potion", "CustomPotionColor", "custom_potion_effects",
	"pages", "title", "filtered_title", "author", "generation", "resolved", "effects",
	"DebugProperty", "instrument", "Recipes", "LodestoneTracked", "LodestonePos",
	"LodestoneDimension", "Fireworks", "Explosion", "SkullOwner", "BucketVariantTag",
}

// tagNeedsFixItems are the items whose tag carries more than the generic keys.
var tagNeedsFixItems = map[string]bool{
	"minecraft:potion": true, "minecraft:splash_potion": true, "minecraft:lingering_potion": true,
	"minecraft:tipped_arrow": true, "minecraft:writable_book": true, "minecraft:written_book": true,
	"minecraft:filled_map": true, "minecraft:suspicious_stew": true, "minecraft:firework_rocket": true,
	"minecraft:firework_star": true, "minecraft:player_head": true,
}

// stackTagUnfixable reports whether the tag of the stack of item id holds
// something componentizeTag does not convert.
func stackTagUnfixable(id string, tag nbt.Compound) bool {
	if tagNeedsFixItems[id] {
		return len(tag) > 0
	}
	for _, k := range tagNeedsFix {
		if _, ok := tag[k]; ok {
			return true
		}
	}
	return false
}

// componentizeTag moves the tag of a legacy stack, already renamed, into
// entries["components"]; what is left of the tag becomes custom_data.
// vanilla: ItemStackComponentizationFix.fixItemStack
func componentizeTag(entries nbt.Compound, id string) {
	tag, ok := nbt.CompoundView(entries["tag"])
	delete(entries, "tag")
	if !ok || len(tag) == 0 {
		return
	}
	c := nbt.Compound{}
	take := func(key string) (nbt.Tag, bool) {
		v, ok := tag[key]
		delete(tag, key)
		return v, ok
	}
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
		c["minecraft:entity_data"] = v
	}
	if v, ok := take("Unbreakable"); ok && boolValue(v) {
		u := nbt.Compound{}
		if hide&hideUnbreakable != 0 {
			u["show_in_tooltip"] = nbt.Byte(0)
		}
		c["minecraft:unbreakable"] = u
	}
	componentizeEnchantments(c, tag, "Enchantments", "minecraft:enchantments", hide&hideEnchantments != 0)
	if id == "minecraft:enchanted_book" {
		componentizeEnchantments(c, tag, "StoredEnchantments", "minecraft:stored_enchantments", hide&hideAdditional != 0)
	}
	if display, ok := nbt.CompoundView(tag["display"]); ok {
		componentizeDisplay(c, display, hide)
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
	if hide&hideAdditional != 0 {
		c["minecraft:hide_additional_tooltip"] = nbt.Compound{}
	}
	if len(tag) > 0 {
		c["minecraft:custom_data"] = tag
	}
	if len(c) > 0 {
		entries["components"] = c
	}
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

func componentizeDisplay(c, display nbt.Compound, hide int) {
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
