package commands

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// oneItemTable is a one-pool table giving item with the functions, a JSON
// list, applied.
func oneItemTable(item, functions string) string {
	return `{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"` + item +
		`","functions":` + functions + `}]}]}`
}

// TestPackLootComponentFunctionsRoll checks that a data pack's loot tables
// using each function that writes a data component load and roll: banner
// patterns, a book's cover and pages, custom model data, firework stars and
// rockets, a container's loot table, a player head's profile (the runner's
// own, and the killer's as attacking_player of /loot kill) and a
// container's contents. The pack layer is process-wide, so this is not
// parallel with the other tests that load loot.
// vanilla: SetBannerPatternFunction, SetBookCoverFunction,
// SetCustomModelDataFunction, SetFireworkExplosionFunction,
// SetFireworksFunction, SetContainerLootTable, SetWritableBookPagesFunction,
// SetWrittenBookPagesFunction, FillPlayerHead, ModifyContainerContents
func TestPackLootComponentFunctionsRoll(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	tables := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk", "loot_table")
	for name, body := range map[string]string{
		"chest": oneItemTable("minecraft:chest",
			`[{"function":"minecraft:set_loot_table","loot_table_id":"minecraft:chests/simple_dungeon","seed":42}]`),
		"banner": oneItemTable("minecraft:white_banner",
			`[{"function":"minecraft:set_banner_pattern","append":false,"patterns":[{"pattern":"minecraft:stripe_top","color":"red"}]},`+
				`{"function":"minecraft:set_banner_pattern","append":true,"patterns":[{"pattern":"minecraft:border","color":"blue"}]}]`),
		"cover": oneItemTable("minecraft:written_book",
			`[{"function":"minecraft:set_book_cover","title":"Tale","author":"Anon","generation":2}]`),
		"written": oneItemTable("minecraft:written_book",
			`[{"function":"minecraft:set_written_book_pages","pages":["one",{"text":"two"}],"mode":"append"}]`),
		"writable": oneItemTable("minecraft:writable_book",
			`[{"function":"minecraft:set_writable_book_pages","pages":["draft"],"mode":"replace_all"}]`),
		"model": oneItemTable("minecraft:stick",
			`[{"function":"minecraft:set_custom_model_data","floats":{"values":[1.5],"mode":"append"},`+
				`"flags":{"values":[true],"mode":"append"},"strings":{"values":["s"],"mode":"replace_all"},`+
				`"colors":{"values":[255,[0.0,1.0,0.0]],"mode":"append"}}]`),
		"star": oneItemTable("minecraft:firework_star",
			`[{"function":"minecraft:set_firework_explosion","shape":"star","colors":[16711680],"trail":true}]`),
		"rocket": oneItemTable("minecraft:firework_rocket",
			`[{"function":"minecraft:set_fireworks","flight_duration":3,"explosions":{"values":[{"shape":"creeper"}],"mode":"append"}}]`),
		"head": oneItemTable("minecraft:player_head", `[{"function":"minecraft:fill_player_head","entity":"this"}]`),
		"box": oneItemTable("minecraft:shulker_box",
			`[{"function":"minecraft:set_contents","component":"minecraft:container","entries":[{"type":"minecraft:item","name":"minecraft:stick"}]},`+
				`{"function":"minecraft:modify_contents","component":"minecraft:container","modifier":{"function":"minecraft:set_count","count":5}}]`),
	} {
		writeFile(t, filepath.Join(tables, name+".json"), body)
	}
	writeFile(t, filepath.Join(cfg.LevelName, "datapacks", "a", "data", "minecraft", "loot_table", "entities", "endermite.json"),
		oneItemTable("minecraft:player_head", `[{"function":"minecraft:fill_player_head","entity":"attacking_player"}]`))
	_, c := joinPacks(t, cfg)
	give := func(name string) protocol.ItemStack { return giveStack(t, c, "loot give @s loot pk:"+name) }

	// The chest lands in the selected hotbar slot; container_loot has no
	// network codec, so the saved form shows it.
	if got := give("chest"); got.ItemID != servertest.MustItem("minecraft:chest") {
		t.Fatalf("pk:chest gave item %d", got.ItemID)
	}
	c.ChatCommand(`data get entity @s SelectedItem.components."minecraft:container_loot"`)
	if got := servertest.FeedbackComponent(t, c); !strings.Contains(got.Plain(), `loot_table: "minecraft:chests/simple_dungeon"`) ||
		!strings.Contains(got.Plain(), "seed: 42L") {
		t.Errorf("set_loot_table's container_loot reads %q, want the dungeon table and seed 42", got.Plain())
	}

	layers, _ := give("banner").BannerPatternsOf()
	red, blue := int32(slices.Index(registry.DyeNames, "red")), int32(slices.Index(registry.DyeNames, "blue"))
	if len(layers) != 2 || layers[0].Dye != red || layers[1].Dye != blue {
		t.Errorf("set_banner_pattern gave layers %+v, want a red then an appended blue", layers)
	}

	if book, ok := give("cover").WrittenBookOf(); !ok || book.Title != "Tale" || book.Author != "Anon" || book.Generation != 2 {
		t.Errorf("set_book_cover gave %+v (%v), want Tale by Anon, generation 2", book, ok)
	}
	if book, ok := give("written").WrittenBookOf(); !ok || len(book.Pages) != 2 ||
		book.Pages[0].Raw.Plain() != "one" || book.Pages[1].Raw.Plain() != "two" || book.Resolved {
		t.Errorf("set_written_book_pages gave %+v (%v), want unresolved pages one and two", book, ok)
	}
	if book, ok := give("writable").WritableBookOf(); !ok || len(book.Pages) != 1 || book.Pages[0].Raw != "draft" {
		t.Errorf("set_writable_book_pages gave %+v (%v), want the page draft", book, ok)
	}

	model, ok := give("model").CustomModelDataOf()
	if !ok || !slices.Equal(model.Floats, []float32{1.5}) || !slices.Equal(model.Flags, []bool{true}) ||
		!slices.Equal(model.Strings, []string{"s"}) || !slices.Equal(model.Colors, []int32{255, -16711936}) {
		t.Errorf("set_custom_model_data gave %+v (%v), want floats [1.5], flags [true], strings [s], colours [255 0xFF00FF00]", model, ok)
	}

	if e, ok := give("star").FireworkExplosionOf(); !ok || e.Shape != 2 || !slices.Equal(e.Colors, []int32{16711680}) || !e.Trail || e.Twinkle {
		t.Errorf("set_firework_explosion gave %+v (%v), want a red trailing star", e, ok)
	}
	if flight, explosions, ok := give("rocket").FireworksComponentOf(); !ok || flight != 3 || len(explosions) != 1 || explosions[0].Shape != 3 {
		t.Errorf("set_fireworks gave flight %d, explosions %+v (%v), want flight 3 and one creeper", flight, explosions, ok)
	}

	if profile, ok := give("head").ProfileOf(); !ok || profile.Name != "Testificate" || !profile.HasUUID {
		t.Errorf("fill_player_head gave profile %+v (%v), want the runner's", profile, ok)
	}

	c.ChatCommand("summon minecraft:endermite 4.5 64 4.5 {NoAI:1b}")
	servertest.FeedbackComponent(t, c)
	if profile, ok := giveStack(t, c, "loot give @s kill @e[type=minecraft:endermite,limit=1]").ProfileOf(); !ok ||
		profile.Name != "Testificate" || !profile.HasUUID {
		t.Errorf("fill_player_head of attacking_player gave profile %+v (%v), want the killer's", profile, ok)
	}

	box := give("box")
	if len(box.Container) != 1 || box.Container[0].ItemID != servertest.MustItem("minecraft:stick") || box.Container[0].Count != 5 {
		t.Errorf("modify_contents left the box holding %+v, want five sticks", box.Container)
	}
}

// TestPackLootSetItemAndComponentsRoll checks that a loot table's set_item,
// set_components (beyond a trim), a set_components inside modify_contents and
// copy_custom_data change the rolled stack, and that a name from set_name or
// set_components rides every stack a count above the stack size splits into.
// Not parallel: the pack layer is process-wide.
// vanilla: SetItemFunction, SetComponentsFunction, ModifyContainerContents,
// CopyCustomDataFunction, LootTable.createStackSplitter
func TestPackLootSetItemAndComponentsRoll(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	tables := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk", "loot_table")
	for name, body := range map[string]string{
		"swap": oneItemTable("minecraft:stick", `[{"function":"minecraft:set_item","item":"minecraft:blaze_rod"}]`),
		"named": oneItemTable("minecraft:stick",
			`[{"function":"minecraft:set_components","components":{"minecraft:custom_name":"Wand","minecraft:unbreakable":{}}}]`),
		"box": oneItemTable("minecraft:shulker_box",
			`[{"function":"minecraft:set_contents","component":"minecraft:container","entries":[{"type":"minecraft:item","name":"minecraft:stick"}]},`+
				`{"function":"minecraft:modify_contents","component":"minecraft:container",`+
				`"modifier":{"function":"minecraft:set_components","components":{"minecraft:custom_name":"Twig"}}}]`),
		"named_many": oneItemTable("minecraft:stick",
			`[{"function":"minecraft:set_count","count":100},{"function":"minecraft:set_name","name":"Bundle"}]`),
		"components_many": oneItemTable("minecraft:stick",
			`[{"function":"minecraft:set_count","count":100},{"function":"minecraft:set_components","components":{"minecraft:custom_name":"Kindling"}}]`),
		"copied": oneItemTable("minecraft:stick",
			`[{"function":"minecraft:copy_custom_data","source":{"type":"minecraft:storage","source":"pk:src"},`+
				`"ops":[{"source":"x","target":"a.b","op":"replace"}]}]`),
	} {
		writeFile(t, filepath.Join(tables, name+".json"), body)
	}
	_, c := joinPacks(t, cfg)
	give := func(name string) protocol.ItemStack { return giveStack(t, c, "loot give @s loot pk:"+name) }

	if got := give("swap"); got.ItemID != servertest.MustItem("minecraft:blaze_rod") {
		t.Errorf("set_item gave item %d, want a blaze rod", got.ItemID)
	}
	if got := give("named"); got.CustomName == nil || got.CustomName.Plain() != "Wand" || !got.Unbreakable {
		t.Errorf("set_components gave name %v, unbreakable %v; want Wand and unbreakable", got.CustomName, got.Unbreakable)
	}
	box := give("box")
	if len(box.Container) != 1 || box.Container[0].CustomName == nil || box.Container[0].CustomName.Plain() != "Twig" {
		t.Errorf("modify_contents' set_components left the box holding %+v, want a stick named Twig", box.Container)
	}

	for table, name := range map[string]string{"named_many": "Bundle", "components_many": "Kindling"} {
		c.ChatCommand("loot give @s loot pk:" + table)
		updates, chats := servertest.AwaitSlotUpdates(t, c, 2)
		if len(chats) == 0 {
			c.ExpectPlay(protocol.IDPlaySystemChat)
		}
		for _, u := range updates {
			if got := u.Stack; got.CustomName == nil || got.CustomName.Plain() != name {
				t.Errorf("pk:%s gave slot %d x%d named %v, want every split named %s", table, u.Slot, got.Count, got.CustomName, name)
			}
		}
		if counts := []int32{updates[0].Stack.Count, updates[1].Stack.Count}; counts[0]+counts[1] != 100 {
			t.Errorf("pk:%s gave stacks of %v, want 100 sticks in all", table, counts)
		}
	}

	c.ChatCommand("data merge storage pk:src {x:5}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "pk:src")
	data := give("copied").CustomData()
	if inner, _ := data["a"].(nbt.Compound); inner["b"] != nbt.Int(5) {
		t.Errorf("copy_custom_data gave custom data %v, want a.b = 5 from storage", data)
	}
}

// A bold name is kept raw (protocol.RawTextOf), so this checks raw names still
// tell two stacks apart (ItemStack.isSameItemSameComponents).
func TestPackLootStyledNamesDoNotMerge(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	tables := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk", "loot_table")
	for _, name := range []string{"Gold", "Silver"} {
		writeFile(t, filepath.Join(tables, strings.ToLower(name)+".json"), oneItemTable("minecraft:stick",
			`[{"function":"minecraft:set_name","name":{"text":"`+name+`","bold":true}}]`))
	}
	_, c := joinPacks(t, cfg)
	giveStack(t, c, "loot give @s loot pk:gold")

	c.ChatCommand("loot give @s loot pk:silver")
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	if u := updates[0]; u.Slot != 37 || u.Stack.Count != 1 {
		t.Errorf("Silver landed in slot %d x%d, want slot 37 x1 apart from Gold", u.Slot, u.Stack.Count)
	}
}
