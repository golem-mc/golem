package world

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func chestWith(components nbt.Compound) nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:chest"), "x": nbt.Int(16), "y": nbt.Int(64), "z": nbt.Int(32),
		"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"Slot": nbt.Byte(0), "id": nbt.String("minecraft:diamond_sword"), "count": nbt.Int(1), "components": components},
		}},
	}
}

func TestOldChestEnchantedSwordLoads(t *testing.T) {
	t.Parallel()
	for _, version := range []int32{4189, 3837} {
		enchants := nbt.Compound{"levels": nbt.Compound{"minecraft:sharpness": nbt.Int(5)}, "show_in_tooltip": nbt.Byte(1)}
		w, pos := oldChunkWorld(t, version, chestWith(nbt.Compound{"minecraft:enchantments": enchants}))
		w.Acquire(pos)
		be := w.BlockEntity(16, 64, 32)
		if be == nil {
			t.Fatalf("%d: the chest was not loaded", version)
		}
		sharpness, _ := registry.EnchantmentID("minecraft:sharpness")
		stack := be.Slots[0]
		if stack.ItemID != int32(registry.ItemDiamondSword) || stack.Enchantments[sharpness] != 5 || be.Stored != nil {
			t.Errorf("%d: slot 0 = %+v, stored %v", version, stack, be.Stored)
		}
	}
}

func TestFixComponentShapes(t *testing.T) {
	t.Parallel()
	c := nbt.Compound{
		"minecraft:custom_model_data": nbt.Int(7),
		"minecraft:custom_name":       nbt.String(`{"text":"Hi","bold":true}`),
		"minecraft:food":              nbt.Compound{"nutrition": nbt.Int(4), "saturation": nbt.Float(1), "eat_seconds": nbt.Float(0.5)},
		"minecraft:unbreakable":       nbt.Compound{"show_in_tooltip": nbt.Byte(0)},
		"minecraft:attribute_modifiers": nbt.Compound{"show_in_tooltip": nbt.Byte(1), "modifiers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"name": nbt.String("Random spawn bonus"), "amount": nbt.Double(1)},
			nbt.Compound{"name": nbt.String("Random spawn bonus"), "amount": nbt.Double(2)},
		}}},
	}
	fixComponentShapes(c, "minecraft:diamond_sword", 3837)
	cmd, _ := nbt.CompoundView(c["minecraft:custom_model_data"])
	if floats, _ := cmd["floats"].(nbt.List); len(floats.Items) != 1 || floats.Items[0] != nbt.Float(7) {
		t.Errorf("custom_model_data = %v", c["minecraft:custom_model_data"])
	}
	name, _ := nbt.CompoundView(c["minecraft:custom_name"])
	if name["text"] != nbt.String("Hi") || name["bold"] != nbt.Byte(1) {
		t.Errorf("custom_name = %v", c["minecraft:custom_name"])
	}
	consumable, _ := nbt.CompoundView(c["minecraft:consumable"])
	if consumable["consume_seconds"] != nbt.Float(0.5) {
		t.Errorf("consumable = %v", consumable)
	}
	display, _ := nbt.CompoundView(c["minecraft:tooltip_display"])
	hidden, _ := display["hidden_components"].(nbt.List)
	if len(hidden.Items) != 1 || hidden.Items[0] != nbt.String("minecraft:unbreakable") {
		t.Errorf("tooltip_display = %v", display)
	}
	mods, _ := c["minecraft:attribute_modifiers"].(nbt.List)
	if len(mods.Items) != 1 {
		t.Fatalf("attribute_modifiers = %v", c["minecraft:attribute_modifiers"])
	}
	m, _ := nbt.CompoundView(mods.Items[0])
	if m["id"] != nbt.String("minecraft:random_spawn_bonus") || m["amount"] != nbt.Double(3) || m["name"] != nil {
		t.Errorf("modifier = %v", m)
	}
}

func TestComponentizeTag(t *testing.T) {
	t.Parallel()
	stack := func(id string, tag nbt.Compound) nbt.Compound {
		return nbt.Compound{"id": nbt.String(id), "Count": nbt.Byte(1), "tag": tag}
	}
	entries := stack("minecraft:diamond_pickaxe", nbt.Compound{
		"Damage": nbt.Int(7), "RepairCost": nbt.Int(0), "HideFlags": nbt.Int(1 | 4), "Unbreakable": nbt.Byte(1),
		"Enchantments": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"id": nbt.String("minecraft:efficiency"), "lvl": nbt.Short(3)},
			nbt.Compound{"id": nbt.String("minecraft:unbreaking"), "lvl": nbt.Short(0)},
		}},
		"display": nbt.Compound{"Name": nbt.String(`{"text":"Pick"}`), "Lore": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("plain")}}, "color": nbt.Int(255), "Other": nbt.Byte(1)},
		"Mine":    nbt.String("x"),
	})
	componentizeTag(entries, "minecraft:diamond_pickaxe")
	c := entries["components"].(nbt.Compound)
	if _, ok := entries["tag"]; ok {
		t.Error("the tag was kept")
	}
	if c["minecraft:damage"] != nbt.Int(7) || c["minecraft:repair_cost"] != nil {
		t.Errorf("damage %v, repair cost %v", c["minecraft:damage"], c["minecraft:repair_cost"])
	}
	ench := c["minecraft:enchantments"].(nbt.Compound)
	if lv := ench["levels"].(nbt.Compound); len(lv) != 1 || lv["minecraft:efficiency"] != nbt.Int(3) || ench["show_in_tooltip"] != nbt.Byte(0) {
		t.Errorf("enchantments = %v", ench)
	}
	if u := c["minecraft:unbreakable"].(nbt.Compound); u["show_in_tooltip"] != nbt.Byte(0) {
		t.Errorf("unbreakable = %v", u)
	}
	if c["minecraft:custom_name"] != nbt.String(`{"text":"Pick"}`) {
		t.Errorf("custom_name = %v", c["minecraft:custom_name"])
	}
	if lore := c["minecraft:lore"].(nbt.List); len(lore.Items) != 0 {
		t.Errorf("a lore line that is not JSON was kept: %v", lore)
	}
	if dye := c["minecraft:dyed_color"].(nbt.Compound); dye["rgb"] != nbt.Int(255) {
		t.Errorf("dyed_color = %v", dye)
	}
	data := c["minecraft:custom_data"].(nbt.Compound)
	if data["Mine"] != nbt.String("x") || data["display"].(nbt.Compound)["Other"] != nbt.Byte(1) || len(data) != 2 {
		t.Errorf("custom_data = %v", data)
	}
}

func TestComponentizeItemKinds(t *testing.T) {
	t.Parallel()
	str := func(s string) nbt.Tag { return nbt.String(s) }
	list := func(items ...nbt.Tag) nbt.List { return nbt.ListOf(items) }
	componentsOf := func(id string, tag nbt.Compound) nbt.Compound {
		entries := nbt.Compound{"id": str(id), "Count": nbt.Byte(1), "tag": tag}
		componentizeTag(entries, id)
		c, _ := entries["components"].(nbt.Compound)
		return c
	}

	// A shulker box item keeps its contents as a container and the rest as
	// block entity data.
	c := componentsOf("minecraft:shulker_box", nbt.Compound{"BlockEntityTag": nbt.Compound{
		"id": str("minecraft:shulker_box"), "Lock": str("key"), "LootTable": str("minecraft:x"), "LootTableSeed": nbt.Long(5), "CustomName": str("n"),
		"Items": list(nbt.Compound{"Slot": nbt.Byte(-1), "id": str("minecraft:dirt"), "Count": nbt.Byte(2)}),
	}})
	container, _ := c["minecraft:container"].(nbt.List)
	if len(container.Items) != 1 {
		t.Fatalf("container = %v", c["minecraft:container"])
	}
	slot := container.Items[0].(nbt.Compound)
	if slot["slot"] != nbt.Int(255) || slot["item"].(nbt.Compound)["id"] != str("minecraft:dirt") {
		t.Errorf("container slot = %v", slot)
	}
	if c["minecraft:lock"] != str("key") || c["minecraft:container_loot"].(nbt.Compound)["seed"] != nbt.Long(5) {
		t.Errorf("lock %v, loot %v", c["minecraft:lock"], c["minecraft:container_loot"])
	}
	if data := c["minecraft:block_entity_data"].(nbt.Compound); len(data) != 2 || data["id"] != str("minecraft:shulker_box") {
		t.Errorf("block_entity_data = %v", data)
	}

	c = componentsOf("minecraft:shulker_box", nbt.Compound{"BlockEntityTag": nbt.Compound{
		"id":    str("minecraft:shulker_box"),
		"Items": list(nbt.Compound{"Slot": nbt.Byte(0), "id": str("minecraft:dirt"), "Count": nbt.Byte(1)}),
	}})
	if _, ok := c["minecraft:block_entity_data"]; ok || c["minecraft:container"] == nil {
		t.Errorf("id-only remainder: components %v", c)
	}

	c = componentsOf("minecraft:potion", nbt.Compound{"Potion": str("minecraft:swiftness"), "CustomPotionColor": nbt.Int(9)})
	if p := c["minecraft:potion_contents"].(nbt.Compound); p["potion"] != str("minecraft:swiftness") || p["custom_color"] != nbt.Int(9) {
		t.Errorf("potion_contents = %v", p)
	}
	if c = componentsOf("minecraft:potion", nbt.Compound{"Potion": str("minecraft:empty")}); c != nil {
		t.Errorf("an empty potion left components %v", c)
	}

	c = componentsOf("minecraft:written_book", nbt.Compound{
		"title": str("T"), "author": str("A"), "generation": nbt.Int(1),
		"pages": list(str("p0"), str("p1")), "filtered_pages": nbt.Compound{"1": str("f1")},
	})
	book := c["minecraft:written_book_content"].(nbt.Compound)
	pages := book["pages"].(nbt.List).Items
	if book["title"].(nbt.Compound)["raw"] != str("T") || book["author"] != str("A") || len(pages) != 2 ||
		pages[1].(nbt.Compound)["filtered"] != str("f1") || pages[0].(nbt.Compound)["filtered"] != nil {
		t.Errorf("written_book_content = %v", book)
	}

	c = componentsOf("minecraft:firework_rocket", nbt.Compound{"Fireworks": nbt.Compound{
		"Flight": nbt.Byte(2), "Explosions": list(nbt.Compound{"Type": nbt.Byte(3), "Colors": nbt.IntArray{1}, "Trail": nbt.Byte(1)}),
	}})
	fw := c["minecraft:fireworks"].(nbt.Compound)
	explosion := fw["explosions"].(nbt.List).Items[0].(nbt.Compound)
	if fw["flight_duration"] != nbt.Byte(2) || explosion["shape"] != str("creeper") || explosion["has_trail"] != nbt.Byte(1) || explosion["colors"] == nil {
		t.Errorf("fireworks = %v", fw)
	}

	c = componentsOf("minecraft:filled_map", nbt.Compound{"map": nbt.Int(4), "display": nbt.Compound{"MapColor": nbt.Int(7)}, "Decorations": list(
		nbt.Compound{"id": str("d"), "type": nbt.Byte(1), "x": nbt.Double(1.5), "z": nbt.Double(2), "rot": nbt.Double(90)},
	)})
	deco := c["minecraft:map_decorations"].(nbt.Compound)["d"].(nbt.Compound)
	if c["minecraft:map_id"] != nbt.Int(4) || c["minecraft:map_color"] != nbt.Int(7) || deco["type"] != str("frame") || deco["rotation"] != nbt.Float(90) {
		t.Errorf("map components = %v", c)
	}

	c = componentsOf("minecraft:stone_pickaxe", nbt.Compound{"HideFlags": nbt.Int(8), "CanDestroy": list(str("minecraft:stone[facing=north,lit=true]{a:1}"))})
	pred := c["minecraft:can_break"].(nbt.Compound)
	first := pred["predicates"].(nbt.List).Items[0].(nbt.Compound)
	if pred["show_in_tooltip"] != nbt.Byte(0) || first["blocks"] != str("minecraft:stone") ||
		first["state"].(nbt.Compound)["lit"] != str("true") || first["nbt"] != str("{a:1}") {
		t.Errorf("can_break = %v", pred)
	}

	c = componentsOf("minecraft:player_head", nbt.Compound{"SkullOwner": str("Steve")})
	if c["minecraft:profile"].(nbt.Compound)["name"] != str("Steve") {
		t.Errorf("profile = %v", c["minecraft:profile"])
	}
}

func TestComponentizeLegacyTagKinds(t *testing.T) {
	t.Parallel()
	componentize := func(id string, tag nbt.Compound) nbt.Compound {
		entries := nbt.Compound{"id": nbt.String(id), "Count": nbt.Byte(1), "tag": tag}
		componentizeTag(entries, id)
		c, _ := entries["components"].(nbt.Compound)
		fixComponentShapes(c, id, 3700)
		return c
	}
	layer := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"Pattern": nbt.String("cbo"), "Color": nbt.Int(0)}}}
	wantLayer := func(pattern string) nbt.Tag {
		return nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"pattern": nbt.String(pattern), "color": nbt.String("white")}}}
	}

	shield := componentize("minecraft:shield", nbt.Compound{"BlockEntityTag": nbt.Compound{"Base": nbt.Int(1), "Patterns": layer}})
	if shield["minecraft:base_color"] != nbt.String("orange") || !nbtEqual(shield["minecraft:banner_patterns"], wantLayer("minecraft:curly_border")) || shield["minecraft:block_entity_data"] != nil {
		t.Errorf("shield = %v", shield)
	}
	flo := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"Pattern": nbt.String("flo"), "Color": nbt.Int(0)}}}
	banner := componentize("minecraft:red_banner", nbt.Compound{"BlockEntityTag": nbt.Compound{"Patterns": flo}})
	if !nbtEqual(banner["minecraft:banner_patterns"], wantLayer("minecraft:flower")) || banner["minecraft:block_entity_data"] != nil {
		t.Errorf("banner = %v", banner)
	}

	pick := componentize("minecraft:diamond_pickaxe", nbt.Compound{"AttributeModifiers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"AttributeName": nbt.String("generic.attack_damage"), "Name": nbt.String("n"), "Amount": nbt.Double(1), "Operation": nbt.Int(0), "UUID": nbt.IntArray{1, 2, 3, 4}},
	}}})
	mods := pick["minecraft:attribute_modifiers"].(nbt.List)
	if m := mods.Items[0].(nbt.Compound); m["type"] != nbt.String("minecraft:attack_damage") {
		t.Errorf("modifier = %v", m)
	}

	compass := componentize("minecraft:compass", nbt.Compound{
		"LodestonePos": nbt.Compound{"X": nbt.Int(1), "Y": nbt.Int(2), "Z": nbt.Int(3)}, "LodestoneDimension": nbt.String("minecraft:overworld"),
	})
	target := compass["minecraft:lodestone_tracker"].(nbt.Compound)["target"].(nbt.Compound)
	if !nbtEqual(target["pos"], nbt.IntArray{1, 2, 3}) {
		t.Errorf("lodestone target = %v", target)
	}

	egg := componentize("minecraft:zombie_spawn_egg", nbt.Compound{"EntityTag": nbt.Compound{"CustomName": nbt.String(`"Z"`)}})
	if data := egg["minecraft:entity_data"].(nbt.Compound); data["CustomName"] != nbt.String("Z") {
		t.Errorf("entity_data = %v", data)
	}

	fish := componentize("minecraft:tropical_fish_bucket", nbt.Compound{"BucketVariantTag": nbt.Int(65536)})
	if fish["minecraft:tropical_fish/pattern"] != nbt.String("kob") || fish["minecraft:tropical_fish/base_color"] != nbt.String("orange") ||
		fish["minecraft:tropical_fish/pattern_color"] != nbt.String("white") {
		t.Errorf("tropical fish = %v", fish)
	}
	if data := fish["minecraft:bucket_entity_data"].(nbt.Compound); data["BucketVariantTag"] != nil {
		t.Errorf("bucket_entity_data = %v", data)
	}

	book := componentize("minecraft:written_book", nbt.Compound{
		"title": nbt.String("t"), "author": nbt.String("a"),
		"pages": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String(`{"text":"hello","bold":true}`), nbt.String(`"page two"`)}},
	})
	pages := book["minecraft:written_book_content"].(nbt.Compound)["pages"].(nbt.List)
	first := pages.Items[0].(nbt.Compound)["raw"].(nbt.Compound)
	if first["text"] != nbt.String("hello") || first["bold"] != nbt.Byte(1) || pages.Items[1].(nbt.Compound)["raw"] != nbt.String("page two") {
		t.Errorf("pages = %v", pages)
	}
}

func TestFixComponentShapesAfter1_20_5(t *testing.T) {
	t.Parallel()
	c := nbt.Compound{"minecraft:attribute_modifiers": nbt.Compound{"modifiers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"type": nbt.String("minecraft:generic.attack_damage"), "uuid": nbt.IntArray{1, 1, 1, 1}},
		nbt.Compound{"type": nbt.String("minecraft:other"), "uuid": nbt.IntArray{2, 2, 2, 2}},
	}}}}
	fixComponentShapes(c, "minecraft:diamond_sword", 3900)
	mods := c["minecraft:attribute_modifiers"].(nbt.List)
	if mods.Items[0].(nbt.Compound)["type"] != nbt.String("minecraft:attack_damage") || mods.Items[1].(nbt.Compound)["type"] != nbt.String("minecraft:other") {
		t.Errorf("modifiers = %v", mods)
	}

	book := nbt.Compound{"minecraft:written_book_content": nbt.Compound{"pages": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"raw": nbt.String(`"hi"`)},
	}}}}
	fixComponentShapes(book, "minecraft:written_book", 4189)
	pages := book["minecraft:written_book_content"].(nbt.Compound)["pages"].(nbt.List)
	if pages.Items[0].(nbt.Compound)["raw"] != nbt.String("hi") {
		t.Errorf("pages = %v", pages)
	}
}

func nbtEqual(a, b nbt.Tag) bool { return reflect.DeepEqual(a, b) }
