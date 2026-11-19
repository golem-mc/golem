package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Display.ItemDisplay.getSlot, SlotAccess.of
func TestItemDisplayContentsSlot(t *testing.T) {
	t.Parallel()
	srv, c := dialItem(t, "Testificate")
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon item_display 3.5 64 3.5 {item_display:"gui"}`)
	display := c.AwaitSpawn(protocol.EntityTypeItemDisplay)
	c.Settle()
	target := display.Uuid.String()

	write := func(cmd string, count int32) {
		t.Helper()
		c.ChatCommand(cmd)
		var fields []servertest.MetaEntry
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlayEntityMetadata {
				return
			}
			r := protocol.NewReader(data)
			if r.ReadVarInt() == display.EntityID {
				entries, ok := servertest.ReadMetadata(r)
				if !ok || r.Err() != nil {
					t.Fatalf("invalid display metadata: %v", r.Err())
				}
				fields = append(fields, entries...)
			}
		})
		// A slot write must not restart transformation interpolation (index 8).
		if len(fields) != 1 || fields[0].Index != 23 || fields[0].Typ != protocol.MetadataSlot {
			t.Fatalf("%s: metadata = %+v, want only the displayed item at index 23", cmd, fields)
		}
		stack := fields[0].SlotVal
		if count == 0 {
			if !stack.Empty() {
				t.Fatalf("cleared display = %+v, want empty", stack)
			}
		} else if stack.ItemID != int32(registry.ItemStone) || stack.Count != count || stack.CustomData()["marker"] != nbt.Int(7) {
			t.Fatalf("display = %+v, want %d stone with custom data marker=7", stack, count)
		}
	}
	query := func(path, want string) {
		t.Helper()
		c.ChatCommand("data get entity " + target + " " + path)
		line := servertest.FeedbackComponent(t, c)
		if line.Translate != "commands.data.entity.query" || len(line.With) != 2 || line.With[1].Plain() != want {
			t.Fatalf("data get %s = %+v, want %s", path, line, want)
		}
	}
	copyToPlayer := func(slot string, count int32) {
		t.Helper()
		c.ChatCommand("item replace entity @s hotbar.0 from entity " + target + " " + slot)
		updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
		stack := updates[0].Stack
		if updates[0].Slot != 36 || stack.Count != count {
			t.Fatalf("copied display = %+v, want count %d in hotbar.0", updates[0], count)
		}
		if count > 0 && (stack.ItemID != int32(registry.ItemStone) || stack.CustomData()["marker"] != nbt.Int(7)) {
			t.Fatalf("copied display lost its item/components: %+v", stack)
		}
		c.Settle()
	}

	write("item replace entity "+target+" contents with stone[custom_data={marker:7}] 5", 5)
	query("item.id", `"minecraft:stone"`)
	query("item.count", "5")
	query(`item.components."minecraft:custom_data".marker`, "7")
	query("item_display", `"gui"`)
	copyToPlayer("contents", 5)
	write("item modify entity "+target+` contents {type:"set_count",count:9}`, 9)
	query("item.count", "9")
	copyToPlayer("container.0", 9)
	write("item replace entity "+target+" contents with air", 0)
	c.ChatCommand("data get entity " + target + " item")
	line := servertest.FeedbackComponent(t, c)
	if len(line.Extra) == 1 {
		line = line.Extra[0]
	}
	if line.Translate != "arguments.nbtpath.nothing_found" {
		t.Fatalf("empty display item NBT = %+v, want absent item", line)
	}
	write("item replace entity "+target+" container.0 from entity @s hotbar.0", 9)
	query("item.count", "9")
	write("item modify entity "+target+` contents {type:"discard"}`, 0)
	copyToPlayer("contents", 0)
}

// vanilla: Display.ItemDisplay.getSlot, Entity.getSlot
func TestItemDisplayRejectsOtherSlots(t *testing.T) {
	t.Parallel()
	srv, c := dialItem(t, "Testificate")
	servertest.HoldTicks(t, srv)
	for _, kind := range []string{"item_display", "block_display", "text_display", "interaction"} {
		c.ChatCommand("summon " + kind + " 3.5 64 3.5")
		servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
		slots := []string{"contents"}
		if kind == "item_display" {
			slots = []string{"container.1", "weapon.mainhand"}
		}
		for _, slot := range slots {
			target := "@e[type=" + kind + ",limit=1]"
			c.ChatCommand("item replace entity " + target + " " + slot + " with stone")
			awaitItemFailure(t, c, itemTargetNoSlotKey, protocol.Text{Text: slot})
			c.ChatCommand("item replace entity @s hotbar.0 from entity " + target + " " + slot)
			awaitItemFailure(t, c, itemSourceNoSlotKey, protocol.Text{Text: slot})
		}
	}
}
