package commands

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestSlotSources checks that /execute if items and if slots take an inline
// slot source and a data pack's one by id, and that a loot table's slots
// entry copies what its source finds in the runner's inventory.
// vanilla: SlotSourceArgument, SlotSources, SlotLoot
func TestSlotSources(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil); registry.ApplyPackSlotSources(nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pack")
	writeFile(t, filepath.Join(data, "slot_source", "hotbar.json"), `{"type":"minecraft:slot_range","slots":"hotbar.*"}`)
	writeFile(t, filepath.Join(data, "slot_source", "limited.json"),
		`{"type":"minecraft:limit_slots","limit":2,"slot_source":"pack:hotbar"}`)
	writeFile(t, filepath.Join(data, "loot_table", "copy_hotbar.json"),
		`{"pools":[{"rolls":1,"entries":[{"type":"minecraft:slots","slot_source":{"type":"minecraft:slot_range","source":"this","slots":"hotbar.3"}}]}]}`)
	_, c := joinPacks(t, cfg)

	c.ChatCommand("item replace entity @s hotbar.3 with minecraft:diamond")
	c.Settle()

	pass := func(command string, count int) {
		t.Helper()
		c.ChatCommand(command)
		if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(int64(count))) {
			t.Errorf("%q answered % X, want %d counted", command, got, count)
		}
	}
	fail := func(command string) {
		t.Helper()
		c.ChatCommand(command)
		if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
			t.Errorf("%q answered % X, want the conditional failure", command, got)
		}
	}
	const filtered = `execute if items entity @s {type:"minecraft:filtered",slot_source:{type:"minecraft:slot_range",slots:"hotbar.*"},item_filter:{items:"minecraft:%s"}} *`
	pass(fmt.Sprintf(filtered, "diamond"), 1)
	fail(fmt.Sprintf(filtered, "emerald"))

	pass("execute if slots entity @s pack:hotbar", 9)
	pass("execute if slots entity @s pack:limited", 2)
	pass(`execute if slots entity @s [{type:"minecraft:empty"},"pack:hotbar"]`, 9)

	// The copy joins the diamond in slot 3, which now holds two.
	expectGave(t, c, "loot give @s loot pack:copy_hotbar", "minecraft:diamond", 2)

	c.ChatCommand("execute if slots entity @s pack:nothing")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got,
		servertest.CommandErrorMessageForTest("argument.resource_or_id.no_such_element", "pack:nothing", "minecraft:slot_source")) {
		t.Errorf("an unknown source answered % X, want the unknown element refusal", got)
	}
	c.Disconnect()
}
