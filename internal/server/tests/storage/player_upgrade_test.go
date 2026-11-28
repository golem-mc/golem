package storage

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func legacyStack(id string, count int8, slot int8, tag nbt.Compound) nbt.Compound {
	c := nbt.Compound{"id": nbt.String(id), "Count": nbt.Byte(count), "Slot": nbt.Byte(slot)}
	if tag != nil {
		c["tag"] = tag
	}
	return c
}

func list(items ...nbt.Tag) nbt.List { return nbt.ListOf(items) }

// savedStack finds the stack of item id in a saved list.
func savedStack(t *testing.T, root nbt.Compound, key, id string) nbt.Compound {
	t.Helper()
	items, _ := nbt.Get[nbt.List](root, key)
	for _, it := range items.Items {
		if c, ok := nbt.CompoundView(it); ok {
			if got, _ := nbt.Get[nbt.String](c, "id"); string(got) == id {
				return c
			}
		}
	}
	t.Fatalf("%s holds no %s: %v", key, id, items)
	return nil
}

func joinOldPlayer(t *testing.T, root nbt.Compound) nbt.Compound {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Tester"), root)
	srv, c, _ := servertest.JoinAs(t, cfg, "Tester")
	c.Disconnect()
	srv.Shutdown()
	return servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Tester"))
}

// TestPlayerFileBefore1_20_5Upgrades loads a 1.20.4 player file and finds its
// stacks and attributes upgraded.
// vanilla: DataFixTypes.PLAYER.updateToCurrentVersion
func TestPlayerFileBefore1_20_5Upgrades(t *testing.T) {
	t.Parallel()
	saved := joinOldPlayer(t, nbt.Compound{
		"DataVersion": nbt.Int(3700),
		"Health":      nbt.Float(25),
		"Attributes": list(nbt.Compound{
			"Name": nbt.String("minecraft:generic.max_health"), "Base": nbt.Double(30)}),
		"Inventory": list(
			legacyStack("minecraft:written_book", 1, 0, nbt.Compound{
				"title": nbt.String("T"), "author": nbt.String("A"),
				"pages": list(nbt.String(`{"text":"hello","bold":true}`))}),
			legacyStack("minecraft:iron_sword", 1, 1, nbt.Compound{
				"display": nbt.Compound{"Name": nbt.String(`{"text":"Blade","color":"red"}`)},
				"Enchantments": list(nbt.Compound{
					"id": nbt.String("minecraft:sharpness"), "lvl": nbt.Short(3)})}),
			legacyStack("minecraft:diamond", 7, 2, nil),
			legacyStack("minecraft:iron_chestplate", 1, 102, nil),
		),
		"EnderItems": list(legacyStack("minecraft:emerald", 1, 0, nil)),
	})
	inv := func(id string) nbt.Compound { return savedStack(t, saved, "Inventory", id) }
	if n, _ := nbt.Get[nbt.Int](inv("minecraft:diamond"), "count"); n != 7 {
		t.Errorf("diamonds = %d, want 7", n)
	}
	comps, _ := nbt.Get[nbt.Compound](inv("minecraft:iron_sword"), "components")
	if _, ok := comps["minecraft:custom_name"]; !ok {
		t.Errorf("sword components %v lack custom_name", comps)
	}
	if _, ok := comps["minecraft:enchantments"]; !ok {
		t.Errorf("sword components %v lack enchantments", comps)
	}
	book, _ := nbt.Get[nbt.Compound](inv("minecraft:written_book"), "components")
	if _, ok := book["minecraft:written_book_content"]; !ok {
		t.Errorf("book components %v lack written_book_content", book)
	}
	// The file is stamped current, so the worn item is saved in the equipment tag.
	equipment, _ := nbt.Get[nbt.Compound](saved, "equipment")
	if chest, _ := nbt.Get[nbt.Compound](equipment, "chest"); chest["id"] != nbt.String("minecraft:iron_chestplate") {
		t.Errorf("equipment = %v, want an iron chestplate in chest", equipment)
	}
	savedStack(t, saved, "EnderItems", "minecraft:emerald")
	attrs, _ := nbt.Get[nbt.List](saved, "attributes")
	found := false
	for _, a := range attrs.Items {
		c, _ := nbt.CompoundView(a)
		if id, _ := nbt.Get[nbt.String](c, "id"); id == "minecraft:max_health" {
			base, _ := nbt.Get[nbt.Double](c, "base")
			found = base == 30
		}
	}
	if !found {
		t.Errorf("attributes %v lack max_health 30", attrs)
	}
}

// TestPlayerFileFrom1_21_1Upgrades loads a 1.21.1 player file and finds its
// components in the current shapes.
func TestPlayerFileFrom1_21_1Upgrades(t *testing.T) {
	t.Parallel()
	saved := joinOldPlayer(t, nbt.Compound{
		"DataVersion": nbt.Int(3955),
		"attributes": list(nbt.Compound{
			"id": nbt.String("minecraft:generic.max_health"), "base": nbt.Double(30)}),
		"Inventory": list(nbt.Compound{
			"id": nbt.String("minecraft:iron_sword"), "count": nbt.Int(1), "Slot": nbt.Byte(1),
			"components": nbt.Compound{
				"minecraft:custom_name": nbt.String(`{"text":"Blade","color":"red"}`),
				"minecraft:attribute_modifiers": nbt.Compound{"modifiers": list(nbt.Compound{
					"type": nbt.String("minecraft:generic.attack_damage"),
					"id":   nbt.String("minecraft:x"), "amount": nbt.Double(5),
					"operation": nbt.String("add_value"), "slot": nbt.String("mainhand")})}}}),
	})
	sword := savedStack(t, saved, "Inventory", "minecraft:iron_sword")
	comps, _ := nbt.Get[nbt.Compound](sword, "components")
	name, ok := nbt.CompoundView(comps["minecraft:custom_name"])
	if !ok {
		t.Errorf("custom_name = %v, want a text compound", comps["minecraft:custom_name"])
	} else if text, _ := nbt.Get[nbt.String](name, "text"); text != "Blade" {
		t.Errorf("custom_name text = %q", text)
	}
	mods, _ := nbt.Get[nbt.List](comps, "minecraft:attribute_modifiers")
	if len(mods.Items) != 1 {
		t.Fatalf("attribute_modifiers = %v, want a list of one", comps["minecraft:attribute_modifiers"])
	}
	m, _ := nbt.CompoundView(mods.Items[0])
	if typ, _ := nbt.Get[nbt.String](m, "type"); typ != "minecraft:attack_damage" {
		t.Errorf("modifier type = %q", typ)
	}
}

// TestPlayerFileFrom26_2KeepsExplorerMap loads a 26.2 player file and finds
// the explorer map as its own item.
// vanilla: ExplorerMapItemFix, ItemRenameFix
func TestPlayerFileFrom26_2KeepsExplorerMap(t *testing.T) {
	t.Parallel()
	saved := joinOldPlayer(t, nbt.Compound{
		"DataVersion": nbt.Int(4903),
		"Inventory": list(nbt.Compound{
			"id": nbt.String("minecraft:filled_map"), "count": nbt.Int(1), "Slot": nbt.Byte(1),
			"components": nbt.Compound{
				"minecraft:map_id":    nbt.Int(1),
				"minecraft:map_color": nbt.Int(3830373),
				"minecraft:map_decorations": nbt.Compound{"+": nbt.Compound{
					"type": nbt.String("minecraft:mansion"), "x": nbt.Double(0), "z": nbt.Double(0), "rotation": nbt.Float(180)}},
				"minecraft:item_name": nbt.Compound{"translate": nbt.String("filled_map.mansion")},
			}}),
	})
	savedStack(t, saved, "Inventory", "minecraft:woodland_mansion_map")
}

// TestPlayerFileBefore1_16KeepsItsDimension loads a 1.15.2 player file whose
// Dimension is a number and finds the player saved in that dimension.
// vanilla: LegacyDimensionIdFix.fixPlayer
func TestPlayerFileBefore1_16KeepsItsDimension(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		dim  nbt.Tag
		want string
	}{
		{nbt.Int(-1), "minecraft:the_nether"},
		{nbt.Int(1), "minecraft:the_end"},
		{nbt.Byte(0), "minecraft:overworld"},
	} {
		saved := joinOldPlayer(t, nbt.Compound{
			"DataVersion": nbt.Int(2230),
			"Dimension":   tc.dim,
			"Pos":         nbt.ListOf([]nbt.Tag{nbt.Double(8), nbt.Double(70), nbt.Double(8)}),
		})
		if got, _ := nbt.Get[nbt.String](saved, "Dimension"); string(got) != tc.want {
			t.Errorf("Dimension %v saved as %q, want %q", tc.dim, got, tc.want)
		}
	}
}

// TestPlayerFileTextEventsAreRenamed loads a 1.20.4 file whose book page and
// item name carry click and hover events.
// vanilla: TextComponentHoverAndClickEventFix
func TestPlayerFileTextEventsAreRenamed(t *testing.T) {
	t.Parallel()
	saved := joinOldPlayer(t, nbt.Compound{
		"DataVersion": nbt.Int(3700),
		"Inventory": list(
			legacyStack("minecraft:written_book", 1, 0, nbt.Compound{
				"title": nbt.String("T"), "author": nbt.String("A"),
				"pages": list(nbt.String(`{"text":"x","clickEvent":{"action":"run_command","value":"/say hi"},"hoverEvent":{"action":"show_text","contents":"h"}}`))}),
			legacyStack("minecraft:iron_sword", 1, 1, nbt.Compound{
				"display": nbt.Compound{"Name": nbt.String(`{"text":"S","hoverEvent":{"action":"show_text","contents":"t"}}`)}}),
		),
	})
	book, _ := nbt.Get[nbt.Compound](savedStack(t, saved, "Inventory", "minecraft:written_book"), "components")
	content, _ := nbt.Get[nbt.Compound](book, "minecraft:written_book_content")
	pages, _ := nbt.Get[nbt.List](content, "pages")
	page, _ := nbt.CompoundView(pages.Items[0])
	raw, _ := nbt.CompoundView(page["raw"])
	click, _ := nbt.Get[nbt.Compound](raw, "click_event")
	if action, _ := nbt.Get[nbt.String](click, "action"); action != "run_command" {
		t.Errorf("page raw = %v, want a click_event", raw)
	}
	if cmd, _ := nbt.Get[nbt.String](click, "command"); cmd != "/say hi" {
		t.Errorf("click_event = %v, want command /say hi", click)
	}
	hover, _ := nbt.Get[nbt.Compound](raw, "hover_event")
	if v, _ := nbt.Get[nbt.String](hover, "value"); v != "h" {
		t.Errorf("hover_event = %v, want value h", hover)
	}
	sword, _ := nbt.Get[nbt.Compound](savedStack(t, saved, "Inventory", "minecraft:iron_sword"), "components")
	name, _ := nbt.Get[nbt.Compound](sword, "minecraft:custom_name")
	hover, _ = nbt.Get[nbt.Compound](name, "hover_event")
	if v, _ := nbt.Get[nbt.String](hover, "value"); v != "t" {
		t.Errorf("custom_name = %v, want hover_event value t", name)
	}
}
