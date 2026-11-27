package world

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

var (
	potionHolderItems = []string{"minecraft:potion", "minecraft:splash_potion", "minecraft:lingering_potion", "minecraft:tipped_arrow"}
	bucketedMobItems  = []string{"minecraft:pufferfish_bucket", "minecraft:salmon_bucket", "minecraft:cod_bucket", "minecraft:tropical_fish_bucket", "minecraft:axolotl_bucket", "minecraft:tadpole_bucket"}
	bucketedMobTags   = []string{"NoAI", "Silent", "NoGravity", "Glowing", "Invulnerable", "Health", "Age", "Variant", "HuntingCooldown", "BucketVariantTag"}
	containerBEs      = []string{
		"minecraft:shulker_box", "minecraft:chest", "minecraft:trapped_chest", "minecraft:furnace", "minecraft:ender_chest",
		"minecraft:dispenser", "minecraft:dropper", "minecraft:brewing_stand", "minecraft:hopper", "minecraft:barrel",
		"minecraft:smoker", "minecraft:blast_furnace", "minecraft:campfire", "minecraft:chiseled_bookshelf", "minecraft:crafter",
	}
	dyeNames = []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray", "light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}
)

// componentizeBlockEntityTag turns a stack's BlockEntityTag into the
// components of the block entity's kind; what is left becomes
// minecraft:block_entity_data (kept with its id).
// vanilla: ItemStackComponentizationFix.fixBlockEntityTag
func componentizeBlockEntityTag(c, tag nbt.Compound, itemID string) {
	be, ok := nbt.CompoundView(tag["BlockEntityTag"])
	if !ok {
		return
	}
	delete(tag, "BlockEntityTag")
	id, _ := be["id"].(nbt.String)
	if id == "" {
		// vanilla: V99.addNames. A superset: vanilla (V704.ITEM_TO_BLOCKENTITY)
		// names only some items, and leaves the id off for the rest.
		id = nbt.String(registry.BlockEntityTypes[itemID])
		if id != "" {
			be["id"] = id
		}
	}
	name := string(id)
	if name != "" && !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	if v, ok := takeKey(be, "Lock"); ok {
		c["minecraft:lock"] = v
	}
	if table, ok := takeKey(be, "LootTable"); ok {
		loot := nbt.Compound{"loot_table": table}
		seedTag, _ := takeKey(be, "LootTableSeed")
		if seed, ok := seedTag.(nbt.Long); ok && seed != 0 {
			loot["seed"] = seed
		} else if n, ok := nbt.IntValue(seedTag); ok && n != 0 {
			loot["seed"] = nbt.Long(n)
		}
		c["minecraft:container_loot"] = loot
	}
	switch {
	case name == "minecraft:skull":
		if v, ok := takeKey(be, "note_block_sound"); ok {
			c["minecraft:note_block_sound"] = v
		}
	case name == "minecraft:decorated_pot":
		if v, ok := takeKey(be, "sherds"); ok {
			c["minecraft:pot_decorations"] = v
		}
		if item, ok := takeKey(be, "item"); ok {
			c["minecraft:container"] = nbt.ListOf([]nbt.Tag{nbt.Compound{"slot": nbt.Int(0), "item": item}})
		}
	case name == "minecraft:banner":
		if v, ok := takeKey(be, "patterns"); ok {
			c["minecraft:banner_patterns"] = v
		}
		if base, ok := takeKey(be, "Base"); ok {
			if n, isNum := nbt.IntValue(base); isNum {
				color := "white"
				if n >= 0 && int(n) < len(dyeNames) {
					color = dyeNames[n]
				}
				c["minecraft:base_color"] = nbt.String(color)
			}
		}
	case slices.Contains(containerBEs, name):
		if items, ok := takeKey(be, "Items"); ok {
			list, _ := items.(nbt.List)
			slots := make([]nbt.Tag, 0, len(list.Items))
			for _, it := range list.Items {
				stack, _ := nbt.CompoundView(it)
				slot, _ := nbt.IntValue(stack["Slot"])
				delete(stack, "Slot")
				slots = append(slots, nbt.Compound{"slot": nbt.Int(slot & 0xFF), "item": stack})
			}
			if len(slots) > 0 {
				c["minecraft:container"] = nbt.ListOf(slots)
			}
		}
	case name == "minecraft:beehive":
		if v, ok := takeKey(be, "bees"); ok {
			c["minecraft:bees"] = v
		}
	}
	// An id with nothing else is an empty sub-tag in vanilla and is dropped.
	if _, hasID := be["id"]; len(be) > 1 || (len(be) == 1 && !hasID) {
		c["minecraft:block_entity_data"] = be
	}
}

// componentizeBlockPredicates turns a CanDestroy or CanPlaceOn list of
// "block[state]{nbt}" strings into predicates.
// vanilla: ItemStackComponentizationFix.fixBlockStatePredicates
func componentizeBlockPredicates(c, tag nbt.Compound, key, component string, hideInTooltip bool) {
	old, ok := takeKey(tag, key)
	if !ok {
		return
	}
	list, _ := old.(nbt.List)
	predicates := make([]nbt.Tag, 0, len(list.Items))
	for _, p := range list.Items {
		if str, isStr := p.(nbt.String); isStr {
			p = blockPredicate(string(str))
		}
		predicates = append(predicates, p)
	}
	comp := nbt.Compound{"predicates": nbt.ListOf(predicates)}
	if hideInTooltip {
		comp["show_in_tooltip"] = nbt.Byte(0)
	}
	c[component] = comp
}

func blockPredicate(s string) nbt.Tag {
	props, nbtAt := strings.IndexByte(s, '['), strings.IndexByte(s, '{')
	nameEnd := len(s)
	if props != -1 {
		nameEnd = props
	}
	if nbtAt != -1 {
		nameEnd = min(nameEnd, nbtAt)
	}
	pred := nbt.Compound{"blocks": nbt.String(strings.TrimSpace(s[:nameEnd]))}
	if end := strings.IndexByte(s, ']'); props != -1 && end != -1 {
		state := nbt.Compound{}
		for prop := range strings.SplitSeq(s[props+1:end], ",") {
			if before, after, ok := strings.Cut(prop, "="); ok {
				state[strings.TrimSpace(before)] = nbt.String(strings.TrimSpace(after))
			}
		}
		pred["state"] = state
	}
	if end := strings.IndexByte(s, '}'); nbtAt != -1 && end != -1 {
		pred["nbt"] = nbt.String(s[nbtAt : end+1])
	}
	return pred
}

// componentizeItemKind moves the tag keys only some items carry.
// vanilla: ItemStackComponentizationFix.fixItemStack
func componentizeItemKind(c, tag nbt.Compound, id string) {
	item := itemOrNone(id)
	move := func(key, component string) {
		if v, ok := takeKey(tag, key); ok {
			c[component] = v
		}
	}
	switch {
	case item == registry.ItemCrossbow:
		delete(tag, "Charged")
		if v, ok := takeKey(tag, "ChargedProjectiles"); ok {
			if l, isList := v.(nbt.List); !isList || len(l.Items) > 0 {
				c["minecraft:charged_projectiles"] = v
			}
		}
	case item == registry.ItemBundle:
		if v, ok := takeKey(tag, "Items"); ok {
			if l, isList := v.(nbt.List); !isList || len(l.Items) > 0 {
				c["minecraft:bundle_contents"] = v
			}
		}
	case item == registry.ItemFilledMap:
		move("map", "minecraft:map_id")
		if v, ok := takeKey(tag, "Decorations"); ok {
			list, _ := v.(nbt.List)
			decorations := nbt.Compound{}
			for _, d := range list.Items {
				key, value := mapDecoration(d)
				if _, dup := decorations[key]; !dup {
					decorations[key] = value
				}
			}
			if len(decorations) > 0 {
				c["minecraft:map_decorations"] = decorations
			}
		}
	case slices.Contains(potionHolderItems, id):
		potion := nbt.Compound{}
		if p, ok := takeKey(tag, "Potion"); ok && p != nbt.String("minecraft:empty") {
			if _, isStr := p.(nbt.String); isStr {
				potion["potion"] = p
			}
		}
		if v, ok := takeKey(tag, "CustomPotionColor"); ok {
			potion["custom_color"] = v
		}
		if v, ok := takeKey(tag, "custom_potion_effects"); ok {
			potion["custom_effects"] = v
		}
		if len(potion) > 0 {
			c["minecraft:potion_contents"] = potion
		}
	case item == registry.ItemWritableBook:
		if pages := bookPages(tag); pages != nil {
			c["minecraft:writable_book_content"] = nbt.Compound{"pages": pages}
		}
	case item == registry.ItemWrittenBook:
		pages := bookPages(tag)
		title, _ := takeKey(tag, "title")
		filtered, hasFiltered := takeKey(tag, "filtered_title")
		book := nbt.Compound{"title": filteredText(title, filtered, hasFiltered)}
		for _, k := range []string{"author", "resolved", "generation"} {
			if v, ok := takeKey(tag, k); ok {
				book[k] = v
			}
		}
		if pages != nil {
			book["pages"] = pages
		}
		c["minecraft:written_book_content"] = book
	case item == registry.ItemSuspiciousStew:
		move("effects", "minecraft:suspicious_stew_effects")
	case item == registry.ItemDebugStick:
		move("DebugProperty", "minecraft:debug_stick_state")
	case slices.Contains(bucketedMobItems, id):
		data := nbt.Compound{}
		for _, k := range bucketedMobTags {
			if v, ok := takeKey(tag, k); ok {
				data[k] = v
			}
		}
		if len(data) > 0 {
			c["minecraft:bucket_entity_data"] = data
		}
	case item == registry.ItemGoatHorn:
		move("instrument", "minecraft:instrument")
	case item == registry.ItemKnowledgeBook:
		move("Recipes", "minecraft:recipes")
	case item == registry.ItemCompass:
		pos, hasPos := takeKey(tag, "LodestonePos")
		dim, hasDim := takeKey(tag, "LodestoneDimension")
		if hasPos || hasDim {
			tracked, hasTracked := takeKey(tag, "LodestoneTracked")
			comp := nbt.Compound{}
			if hasPos && hasDim {
				comp["target"] = nbt.Compound{"pos": pos, "dimension": dim}
			}
			if hasTracked && !boolValue(tracked) {
				comp["tracked"] = nbt.Byte(0)
			}
			c["minecraft:lodestone_tracker"] = comp
		}
	case item == registry.ItemFireworkRocket:
		if fw, ok := nbt.CompoundView(tag["Fireworks"]); ok {
			list, _ := fw["Explosions"].(nbt.List)
			explosions := make([]nbt.Tag, 0, len(list.Items))
			for _, e := range list.Items {
				explosions = append(explosions, fireworkExplosion(e))
			}
			flight, _ := nbt.IntValue(fw["Flight"])
			c["minecraft:fireworks"] = nbt.Compound{"explosions": nbt.ListOf(explosions), "flight_duration": nbt.Byte(flight)}
			delete(fw, "Explosions")
			delete(fw, "Flight")
			if len(fw) == 0 {
				delete(tag, "Fireworks")
			}
		}
	case item == registry.ItemFireworkStar:
		if ex, ok := nbt.CompoundView(tag["Explosion"]); ok {
			c["minecraft:firework_explosion"] = fireworkExplosion(ex)
			for _, k := range []string{"Type", "Colors", "FadeColors", "Trail", "Flicker"} {
				delete(ex, k)
			}
			if len(ex) == 0 {
				delete(tag, "Explosion")
			}
		}
	case item == registry.ItemPlayerHead:
		if v, ok := takeKey(tag, "SkullOwner"); ok {
			c["minecraft:profile"] = skullProfile(v)
		}
	}
}

func filteredText(raw, filtered nbt.Tag, hasFiltered bool) nbt.Tag {
	text, _ := raw.(nbt.String)
	out := nbt.Compound{"raw": text}
	if f, ok := filtered.(nbt.String); ok && hasFiltered {
		out["filtered"] = f
	}
	return out
}

// bookPages is the pages of a book as filtered texts, nil when it has none.
// vanilla: ItemStackComponentizationFix.fixBookPages
func bookPages(tag nbt.Compound) nbt.Tag {
	pages, _ := takeKey(tag, "pages")
	filtered, _ := takeKey(tag, "filtered_pages")
	list, _ := pages.(nbt.List)
	if len(list.Items) == 0 {
		return nil
	}
	fp, _ := nbt.CompoundView(filtered)
	out := make([]nbt.Tag, len(list.Items))
	for i, p := range list.Items {
		f, has := fp[strconv.Itoa(i)]
		out[i] = filteredText(p, f, has)
	}
	return nbt.ListOf(out)
}

var fireworkShapes = []string{"small_ball", "large_ball", "star", "creeper", "burst"}

// fireworkExplosion renames an explosion's fields.
// vanilla: ItemStackComponentizationFix.fixFireworkExplosion
func fireworkExplosion(tag nbt.Tag) nbt.Tag {
	e, ok := nbt.CompoundView(tag)
	if !ok {
		return tag
	}
	out := nbt.Compound{}
	maps.Copy(out, e)
	n, _ := nbt.IntValue(e["Type"])
	shape := "small_ball"
	if n >= 0 && int(n) < len(fireworkShapes) {
		shape = fireworkShapes[n]
	}
	out["shape"] = nbt.String(shape)
	delete(out, "Type")
	for from, to := range map[string]string{"Colors": "colors", "FadeColors": "fade_colors", "Trail": "has_trail", "Flicker": "has_twinkle"} {
		renameField(out, from, to)
	}
	return out
}

var mapDecorationTypes = []string{
	"player", "frame", "red_marker", "blue_marker", "target_x", "target_point", "player_off_map", "player_off_limits",
	"mansion", "monument", "banner_white", "banner_orange", "banner_magenta", "banner_light_blue", "banner_yellow",
	"banner_lime", "banner_pink", "banner_gray", "banner_light_gray", "banner_cyan", "banner_purple", "banner_blue",
	"banner_brown", "banner_green", "banner_red", "banner_black", "red_x", "village_desert", "village_plains",
	"village_savanna", "village_snowy", "village_taiga", "jungle_temple", "swamp_hut",
}

// mapDecoration is one Decorations entry as a map_decorations key and value.
// vanilla: ItemStackComponentizationFix.fixMapDecoration
func mapDecoration(tag nbt.Tag) (string, nbt.Tag) {
	d, _ := nbt.CompoundView(tag)
	id, _ := d["id"].(nbt.String)
	typ, _ := nbt.IntValue(d["type"])
	name := "player"
	if typ >= 0 && int(typ) < len(mapDecorationTypes) {
		name = mapDecorationTypes[typ]
	}
	return string(id), nbt.Compound{
		"type": nbt.String(name), "x": nbt.Double(nbt.DoubleFieldOr(d, "x", 0)),
		"z": nbt.Double(nbt.DoubleFieldOr(d, "z", 0)), "rotation": nbt.Float(nbt.DoubleFieldOr(d, "rot", 0)),
	}
}

func validPlayerName(name string) bool {
	if len(name) > 16 {
		return false
	}
	for _, r := range name {
		if r <= 32 || r >= 127 {
			return false
		}
	}
	return true
}

// skullProfile turns a SkullOwner into a profile.
// vanilla: ItemStackComponentizationFix.fixProfile
func skullProfile(tag nbt.Tag) nbt.Tag {
	if name, ok := tag.(nbt.String); ok {
		if validPlayerName(string(name)) {
			return nbt.Compound{"name": name}
		}
		return nbt.Compound{}
	}
	d, _ := nbt.CompoundView(tag)
	out := nbt.Compound{}
	if name, _ := d["Name"].(nbt.String); validPlayerName(string(name)) {
		out["name"] = name
	}
	if id, ok := d["Id"]; ok {
		out["id"] = id
	}
	if props, ok := nbt.CompoundView(d["Properties"]); ok {
		var list []nbt.Tag
		for _, name := range slices.Sorted(maps.Keys(props)) {
			entries, _ := props[name].(nbt.List)
			for _, e := range entries.Items {
				ec, _ := nbt.CompoundView(e)
				value, _ := ec["Value"].(nbt.String)
				p := nbt.Compound{"name": nbt.String(name), "value": value}
				if sig, ok := ec["Signature"].(nbt.String); ok {
					p["signature"] = sig
				}
				list = append(list, p)
			}
		}
		if len(list) > 0 {
			out["properties"] = nbt.ListOf(list)
		}
	}
	return out
}

// itemOrNone returns the item a name stands for, or -1 when this version has
// no such item.
func itemOrNone(name string) registry.Item {
	if n, ok := registry.ItemID(name); ok {
		return registry.Item(n)
	}
	return -1
}
