package commands

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestItemSlotSources checks /item replace, fill, override and modify with
// inline slot sources (filtered, group, contents, limit_slots) and data-pack
// slot source ids, plus the unnamed no-such-slot refusals.
// vanilla: ItemCommands, SlotCollection, SlotSourceArgument
func TestItemSlotSources(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil); registry.ApplyPackSlotSources(nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pack")
	writeFile(t, filepath.Join(data, "slot_source", "slot_five.json"),
		`{"type":"minecraft:slot_range","slots":"container.5"}`)
	srv, c := joinPacks(t, cfg)

	c.ChatCommand("setblock 2 64 3 minecraft:chest")
	c.ChatCommand("item fill block 2 64 3 container.* with stone 3")
	c.ChatCommand("item replace block 2 64 3 container.4 with diamond 3")
	c.Settle()

	// 1. Filtered modify: only the diamond stack in slot 4 becomes count 9.
	c.ChatCommand(`item modify block 2 64 3 {type:"filtered",slot_source:{type:"slot_range",slots:"container.*"},item_filter:{items:"minecraft:diamond"}} {type:"set_count",count:9}`)
	servertest.ExpectFeedbackSoon(t, c, nil, itemBlockModifyKey,
		servertest.ItemNumber(1), servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3))
	if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:4b}]", "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:diamond"`) || !strings.Contains(got, "count: 9") {
		t.Errorf("slot 4 after filtered modify = %q, want 9 diamonds", got)
	}

	// 2. Group replace: replace has one item, so only container.1 changes.
	c.ChatCommand(`item replace block 2 64 3 [{type:"slot_range",slots:"container.1"},{type:"slot_range",slots:"container.3"}] with gold_ingot 2`)
	servertest.ExpectFeedbackSoon(t, c, nil, itemBlockSetKey,
		servertest.ItemNumber(1), servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3),
		servertest.ItemFeedbackText("minecraft:gold_ingot", servertest.ItemDisplayKey(t, "minecraft:gold_ingot"), 2))
	if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:1b}]", "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:gold_ingot"`) || !strings.Contains(got, "count: 2") {
		t.Errorf("slot 1 after group replace = %q, want 2 gold_ingot", got)
	}
	if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:3b}]", "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:stone"`) || !strings.Contains(got, "count: 3") {
		t.Errorf("slot 3 after group replace = %q, want unchanged 3 stone", got)
	}

	// 3. Contents fill on a shulker box in slot 0: fills all 256 container slots.
	c.ChatCommand("item replace block 2 64 3 container.0 with minecraft:shulker_box 1")
	c.Settle()
	c.ChatCommand(`item fill block 2 64 3 {type:"contents",component:"minecraft:container",slot_source:{type:"slot_range",slots:"container.0"}} with stone 2`)
	servertest.ExpectFeedbackSoon(t, c, nil, itemBlockSetKey,
		servertest.ItemNumber(256), servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3),
		servertest.ItemFeedbackText("minecraft:stone", servertest.ItemDisplayKey(t, "minecraft:stone"), 2))
	if got := plainQuery(t, srv, `data get block 2 64 3 Items[{Slot:0b}].components."minecraft:container"`, "commands.data.block.query"); !strings.Contains(got, "slot: 0") || !strings.Contains(got, "slot: 255") || !strings.Contains(got, `id: "minecraft:stone"`) {
		t.Errorf("shulker box container after contents fill = %q, want 256 stone slots", got)
	}

	// 4. Limit slots modify: modifies the first 2 slots (slot 0 and slot 1).
	c.ChatCommand(`item modify block 2 64 3 {type:"limit_slots",slot_source:{type:"slot_range",slots:"container.*"},limit:2} {type:"set_count",count:7}`)
	servertest.ExpectFeedbackSoon(t, c, nil, itemBlockModifyKey,
		servertest.ItemNumber(2), servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3))
	if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:1b}]", "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:gold_ingot"`) || !strings.Contains(got, "count: 7") {
		t.Errorf("slot 1 after limit_slots modify = %q, want 7 gold_ingot", got)
	}
	if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:2b}]", "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:stone"`) || !strings.Contains(got, "count: 3") {
		t.Errorf("slot 2 after limit_slots modify = %q, want unchanged 3 stone", got)
	}

	// 5. Data-pack slot source id in /item replace ... with.
	c.ChatCommand("item replace block 2 64 3 pack:slot_five with emerald 4")
	servertest.ExpectFeedbackSoon(t, c, nil, itemBlockSetKey,
		servertest.ItemNumber(1), servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3),
		servertest.ItemFeedbackText("minecraft:emerald", servertest.ItemDisplayKey(t, "minecraft:emerald"), 4))
	if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:5b}]", "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:emerald"`) || !strings.Contains(got, "count: 4") {
		t.Errorf("slot 5 after data-pack replace = %q, want 4 emerald", got)
	}

	// 6. Inline slot source in from, and unnamed refusals when nothing is selected.
	c.ChatCommand(`item replace block 2 64 3 container.6 from block 2 64 3 {type:"filtered",slot_source:{type:"slot_range",slots:"container.*"},item_filter:{items:"minecraft:emerald"}}`)
	servertest.ExpectFeedbackSoon(t, c, nil, itemBlockReplaceKey,
		servertest.ItemNumber(1), servertest.ItemNumber(2), servertest.ItemNumber(64), servertest.ItemNumber(3))
	if got := plainQuery(t, srv, "data get block 2 64 3 Items[{Slot:6b}]", "commands.data.block.query"); !strings.Contains(got, `id: "minecraft:emerald"`) || !strings.Contains(got, "count: 4") {
		t.Errorf("slot 6 after filtered from replace = %q, want 4 emerald", got)
	}

	c.ChatCommand(`item modify block 2 64 3 {type:"filtered",slot_source:{type:"slot_range",slots:"container.*"},item_filter:{items:"minecraft:netherite_ingot"}} {type:"set_count",count:1}`)
	awaitItemFailure(t, c, "commands.item.target.no_such_slot.unnamed")

	c.ChatCommand(`item replace block 2 64 3 container.7 from block 2 64 3 {type:"filtered",slot_source:{type:"slot_range",slots:"container.*"},item_filter:{items:"minecraft:netherite_ingot"}}`)
	awaitItemFailure(t, c, "commands.item.source.no_such_slot.unnamed")

	c.Disconnect()
}
