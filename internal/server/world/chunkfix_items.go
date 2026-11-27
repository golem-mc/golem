package world

import (
	"encoding/json"
	"math"
	"slices"
	"strings"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Data versions of the item component fixes a 1.20.5 to 1.21.4 block entity
// still needs; the generated STRUCTURE chain starts at templateDataVersionFloor.
const (
	attributeModifierIdVersion = 3945 // vanilla: DataFixers.addFixers (AttributeModifierIdFix)
	attributeIdPrefixVersion   = 4055 // vanilla: DataFixers.addFixers (AttributeIdPrefixFix)
	foodConsumableVersion      = 4059 // vanilla: DataFixers.addFixers (FoodToConsumableFix)
	customModelDataVersion     = 4175 // vanilla: DataFixers.addFixers (CustomModelDataExpandFix)
	textComponentVersion       = 4290 // vanilla: DataFixers.addFixers (UnflattenTextComponentFix)
	entityVariantVersion       = 4299 // vanilla: DataFixers.addFixers (EntitySpawnerItemVariantComponentFix)
	tooltipDisplayVersion      = 4307 // vanilla: DataFixers.addFixers (TooltipDisplayComponentFix)
	potDecorationsVersion      = 4996 // vanilla: DataFixers.addFixers (PotDecorationsComponentUnflatteningFix)
	swingAnimationVersion      = 5007 // vanilla: DataFixers.addFixers (SwingAnimationComponentSplitFix)
	explorerMapVersion         = 5008 // vanilla: DataFixers.addFixers (ExplorerMapItemFix, MapColorComponentRemovalFix)
	explorerMapRenameVersion   = 5012 // vanilla: DataFixers.addFixers (ItemRenameFix, explorer map items)
	blockTransformerVersion    = 5014 // vanilla: DataFixers.addFixers (RemoveBlockTransformerComponentFix)
	buriedTreasureMapVersion   = 5016 // vanilla: DataFixers.addFixers (UnfilledBuriedTreasureMapFix)
)

// fixStoredComponents upgrades the components of every stack under tag from
// the shapes of data version version to the current ones.
func fixStoredComponents(tag nbt.Tag, version int32) {
	switch v := tag.(type) {
	case nbt.List:
		for _, item := range v.Items {
			fixStoredComponents(item, version)
		}
	case nbt.CompoundTag:
		entries := v.Entries()
		if id, isStack := entries["id"].(nbt.String); isStack {
			if components, ok := entries["components"].(nbt.CompoundTag); ok {
				fixComponentShapes(components.Entries(), string(id), version)
				fixStackComponents(v, components, version)
			}
		}
		for _, child := range entries {
			fixStoredComponents(child, version)
		}
	}
}

// fixComponentShapes runs the component fixes after version, in version order,
// for the components of a stack of the item id.
func fixComponentShapes(c nbt.Compound, id string, version int32) {
	if version < attributeModifierIdVersion {
		fixAttributeModifierIDs(c)
	}
	if version < attributeIdPrefixVersion {
		fixModifierTypePrefixes(c)
	}
	if version < foodConsumableVersion {
		fixFoodToConsumable(c)
	}
	if version < customModelDataVersion {
		if cmd, ok := c["minecraft:custom_model_data"]; ok {
			n, _ := numberValue(cmd)
			c["minecraft:custom_model_data"] = nbt.Compound{"floats": nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(n)}}}
		}
	}
	if version < textComponentVersion {
		unflattenText(c, "minecraft:custom_name")
		unflattenText(c, "minecraft:item_name")
		if lore, ok := c["minecraft:lore"].(nbt.List); ok {
			items := make([]nbt.Tag, len(lore.Items))
			for i, line := range lore.Items {
				items[i] = unflattenJSON(line)
			}
			c["minecraft:lore"] = mixedList(items)
		}
		unflattenBookText(c)
		for _, key := range []string{"minecraft:entity_data", "minecraft:block_entity_data"} {
			if data, ok := nbt.CompoundView(c[key]); ok {
				unflattenText(data, "CustomName")
			}
		}
	}
	if version < entityVariantVersion {
		fixEntityVariants(c, itemOrNone(id))
	}
	if version < tooltipDisplayVersion {
		fixTooltipDisplay(c)
	}
}

// fixStackComponents runs the fixes of data versions 4996 to 5016 on a stack:
// they delete components and change the item id, so they take the stack's
// CompoundTag, whose key order a plain map write would corrupt.
func fixStackComponents(stack, components nbt.CompoundTag, version int32) {
	if version < potDecorationsVersion {
		if v, ok := components.Entries()["minecraft:pot_decorations"]; ok {
			components.Set("minecraft:pot_decorations", templateUnpackPot(v))
		}
	}
	if version < swingAnimationVersion {
		if animation, ok := components.Entries()["minecraft:swing_animation"]; ok {
			components.Delete("minecraft:swing_animation")
			components.Set("minecraft:attack_animation", animation)
			components.Set("minecraft:interact_animation", animation)
		}
	}
	id, _ := stack.Entries()["id"].(nbt.String)
	if version < explorerMapVersion {
		// vanilla: ExplorerMapItemFix, MapColorComponentRemovalFix
		components.Delete("minecraft:map_color")
		if id == "minecraft:filled_map" {
			templateExplorerMap(stack)
			id, _ = stack.Entries()["id"].(nbt.String)
		}
	}
	if version < explorerMapRenameVersion {
		if renamed, ok := templateExplorerMapRenames[registry.Qualify(string(id))]; ok {
			stack.Set("id", nbt.String(renamed))
		}
	}
	if version < blockTransformerVersion {
		components.Delete("minecraft:block_transformer")
	}
	if version < buriedTreasureMapVersion && id == "minecraft:map" {
		// vanilla: UnfilledBuriedTreasureMapFix
		if name, ok := components.Entries()["minecraft:item_name"].(nbt.CompoundTag); ok && templatePlainTranslation(name) == "filled_map.buried_treasure" {
			name.Set("translate", nbt.String("item.minecraft.buried_treasure_map"))
		}
	}
}

// stripAttributePrefix drops the generic./horse./player./zombie. prefix of an
// attribute id. vanilla: AttributeIdPrefixFix.replaceId
func stripAttributePrefix(id string) string {
	name := strings.TrimPrefix(id, "minecraft:")
	for _, prefix := range []string{"generic.", "horse.", "player.", "zombie."} {
		if rest, ok := strings.CutPrefix(name, prefix); ok {
			return "minecraft:" + rest
		}
	}
	return id
}

// fixModifierTypePrefixes strips the prefix from the type of each modifier in
// a stack's attribute_modifiers. vanilla: AttributesRenameFix.fixDataComponentsOld
func fixModifierTypePrefixes(c nbt.Compound) {
	wrapper, ok := nbt.CompoundView(c["minecraft:attribute_modifiers"])
	if !ok {
		return
	}
	modifiers, _ := wrapper["modifiers"].(nbt.List)
	for _, item := range modifiers.Items {
		if m, ok := nbt.CompoundView(item); ok {
			if t, ok := m["type"].(nbt.String); ok {
				m["type"] = nbt.String(stripAttributePrefix(string(t)))
			}
		}
	}
}

func numberValue(tag nbt.Tag) (float32, bool) {
	switch v := tag.(type) {
	case nbt.Byte:
		return float32(v), true
	case nbt.Short:
		return float32(v), true
	case nbt.Int:
		return float32(v), true
	case nbt.Long:
		return float32(v), true
	case nbt.Float:
		return float32(v), true
	case nbt.Double:
		return float32(v), true
	}
	return 0, false
}

// attributeModifierIDs maps the UUIDs vanilla modifiers had to their ids.
// vanilla: AttributeModifierIdFix.ID_MAP
var attributeModifierIDs = map[[4]int32]string{}

func init() {
	for uuid, id := range map[string]string{
		"736565d2-e1a7-403d-a3f8-1aeb3e302542": "minecraft:creative_mode_block_range",
		"98491ef6-97b1-4584-ae82-71a8cc85cf73": "minecraft:creative_mode_entity_range",
		"91aeaa56-376b-4498-935b-2f7f68070635": "minecraft:effect.speed",
		"7107de5e-7ce8-4030-940e-514c1f160890": "minecraft:effect.slowness",
		"af8b6e3f-3328-4c0a-aa36-5ba2bb9dbef3": "minecraft:effect.haste",
		"55fced67-e92a-486e-9800-b47f202c4386": "minecraft:effect.mining_fatigue",
		"648d7064-6a60-4f59-8abe-c2c23a6dd7a9": "minecraft:effect.strength",
		"c0105bf3-aef8-46b0-9ebc-92943757ccbe": "minecraft:effect.jump_boost",
		"22653b89-116e-49dc-9b6b-9971489b5be5": "minecraft:effect.weakness",
		"5d6f0ba2-1186-46ac-b896-c61c5cee99cc": "minecraft:effect.health_boost",
		"eae29cf0-701e-4ed6-883a-96f798f3dab5": "minecraft:effect.absorption",
		"03c3c89d-7037-4b42-869f-b146bcb64d2e": "minecraft:effect.luck",
		"cc5af142-2bd2-4215-b636-2605aed11727": "minecraft:effect.unluck",
		"6555be74-63b3-41f1-a245-77833b3c2562": "minecraft:evil",
		"1eaf83ff-7207-4596-b37a-d7a07b3ec4ce": "minecraft:powder_snow",
		"662a6b8d-da3e-4c1c-8813-96ea6097278d": "minecraft:sprinting",
		"020e0dfb-87ae-4653-9556-831010e291a0": "minecraft:attacking",
		"766bfa64-11f3-11ea-8d71-362b9e155667": "minecraft:baby",
		"7e0292f2-9434-48d5-a29f-9583af7df27f": "minecraft:covered",
		"9e362924-01de-4ddd-a2b2-d0f7a405a174": "minecraft:suffocating",
		"5cd17e52-a79a-43d3-a529-90fde04b181e": "minecraft:drinking",
		"b9766b59-9566-4402-bc1f-2ee2a276d836": "minecraft:baby",
		"49455a49-7ec5-45ba-b886-3b90b23a1718": "minecraft:attacking",
		"845db27c-c624-495f-8c9f-6020a9a58b6b": "minecraft:armor.boots",
		"d8499b04-0e66-4726-ab29-64469d734e0d": "minecraft:armor.leggings",
		"9f3d476d-c118-4544-8365-64846904b48e": "minecraft:armor.chestplate",
		"2ad3f246-fee1-4e67-b886-69fd380bb150": "minecraft:armor.helmet",
		"c1c72771-8b8e-ba4a-ace0-81a93c8928b2": "minecraft:armor.body",
		"b572ecd2-ac0c-4071-abde-9594af072a37": "minecraft:enchantment.fire_protection",
		"40a9968f-5c66-4e2f-b7f4-2ec2f4b3e450": "minecraft:enchantment.blast_protection",
		"07a65791-f64d-4e79-86c7-f83932f007ec": "minecraft:enchantment.respiration",
		"60b1b7db-fffd-4ad0-817c-d6c6a93d8a45": "minecraft:enchantment.aqua_affinity",
		"11dc269a-4476-46c0-aff3-9e17d7eb6801": "minecraft:enchantment.depth_strider",
		"87f46a96-686f-4796-b035-22e16ee9e038": "minecraft:enchantment.soul_speed",
		"b9716dbd-50df-4080-850e-70347d24e687": "minecraft:enchantment.soul_speed",
		"92437d00-c3a7-4f2e-8f6c-1f21585d5dd0": "minecraft:enchantment.swift_sneak",
		"5d3d087b-debe-4037-b53e-d84f3ff51f17": "minecraft:enchantment.sweeping_edge",
		"3ceb37c0-db62-46b5-bd02-785457b01d96": "minecraft:enchantment.efficiency",
		"cb3f55d3-645c-4f38-a497-9c13a33db5cf": "minecraft:base_attack_damage",
		"fa233e1c-4180-4865-b01b-bcce9785aca3": "minecraft:base_attack_speed",
	} {
		key, _ := parseUUIDInts(uuid)
		attributeModifierIDs[key] = id
	}
}

// attributeModifierNames maps the names of modifiers without a known UUID.
// vanilla: AttributeModifierIdFix.NAME_MAP
var attributeModifierNames = map[string]string{
	"Random spawn bonus":                 "minecraft:random_spawn_bonus",
	"Random zombie-spawn bonus":          "minecraft:zombie_random_spawn_bonus",
	"Leader zombie bonus":                "minecraft:leader_zombie_bonus",
	"Zombie reinforcement callee charge": "minecraft:reinforcement_callee_charge",
	"Zombie reinforcement caller charge": "minecraft:reinforcement_caller_charge",
}

// parseUUIDInts parses a dashed UUID into the four ints an NBT UUID is stored as.
func parseUUIDInts(s string) (out [4]int32, ok bool) {
	hex := strings.ReplaceAll(s, "-", "")
	if len(hex) != 32 {
		return out, false
	}
	for i := range out {
		var n uint32
		for _, ch := range hex[i*8 : i*8+8] {
			d := strings.IndexRune("0123456789abcdef", ch)
			if d < 0 {
				return out, false
			}
			n = n<<4 | uint32(d)
		}
		out[i] = int32(n)
	}
	return out, true
}

// fixAttributeModifierIDs replaces each modifier's uuid and name by an id,
// merging modifiers that share a named id.
// vanilla: AttributeModifierIdFix.fixModifiers
func fixAttributeModifierIDs(c nbt.Compound) {
	wrapper, ok := nbt.CompoundView(c["minecraft:attribute_modifiers"])
	if !ok {
		return
	}
	if modifiers, ok := wrapper["modifiers"].(nbt.List); ok {
		wrapper["modifiers"] = fixModifierIDs(modifiers)
	}
}

// fixModifierIDs gives each modifier of the list an id in place of its uuid
// and name, merging the ones that share a named id.
func fixModifierIDs(modifiers nbt.List) nbt.List {
	var out []nbt.Tag
	index := map[string]int{}
	for _, item := range modifiers.Items {
		m, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		var uuid [4]int32
		haveUUID := false
		if arr, ok := m["uuid"].(nbt.IntArray); ok && len(arr) == 4 {
			uuid, haveUUID = [4]int32(arr), true
		}
		name, _ := nbt.Get[nbt.String](m, "name")
		idFromUUID, byUUID := attributeModifierIDs[uuid]
		byUUID = byUUID && haveUUID
		idFromName, byName := attributeModifierNames[string(name)]
		var id string
		switch {
		case byUUID:
			id = idFromUUID
		case byName:
			id = idFromName
			if at, dup := index[id]; dup {
				prev, _ := nbt.CompoundView(out[at])
				sum := nbt.DoubleFieldOr(prev, "amount", 0) + nbt.DoubleFieldOr(m, "amount", 0)
				prev["amount"] = nbt.Double(sum)
				continue
			}
		case haveUUID:
			id = "minecraft:" + uuidString(uuid)
		default:
			id = "minecraft:unknown"
		}
		m["id"] = nbt.String(id)
		delete(m, "uuid")
		delete(m, "name")
		if at, dup := index[id]; dup {
			out[at] = m
		} else {
			index[id] = len(out)
			out = append(out, m)
		}
	}
	return nbt.List{Elem: nbt.TypeCompound, Items: out}
}

func uuidString(u [4]int32) string {
	const digits = "0123456789abcdef"
	var b strings.Builder
	for _, n := range u {
		for shift := 28; shift >= 0; shift -= 4 {
			b.WriteByte(digits[uint32(n)>>shift&15])
		}
	}
	s := b.String()
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}

// fixFoodToConsumable moves the eating time and effects of a food component
// into a consumable component.
// vanilla: FoodToConsumableFix
func fixFoodToConsumable(c nbt.Compound) {
	food, ok := nbt.CompoundView(c["minecraft:food"])
	if !ok {
		return
	}
	seconds := nbt.Float(1.6)
	if v, ok := nbt.FloatField(food, "eat_seconds"); ok {
		seconds = nbt.Float(v)
	}
	var effects []nbt.Tag
	if list, ok := food["effects"].(nbt.List); ok {
		for _, item := range list.Items {
			effect, _ := nbt.CompoundView(item)
			probability, ok := nbt.FloatField(effect, "probability")
			if !ok {
				probability = 1
			}
			apply := nbt.Compound{"type": nbt.String("minecraft:apply_effects"), "probability": nbt.Float(probability)}
			if e, ok := effect["effect"]; ok {
				apply["effects"] = nbt.List{Elem: e.Type(), Items: []nbt.Tag{e}}
			} else {
				apply["effects"] = nbt.List{Elem: nbt.TypeEnd}
			}
			effects = append(effects, apply)
		}
	}
	if remainder, ok := food["using_converts_to"]; ok {
		c["minecraft:use_remainder"] = remainder
	}
	delete(food, "eat_seconds")
	delete(food, "effects")
	delete(food, "using_converts_to")
	list := nbt.List{Elem: nbt.TypeCompound, Items: effects}
	if len(effects) == 0 {
		list.Elem = nbt.TypeEnd
	}
	c["minecraft:consumable"] = nbt.Compound{"consume_seconds": seconds, "on_consume_effects": list}
}

func unflattenText(c nbt.Compound, key string) {
	if v, ok := c[key]; ok {
		c[key] = unflattenJSON(v)
	}
}

// unflattenJSON parses a text component stored as a JSON string; a string that
// is not JSON stays as it is.
// vanilla: UnflattenTextComponentFix.unflattenJson
func unflattenJSON(tag nbt.Tag) nbt.Tag {
	s, ok := tag.(nbt.String)
	if !ok {
		return tag
	}
	var value any
	if err := json.Unmarshal([]byte(s), &value); err != nil || value == nil {
		return tag
	}
	return jsonToNBT(value)
}

// jsonToNBT converts a decoded JSON value as vanilla's JsonOps conversion
// does: whole numbers are ints, others doubles, booleans bytes.
func jsonToNBT(v any) nbt.Tag {
	switch v := v.(type) {
	case string:
		return nbt.String(v)
	case bool:
		if v {
			return nbt.Byte(1)
		}
		return nbt.Byte(0)
	case float64:
		if v == math.Trunc(v) && math.Abs(v) <= math.MaxInt32 {
			return nbt.Int(int32(v))
		}
		return nbt.Double(v)
	case []any:
		items := make([]nbt.Tag, len(v))
		for i, item := range v {
			items[i] = jsonToNBT(item)
		}
		return mixedList(items)
	case map[string]any:
		out := make(nbt.Compound, len(v))
		for key, item := range v {
			out[key] = jsonToNBT(item)
		}
		return out
	}
	return nbt.String("")
}

// mixedList is a list whose items may differ in type: vanilla stores each in
// a one-key compound then, which the codec reads back to the same values.
func mixedList(items []nbt.Tag) nbt.List {
	if len(items) == 0 {
		return nbt.List{Elem: nbt.TypeEnd}
	}
	elem := items[0].Type()
	if slices.ContainsFunc(items, func(t nbt.Tag) bool { return t.Type() != elem }) {
		for i, item := range items {
			items[i] = nbt.Compound{"": item}
		}
		elem = nbt.TypeCompound
	}
	return nbt.List{Elem: elem, Items: items}
}

// fixTooltipDisplay moves the show_in_tooltip flags and hide_* components into
// minecraft:tooltip_display and unwraps the components that only wrapped the
// flag.
// vanilla: TooltipDisplayComponentFix
func fixTooltipDisplay(c nbt.Compound) {
	var hidden []string
	hide := func(name string) {
		if !slices.Contains(hidden, name) {
			hidden = append(hidden, name)
		}
	}
	for _, name := range []string{"minecraft:can_place_on", "minecraft:can_break"} {
		if wrapper, ok := nbt.CompoundView(c[name]); ok {
			if predicates, ok := wrapper["predicates"]; ok {
				if shown, ok := wrapper["show_in_tooltip"]; ok && !boolValue(shown) {
					hide(name)
				}
				c[name] = predicates
			}
		}
	}
	for _, f := range []struct{ name, field string }{
		{"minecraft:trim", ""}, {"minecraft:unbreakable", ""}, {"minecraft:dyed_color", "rgb"},
		{"minecraft:attribute_modifiers", "modifiers"}, {"minecraft:enchantments", "levels"},
		{"minecraft:stored_enchantments", "levels"}, {"minecraft:jukebox_playable", "song"},
	} {
		component, ok := nbt.CompoundView(c[f.name])
		if !ok {
			continue
		}
		if shown, ok := component["show_in_tooltip"]; ok && !boolValue(shown) {
			hide(f.name)
		}
		delete(component, "show_in_tooltip")
		if inner, ok := component[f.field]; f.field != "" && ok {
			c[f.name] = inner
		}
	}
	_, hideTooltip := c["minecraft:hide_tooltip"]
	delete(c, "minecraft:hide_tooltip")
	if _, hideAdditional := c["minecraft:hide_additional_tooltip"]; hideAdditional {
		delete(c, "minecraft:hide_additional_tooltip")
		for _, name := range additionalTooltipComponents {
			if _, ok := c[name]; ok {
				hide(name)
			}
		}
	}
	if len(hidden) == 0 && !hideTooltip {
		return
	}
	slices.Sort(hidden)
	names := make([]nbt.Tag, len(hidden))
	for i, name := range hidden {
		names[i] = nbt.String(name)
	}
	list := nbt.List{Elem: nbt.TypeString, Items: names}
	if len(names) == 0 {
		list.Elem = nbt.TypeEnd
	}
	c["minecraft:tooltip_display"] = nbt.Compound{"hide_tooltip": boolTag(hideTooltip), "hidden_components": list}
}

func boolTag(b bool) nbt.Byte {
	if b {
		return 1
	}
	return 0
}

// boolValue reads a lenient boolean: any non-zero number is true.
func boolValue(tag nbt.Tag) bool {
	n, _ := numberValue(tag)
	return n != 0
}

// additionalTooltipComponents are the components hide_additional_tooltip hid.
// vanilla: TooltipDisplayComponentFix.CONVERTED_ADDITIONAL_TOOLTIP_TYPES
var additionalTooltipComponents = []string{"minecraft:banner_patterns", "minecraft:bees", "minecraft:block_entity_data", "minecraft:block_state", "minecraft:bundle_contents", "minecraft:charged_projectiles", "minecraft:container", "minecraft:container_loot", "minecraft:firework_explosion", "minecraft:fireworks", "minecraft:instrument", "minecraft:map_id", "minecraft:painting/variant", "minecraft:pot_decorations", "minecraft:potion_contents", "minecraft:tropical_fish/pattern", "minecraft:written_book_content"}

// unflattenBookText parses the JSON of a written book's pages.
// vanilla: UnflattenTextComponentFix (written_book_content)
func unflattenBookText(c nbt.Compound) {
	book, ok := nbt.CompoundView(c["minecraft:written_book_content"])
	if !ok {
		return
	}
	pages, _ := book["pages"].(nbt.List)
	for _, page := range pages.Items {
		if p, ok := nbt.CompoundView(page); ok {
			unflattenText(p, "raw")
			unflattenText(p, "filtered")
		}
	}
}

// tropicalPatterns maps the pattern bits of a packed tropical fish variant.
// vanilla: EntitySpawnerItemVariantComponentFix.getPattern
var tropicalPatterns = map[int32]string{
	256: "sunstreak", 512: "snooper", 768: "dasher", 1024: "brinely", 1280: "spotty",
	1: "flopper", 257: "stripey", 513: "glitter", 769: "blockfish", 1025: "betty", 1281: "clayfish",
}

var axolotlVariants = []string{"lucy", "wild", "gold", "cyan", "blue"}

// fixEntityVariants moves the variant of a bucket, or the painting variant of
// a painting, out of its entity data into its own component.
// vanilla: EntitySpawnerItemVariantComponentFix
func fixEntityVariants(c nbt.Compound, item registry.Item) {
	bucket, _ := nbt.CompoundView(c["minecraft:bucket_entity_data"])
	switch item {
	case registry.ItemSalmonBucket:
		if v, ok := takeKey(bucket, "type"); ok {
			c["minecraft:salmon/size"] = v
		}
	case registry.ItemAxolotlBucket:
		if v, ok := takeKey(bucket, "Variant"); ok {
			if n, isNum := nbt.IntValue(v); isNum {
				variant := "lucy"
				if n >= 1 && int(n) < len(axolotlVariants) {
					variant = axolotlVariants[n]
				}
				c["minecraft:axolotl/variant"] = nbt.String(variant)
			} else {
				bucket["Variant"] = v
			}
		}
	case registry.ItemTropicalFishBucket:
		if v, ok := bucket["BucketVariantTag"]; ok {
			if packed, isNum := nbt.IntValue(v); isNum {
				pattern, known := tropicalPatterns[packed&0xFFFF]
				if !known {
					pattern = "kob"
				}
				delete(bucket, "BucketVariantTag")
				c["minecraft:tropical_fish/pattern"] = nbt.String(pattern)
				c["minecraft:tropical_fish/base_color"] = nbt.String(dyeName(packed >> 16 & 0xFF))
				c["minecraft:tropical_fish/pattern_color"] = nbt.String(dyeName(packed >> 24 & 0xFF))
			}
		}
	case registry.ItemPainting:
		data, ok := nbt.CompoundView(c["minecraft:entity_data"])
		if !ok || data["id"] != nbt.String("minecraft:painting") {
			return
		}
		variant, hasVariant := takeKey(data, "variant")
		if len(data) == 1 {
			delete(c, "minecraft:entity_data")
		}
		if hasVariant {
			c["minecraft:painting/variant"] = variant
		}
	}
}
