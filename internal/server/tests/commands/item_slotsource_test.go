package commands

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestItemSlotSources checks /item replace, fill, override and modify with
// inline slot sources (filtered, group, contents, limit_slots) and data-pack
// slot source ids, bundle weight on non-bundle stacks, explicit empty
// component preservation, and unnamed no-such-slot refusals.
// vanilla: ItemCommands, SlotCollection, SlotSourceArgument
func TestItemSlotSources(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil); registry.ApplyPackSlotSources(nil) })
	cfg := packWorld(t)
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pack")
	writeFile(t, filepath.Join(data, "slot_source", "slot_five.json"),
		`{"type":"minecraft:slot_range","slots":"container.5"}`)
	srv, c := joinPacks(t, cfg)
	servertest.HoldTicks(t, srv)

	run := func(cmd string) {
		t.Helper()
		c.ChatCommand(cmd)
		c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
	}
	expectFeedback := func(cmd, key string, with ...protocol.Text) {
		t.Helper()
		c.ChatCommand(cmd)
		want := servertest.SystemChatWire(protocol.Text{Translate: key, With: with})
		if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, want) {
			t.Errorf("%s: feedback = % X, want % X", cmd, got, want)
		}
	}
	expectRefusal := func(cmd, key string, with ...string) {
		t.Helper()
		c.ChatCommand(cmd)
		want := servertest.CommandErrorMessageForTest(key, with...)
		if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, want) {
			t.Errorf("%s: refusal = % X, want % X", cmd, got, want)
		}
	}

	run("setblock 2 64 3 minecraft:chest")
	run("item fill block 2 64 3 container.* with stone 3")
	run("item replace block 2 64 3 container.4 with diamond 3")

	// 1. Filtered modify: only the diamond stack in slot 4 becomes count 9.
	expectFeedback(
		`item modify block 2 64 3 {type:"filtered",slot_source:{type:"slot_range",slots:"container.*"},item_filter:{items:"minecraft:diamond"}} {type:"set_count",count:9}`,
		itemBlockModifyKey,
		servertest.ItemNumber(1), servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3))
	if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:4b}]", "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:diamond"`) || !strings.Contains(got, "count: 9") {
		t.Errorf("slot 4 after filtered modify = %q, want 9 diamonds", got)
	}

	// 2. Group replace: replace has one item, so only container.1 changes.
	expectFeedback(
		`item replace block 2 64 3 [{type:"slot_range",slots:"container.1"},{type:"slot_range",slots:"container.3"}] with gold_ingot 2`,
		itemBlockSetKey,
		servertest.ItemNumber(1), servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3),
		servertest.ItemFeedbackText("minecraft:gold_ingot", servertest.ItemDisplayKey(t, "minecraft:gold_ingot"), 2))
	if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:1b}]", "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:gold_ingot"`) || !strings.Contains(got, "count: 2") {
		t.Errorf("slot 1 after group replace = %q, want 2 gold_ingot", got)
	}
	if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:3b}]", "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:stone"`) || !strings.Contains(got, "count: 3") {
		t.Errorf("slot 3 after group replace = %q, want unchanged 3 stone", got)
	}

	// 3. Contents fill on a shulker box in slot 0: fills all 256 container slots.
	run("item replace block 2 64 3 container.0 with minecraft:shulker_box 1")
	expectFeedback(
		`item fill block 2 64 3 {type:"contents",component:"minecraft:container",slot_source:{type:"slot_range",slots:"container.0"}} with stone 2`,
		itemBlockSetKey,
		servertest.ItemNumber(256), servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3),
		servertest.ItemFeedbackText("minecraft:stone", servertest.ItemDisplayKey(t, "minecraft:stone"), 2))
	if got := plainQuery(t, srv, `data get block 2 64 3 Items[{Slot:0b}].components."minecraft:container"`, "commands.data.block.query"); !strings.Contains(got, "slot: 0") || !strings.Contains(got, "slot: 255") || !strings.Contains(got, `id: "minecraft:stone"`) {
		t.Errorf("shulker box container after contents fill = %q, want 256 stone slots", got)
	}

	// 4. Limit slots modify: modifies the first 2 slots (slot 0 and slot 1).
	expectFeedback(
		`item modify block 2 64 3 {type:"limit_slots",slot_source:{type:"slot_range",slots:"container.*"},limit:2} {type:"set_count",count:7}`,
		itemBlockModifyKey,
		servertest.ItemNumber(2), servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3))
	if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:1b}]", "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:gold_ingot"`) || !strings.Contains(got, "count: 7") {
		t.Errorf("slot 1 after limit_slots modify = %q, want 7 gold_ingot", got)
	}
	if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:2b}]", "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:stone"`) || !strings.Contains(got, "count: 3") {
		t.Errorf("slot 2 after limit_slots modify = %q, want unchanged 3 stone", got)
	}

	// 5. Data-pack slot source id in /item replace ... with, and trailing input refusal.
	expectFeedback(
		"item replace block 2 64 3 pack:slot_five with emerald 4",
		itemBlockSetKey,
		servertest.ItemNumber(1), servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3),
		servertest.ItemFeedbackText("minecraft:emerald", servertest.ItemDisplayKey(t, "minecraft:emerald"), 4))
	if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:5b}]", "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:emerald"`) || !strings.Contains(got, "count: 4") {
		t.Errorf("slot 5 after data-pack replace = %q, want 4 emerald", got)
	}
	const trailingCmd = "item replace block 2 64 3 pack:slot_five* with stone"
	c.ChatCommand(trailingCmd)
	wantSep := servertest.CommandErrorMessageForTest("command.expected.separator")
	if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, wantSep) {
		t.Errorf("%s: error = % X, want % X", trailingCmd, got, wantSep)
	}
	wantCtx := servertest.CommandErrorContextForTest(trailingCmd, strings.Index(trailingCmd, "pack:slot_five")+len("pack:slot_five"))
	if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, wantCtx) {
		t.Errorf("%s: context = % X, want % X", trailingCmd, got, wantCtx)
	}

	// 6. Inline slot source in from, and unnamed refusals when nothing is selected.
	expectFeedback(
		`item replace block 2 64 3 container.6 from block 2 64 3 {type:"filtered",slot_source:{type:"slot_range",slots:"container.*"},item_filter:{items:"minecraft:emerald"}}`,
		itemBlockReplaceKey,
		servertest.ItemNumber(1), servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3))
	if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:6b}]", "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:emerald"`) || !strings.Contains(got, "count: 4") {
		t.Errorf("slot 6 after filtered from replace = %q, want 4 emerald", got)
	}

	expectRefusal(
		`item modify block 2 64 3 {type:"filtered",slot_source:{type:"slot_range",slots:"container.*"},item_filter:{items:"minecraft:netherite_ingot"}} {type:"set_count",count:1}`,
		"commands.item.target.no_such_slot.unnamed")

	expectRefusal(
		`item replace block 2 64 3 container.7 from block 2 64 3 {type:"filtered",slot_source:{type:"slot_range",slots:"container.*"},item_filter:{items:"minecraft:netherite_ingot"}}`,
		"commands.item.source.no_such_slot.unnamed")

	// 7. Bundle weight follows the effective bundle_contents component on any item:
	// a stone carrying 1 coal weighs 1/16 + 1/64 = 5/64, so 12 fit in a bundle.
	run("item replace block 2 64 3 container.8 with stone 1")
	run(`item replace block 2 64 3 {type:"contents",component:"minecraft:bundle_contents",slot_source:{type:"slot_range",slots:"container.8"}} with coal 1`)
	run("item replace block 2 64 3 container.9 with bundle 1")
	expectFeedback(
		`item fill block 2 64 3 {type:"contents",component:"minecraft:bundle_contents",slot_source:{type:"slot_range",slots:"container.9"}} from block 2 64 3 container.8`,
		itemBlockReplaceKey,
		servertest.ItemNumber(12), servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3))
	if got := plainQuery(t, srv, `data get block 2 64 3 Items[{Slot:9b}].components."minecraft:bundle_contents"`, "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:stone"`) || !strings.Contains(got, "count: 12") {
		t.Errorf("bundle contents after filling from stone with coal = %q, want 12 stone", got)
	}

	// 8. Clearing a contents component keeps an explicit empty patch on items
	// lacking a default for it, and restores the default (clearing any removal)
	// on items that have one, for both override and modify.
	for _, tc := range []struct {
		component   string
		defaultItem string
		stripped    string
	}{
		{"minecraft:charged_projectiles", "crossbow", "crossbow[!charged_projectiles]"},
		{"minecraft:bundle_contents", "bundle", "bundle[!bundle_contents]"},
		{"minecraft:container", "shulker_box", "shulker_box[!container]"},
	} {
		// Override to empty on stone keeps "<component>": [].
		run("item replace block 2 64 3 container.10 with stone 1")
		run(fmt.Sprintf(`item replace block 2 64 3 {type:"contents",component:%q,slot_source:{type:"slot_range",slots:"container.10"}} with arrow 1`, tc.component))
		run(fmt.Sprintf(`item override block 2 64 3 {type:"contents",component:%q,slot_source:{type:"slot_range",slots:"container.10"}} with air 1`, tc.component))
		wantEmpty := fmt.Sprintf(`%q: []`, tc.component)
		if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:10b}].components", "commands.data.block.query"); !strings.Contains(got, wantEmpty) {
			t.Errorf("%s override on stone components = %q, want %s", tc.component, got, wantEmpty)
		}

		// Modify to empty on stone keeps "<component>": [].
		run("item replace block 2 64 3 container.10 with stone 1")
		run(fmt.Sprintf(`item replace block 2 64 3 {type:"contents",component:%q,slot_source:{type:"slot_range",slots:"container.10"}} with arrow 1`, tc.component))
		run(fmt.Sprintf(`item modify block 2 64 3 {type:"contents",component:%q,slot_source:{type:"slot_range",slots:"container.10"}} {type:"set_count",count:0}`, tc.component))
		if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:10b}].components", "commands.data.block.query"); !strings.Contains(got, wantEmpty) {
			t.Errorf("%s modify on stone components = %q, want %s", tc.component, got, wantEmpty)
		}

		// Clearing on an item with a default drops the empty patch and any removal.
		run("item replace block 2 64 3 container.11 with " + tc.stripped + " 1")
		run(fmt.Sprintf(`item replace block 2 64 3 {type:"contents",component:%q,slot_source:{type:"slot_range",slots:"container.11"}} with arrow 1`, tc.component))
		run(fmt.Sprintf(`item override block 2 64 3 {type:"contents",component:%q,slot_source:{type:"slot_range",slots:"container.11"}} with air 1`, tc.component))
		if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:11b}]", "commands.data.block.query"); strings.Contains(got, "components") {
			t.Errorf("%s override on %s = %q, want default item with no components patch", tc.component, tc.stripped, got)
		}

		run("item replace block 2 64 3 container.11 with " + tc.stripped + " 1")
		run(fmt.Sprintf(`item replace block 2 64 3 {type:"contents",component:%q,slot_source:{type:"slot_range",slots:"container.11"}} with arrow 1`, tc.component))
		run(fmt.Sprintf(`item modify block 2 64 3 {type:"contents",component:%q,slot_source:{type:"slot_range",slots:"container.11"}} {type:"set_count",count:0}`, tc.component))
		if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:11b}]", "commands.data.block.query"); strings.Contains(got, "components") {
			t.Errorf("%s modify on %s = %q, want default item with no components patch", tc.component, tc.stripped, got)
		}
	}

	// 9. Contents visits every parent stack even when nothing is replaced in it,
	// writing an explicit empty component patch on items without a default.
	run("setblock 2 64 4 minecraft:chest")
	run("item replace block 2 64 4 container.0 with stone 1")
	run("item replace block 2 64 4 container.1 with stone 1")
	run("item replace block 2 64 4 container.2 with stone 1")
	expectFeedback(
		`item replace block 2 64 4 {type:"contents",component:"minecraft:charged_projectiles",slot_source:{type:"slot_range",slots:"container.*"}} with arrow 1`,
		itemBlockSetKey,
		servertest.ItemNumber(1), servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(4),
		servertest.ItemFeedbackText("minecraft:arrow", servertest.ItemDisplayKey(t, "minecraft:arrow"), 1))
	if got := plainQuery(t, srv, `data get block 2 64 4 Items[{Slot:0b}].components."minecraft:charged_projectiles"`, "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:arrow"`) {
		t.Errorf("slot 0 charged_projectiles = %q, want arrow", got)
	}
	for _, slot := range []int{1, 2} {
		if got := plainQuery(t, srv, fmt.Sprintf("data get block 2 64 4 Items[{Slot:%db}].components", slot), "commands.data.block.query"); !strings.Contains(got, `"minecraft:charged_projectiles": []`) {
			t.Errorf("slot %d components after unreached contents replace = %q, want empty charged_projectiles patch", slot, got)
		}
	}

	// 10. A bundle the visit sets nothing in keeps its entries unmerged.
	run("setblock 2 64 5 minecraft:chest")
	run("item replace block 2 64 5 container.0 with bundle")
	run("item replace block 2 64 5 container.1 with bundle[bundle_contents=[{id:\"minecraft:stone\"},{id:\"minecraft:stone\"}]]")
	expectFeedback(
		`item replace block 2 64 5 {type:"contents",component:"minecraft:bundle_contents",slot_source:{type:"slot_range",slots:"container.*"}} with arrow`,
		itemBlockSetKey,
		servertest.ItemNumber(1), servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(5),
		servertest.ItemFeedbackText("minecraft:arrow", servertest.ItemDisplayKey(t, "minecraft:arrow"), 1))
	if got := plainQuery(t, srv, `data get block 2 64 5 Items[{Slot:1b}].components."minecraft:bundle_contents"`, "commands.data.block.query"); strings.Count(got, `minecraft:stone`) != 2 || strings.Contains(got, "count: 2") {
		t.Errorf("untouched bundle contents = %q, want two unmerged stone entries", got)
	}

	// 11. Component writes leave a bookshelf's last-interacted slot alone.
	run("setblock 2 64 6 minecraft:chiseled_bookshelf")
	run("item replace block 2 64 6 container.2 with book")
	run("item replace block 2 64 6 container.0 with book")
	run(`item replace block 2 64 6 {type:"contents",component:"minecraft:charged_projectiles",slot_source:{type:"slot_range",slots:"container.*"}} with arrow 1`)
	if got := plainQuery(t, srv, "data get block 2 64 6 last_interacted_slot", "commands.data.block.query"); !strings.Contains(got, "0") || strings.Contains(got, "2") {
		t.Errorf("last_interacted_slot after component write = %q, want 0", got)
	}
	if got := plainQuery(t, srv, "data get block 2 64 6 Items[{Slot:2b}].components", "commands.data.block.query"); !strings.Contains(got, `"minecraft:charged_projectiles": []`) {
		t.Errorf("shelf slot 2 components = %q, want empty charged_projectiles patch", got)
	}

	// 12. A component write on a furnace's input keeps its cooking progress.
	run("setblock 2 64 7 minecraft:furnace")
	run("item replace block 2 64 7 container.0 with iron_ore")
	run("data merge block 2 64 7 {cooking_time_spent:80}")
	run(`item replace block 2 64 7 {type:"contents",component:"minecraft:charged_projectiles",slot_source:{type:"slot_range",slots:"container.0"}} with arrow`)
	if got := plainQuery(t, srv, "data get block 2 64 7 cooking_time_spent", "commands.data.block.query"); !strings.Contains(got, "80") {
		t.Errorf("cooking_time_spent after component write = %q, want 80", got)
	}
	if got := plainQuery(t, srv, `data get block 2 64 7 Items[{Slot:0b}].components."minecraft:charged_projectiles"`, "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:arrow"`) {
		t.Errorf("furnace input charged_projectiles = %q, want arrow", got)
	}

	// 13. A modify_contents loot modifier that empties a shulker box restores its default.
	run(`item replace block 2 64 3 container.15 with shulker_box[container=[{slot:0,item:{id:"minecraft:stone"}}]]`)
	run(`item modify block 2 64 3 container.15 {type:"modify_contents",component:"minecraft:container",modifier:{type:"set_count",count:0}}`)
	if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:15b}]", "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:shulker_box"`) || strings.Contains(got, "components") {
		t.Errorf("emptied shulker box = %q, want default shulker box with no components patch", got)
	}

	// 14. A component write inside a bundle entry does not merge the other entries.
	run(`item replace block 2 64 3 container.16 with bundle[bundle_contents=[{id:"minecraft:stone"},{id:"minecraft:stone"},{id:"minecraft:stone"}]]`)
	run(`item replace block 2 64 3 {type:"contents",component:"minecraft:charged_projectiles",slot_source:{type:"contents",component:"minecraft:bundle_contents",slot_source:{type:"slot_range",slots:"container.16"}}} with arrow`)
	if got := plainQuery(t, srv, `data get block 2 64 3 Items[{Slot:16b}].components."minecraft:bundle_contents"`, "commands.data.block.query"); strings.Count(got, `minecraft:stone`) != 3 || strings.Contains(got, "count: 2") || strings.Count(got, `"minecraft:charged_projectiles": []`) != 2 {
		t.Errorf("bundle entries after nested component write = %q, want three unmerged stones, two with empty projectiles", got)
	}

	c.Disconnect()
}
