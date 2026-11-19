package commands

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func TestNativeStackComponentHistory(t *testing.T) {
	t.Parallel()
	testNativeStackHistory(t, "testdata/stack_component_history.tsv")
}

func TestNativeStackEnchantmentHistory(t *testing.T) {
	t.Parallel()
	testNativeStackHistory(t, "testdata/stack_enchantment_history.tsv")
}

func TestNativeStackMapHistory(t *testing.T) {
	t.Parallel()
	testNativeStackHistory(t, "testdata/stack_map_history.tsv")
}

func TestNativeStackPayloadHistory(t *testing.T) {
	t.Parallel()
	testNativeStackHistory(t, "testdata/stack_payload_history.tsv")
}

func testNativeStackHistory(t *testing.T, fixture string) {
	t.Helper()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var fixtures [][]string
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fixtures = append(fixtures, strings.Split(line, "\t"))
	}
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	ops := servertest.OpsListing(t, "Alice")
	srv, c := servertest.Join(t, cfg, "Alice", ops)
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`setblock 2 64 3 chest`)
	servertest.FeedbackComponent(t, c)
	for i, f := range fixtures {
		stack, err := nbt.ParseOrderedSNBT(f[1])
		if err != nil {
			t.Fatal(err)
		}
		patch, _ := nbt.Get[nbt.Compound](stack.Entries(), "components")
		item := "stick[" + f[2] + "=" + nativeStackInput(patch[f[2]]) + "]"
		c.ChatCommand(fmt.Sprintf("item replace entity @s inventory.%d with %s", i, item))
		servertest.AwaitSystemChatSaying(t, c, "commands.item.entity.replace.success.single.known_item")
		c.ChatCommand(fmt.Sprintf("item replace block 2 64 3 container.%d with %s", i, item))
		servertest.AwaitSystemChatSaying(t, c, "commands.item.block.replace.success.known_item")
		c.ChatCommand(`summon pig 4.5 70 4.5 {NoAI:1b,NoGravity:1b,Tags:["` + f[0] + `"],equipment:{head:` + f[1] + `}}`)
		servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
		c.ChatCommand(`summon item 4.5 70 4.5 {NoGravity:1b,Age:-32768s,PickupDelay:32767s,Tags:["` + f[0] + `"],Item:` + f[1] + `}`)
		servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
		c.ChatCommand(`summon item_display 4.5 70 4.5 {NoGravity:1b,Tags:["` + f[0] + `"],item:` + f[1] + `}`)
		servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	}
	check := func(t *testing.T, c *servertest.Client) {
		t.Helper()
		for i, f := range fixtures {
			for _, source := range []struct{ target, text, path string }{
				{"entity @s", `entity:"@s"`, fmt.Sprintf("Inventory[{Slot:%db}].components.", i+9)},
				{"block 2 64 3", `block:"2 64 3"`, fmt.Sprintf("Items[{Slot:%db}].components.", i)},
				{"entity @e[type=pig,tag=" + f[0] + ",limit=1]", `entity:"@e[type=pig,tag=` + f[0] + `,limit=1]"`, "equipment.head.components."},
				{"entity @e[type=item_display,tag=" + f[0] + ",limit=1]", `entity:"@e[type=item_display,tag=` + f[0] + `,limit=1]"`, "item.components."},
				{"entity @e[type=item,tag=" + f[0] + ",limit=1]", `entity:"@e[type=item,tag=` + f[0] + `,limit=1]"`, "Item.components."},
			} {
				path := source.path + strconv.Quote(f[2])
				for style := range 3 {
					cmd := "data get " + source.target + " " + path
					if style > 0 {
						plain := ""
						if style == 2 {
							plain = ",plain:1b"
						}
						cmd = `tellraw @s {nbt:` + strconv.Quote(path) + `,` + source.text + plain + `}`
					}
					c.ChatCommand(cmd)
					line := servertest.FeedbackComponent(t, c)
					text := line.Plain()
					if style == 0 {
						if len(line.With) < 2 {
							t.Fatalf("%s: feedback %s", cmd, line.Plain())
						}
						text = line.With[len(line.With)-1].Plain()
					}
					want := f[3]
					if style == 2 {
						want = f[4]
					}
					if text != want {
						t.Errorf("%s: got %s, want %s", cmd, text, want)
					}
				}
				c.ChatCommand("data modify storage stackhistory compact set string " + source.target + " " + path)
				line := servertest.FeedbackComponent(t, c)
				if len(line.Extra) == 1 {
					line = line.Extra[0]
				}
				if line.Translate != "commands.data.modify.expected_value" || len(line.With) != 1 || line.With[0].Plain() != f[5] {
					t.Errorf("compact %s %s: %s, want %s", source.target, path, line.Plain(), f[5])
				}
			}
		}
	}
	c.Settle()
	check(t, c)
	// Re-read the native stacks after supported mutations reconstruct their owners.
	c.ChatCommand(`data merge block 2 64 3 {CustomName:"written"}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.data.block.modified")
	for _, f := range fixtures {
		for _, kind := range []string{"pig", "item", "item_display"} {
			c.ChatCommand(`data merge entity @e[type=` + kind + `,tag=` + f[0] + `,limit=1] {Glowing:1b}`)
			servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
		}
	}
	check(t, c)
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	again, connect := servertest.Start(t, cfg, ops)
	servertest.FreezeUntilHeld(t, again)
	client := connect().Join("Alice")
	servertest.HoldTicks(t, again)
	servertest.UntilTicks(t, again, 20, "saved stack owners wake", func() bool {
		return len(again.SelectedEntities("minecraft:pig")) == len(fixtures) && len(again.SelectedEntities("minecraft:item")) == len(fixtures) && len(again.SelectedEntities("minecraft:item_display")) == len(fixtures)
	})
	client.Settle()
	check(t, client)
}

// Keep the parsed collision order in command input; compact SNBT sorts keys.
func nativeStackInput(tag nbt.Tag) string {
	var b strings.Builder
	switch v := tag.(type) {
	case nbt.CompoundTag:
		b.WriteByte('{')
		for i, key := range nbt.HashOrderedKeys(v) {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.Quote(key))
			b.WriteByte(':')
			b.WriteString(nativeStackInput(v.Entries()[key]))
		}
		b.WriteByte('}')
	case nbt.List:
		b.WriteByte('[')
		for i, child := range v.Items {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(nativeStackInput(child))
		}
		b.WriteByte(']')
	default:
		return nbt.Stringify(tag)
	}
	return b.String()
}

func TestBeePayloadHistoryThroughHive(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/stack_payload_history.tsv")
	if err != nil {
		t.Fatal(err)
	}
	var fixture []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "bee_payload\t") {
			fixture = strings.Split(line, "\t")
		}
	}
	if len(fixture) != 6 {
		t.Fatal("missing recorded bee payload")
	}
	stack, err := nbt.ParseOrderedSNBT(fixture[1])
	if err != nil {
		t.Fatal(err)
	}
	patch, _ := nbt.Get[nbt.Compound](stack.Entries(), "components")
	payload := nativeStackInput(patch[fixture[2]])
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	ops := servertest.OpsListing(t, "Alice")
	srv, c := servertest.Join(t, cfg, "Alice", ops)
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`setblock 2 64 3 beehive{bees:[{entity_data:` + payload + `,ticks_in_hive:1,min_ticks_in_hive:2000000}]}`)
	servertest.FeedbackComponent(t, c)
	check := func(c *servertest.Client) {
		t.Helper()
		for style := range 3 {
			cmd := `data get block 2 64 3 bees[0].entity_data`
			if style > 0 {
				plain := ""
				if style == 2 {
					plain = ",plain:1b"
				}
				cmd = `tellraw @s {nbt:"bees[0].entity_data",block:"2 64 3"` + plain + `}`
			}
			c.ChatCommand(cmd)
			line := servertest.FeedbackComponent(t, c)
			text := line.Plain()
			if style == 0 {
				if len(line.With) < 2 {
					t.Fatalf("%s: %s", cmd, text)
				}
				text = line.With[len(line.With)-1].Plain()
			}
			want := fixture[3]
			if style == 2 {
				want = fixture[4]
			}
			if text != want {
				t.Errorf("%s: got %s, want %s", cmd, text, want)
			}
		}
	}
	check(c)
	servertest.RunTicks(t, srv, 3)
	check(c)
	c.ChatCommand(`data modify block 2 64 3 bees[0].entity_data.id set value "bee"`)
	servertest.AwaitSystemChatSaying(t, c, "commands.data.block.modified")
	check(c)
	c.Disconnect()
	srv.Shutdown()
	again, connect := servertest.Start(t, cfg, ops)
	servertest.FreezeUntilHeld(t, again)
	client := connect().Join("Alice")
	servertest.HoldTicks(t, again)
	check(client)
	// Ordered native occupant records remain usable by the ticking/release readers.
	client.ChatCommand(`time set day`)
	servertest.FeedbackComponent(t, client)
	client.ChatCommand(`data modify block 2 64 3 bees[0].min_ticks_in_hive set value 0`)
	servertest.AwaitSystemChatSaying(t, client, "commands.data.block.modified")
	servertest.UntilTicks(t, again, 10, "stored bee releases", func() bool { return len(again.SelectedEntities("minecraft:bee")) == 1 })
}
