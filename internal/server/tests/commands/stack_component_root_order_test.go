package commands

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestStackComponentRootOrder uses three names sharing a 16-slot hash bucket. Each
// phase is recorded from a mutated vanilla stack, including a reset to the
// item's default followed by reinsertion (PatchedDataComponentMap.set).
func TestStackComponentRootOrder(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/stack_component_root_history.tsv")
	if err != nil {
		t.Fatal(err)
	}
	fixtures := map[string][]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("invalid fixture: %s", line)
		}
		fixtures[f[0]] = f
	}
	orders := []struct{ tag, components string }{
		{"ab", `damage=1,block_state={axis:"x"}`},
		{"ba", `block_state={axis:"x"},damage=1`},
	}
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	ops := servertest.OpsListing(t, "Alice")
	srv, c := servertest.Join(t, cfg, "Alice", ops)
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`setblock 2 64 3 chest`)
	servertest.FeedbackComponent(t, c)
	type owner struct{ target, text, path, slot string }
	owners := func(i int, tag string) []owner {
		return []owner{
			{"entity @s", `entity:"@s"`, fmt.Sprintf("Inventory[{Slot:%db}].components", i+9), fmt.Sprintf("inventory.%d", i)},
			{"block 2 64 3", `block:"2 64 3"`, fmt.Sprintf("Items[{Slot:%db}].components", i), fmt.Sprintf("container.%d", i)},
			{"entity @e[type=pig,tag=" + tag + ",limit=1]", `entity:"@e[type=pig,tag=` + tag + `,limit=1]"`, "equipment.head.components", ""},
			{"entity @e[type=item_display,tag=" + tag + ",limit=1]", `entity:"@e[type=item_display,tag=` + tag + `,limit=1]"`, "item.components", ""},
			{"entity @e[type=item,tag=" + tag + ",limit=1]", `entity:"@e[type=item,tag=` + tag + `,limit=1]"`, "Item.components", ""},
		}
	}
	for i, o := range orders {
		item := "diamond_sword[" + o.components + "]"
		for _, source := range owners(i, o.tag)[:2] {
			c.ChatCommand("item replace " + source.target + " " + source.slot + " with " + item)
			if source.target == "entity @s" {
				servertest.AwaitSystemChatSaying(t, c, "commands.item.entity.replace.success.single.known_item")
			} else {
				servertest.AwaitSystemChatSaying(t, c, "commands.item.block.replace.success.known_item")
			}
		}
		f, ok := fixtures[o.tag+"_initial"]
		if !ok {
			t.Fatal("missing initial fixture")
		}
		stack := f[1]
		c.ChatCommand(`summon pig 4.5 70 4.5 {NoAI:1b,NoGravity:1b,Tags:["` + o.tag + `"],equipment:{head:` + stack + `}}`)
		servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
		c.ChatCommand(`summon item 4.5 70 4.5 {NoGravity:1b,Age:-32768s,PickupDelay:32767s,Tags:["` + o.tag + `"],Item:` + stack + `}`)
		servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
		c.ChatCommand(`summon item_display 4.5 70 4.5 {NoGravity:1b,Tags:["` + o.tag + `"],item:` + stack + `}`)
		servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	}
	check := func(c *servertest.Client, phase string) {
		t.Helper()
		for i, o := range orders {
			f, ok := fixtures[o.tag+"_"+phase]
			if !ok {
				t.Fatalf("missing %s %s fixture", o.tag, phase)
			}
			for _, source := range owners(i, o.tag) {
				for style := range 3 {
					cmd := "data get " + source.target + " " + source.path
					if style > 0 {
						plain := ""
						if style == 2 {
							plain = ",plain:1b"
						}
						cmd = `tellraw @s {nbt:` + strconv.Quote(source.path) + `,` + source.text + plain + `}`
					}
					c.ChatCommand(cmd)
					line := servertest.FeedbackComponent(t, c)
					text := line.Plain()
					if style == 0 {
						if len(line.With) < 2 {
							t.Fatalf("%s: feedback %s", cmd, text)
						}
						text = line.With[len(line.With)-1].Plain()
					}
					want := f[3]
					if style == 2 {
						want = f[4]
					}
					if text != want {
						t.Errorf("%s %s: got %s, want %s", phase, cmd, text, want)
					}
				}
				c.ChatCommand("data modify storage stackhistory compact set string " + source.target + " " + source.path)
				line := servertest.FeedbackComponent(t, c)
				if len(line.Extra) == 1 {
					line = line.Extra[0]
				}
				if line.Translate != "commands.data.modify.expected_value" || len(line.With) != 1 || line.With[0].Plain() != f[5] {
					t.Errorf("%s compact %s %s: got %s, want %s", phase, source.target, source.path, line.Plain(), f[5])
				}
			}
		}
	}
	c.Settle()
	check(c, "initial")
	for _, step := range []struct{ phase, patch string }{
		{"insert", `"minecraft:unbreakable":{}`},
		{"replace", `"minecraft:damage":2`},
		{"removed", `"minecraft:damage":0`},
		{"reinsert", `"minecraft:damage":3`},
	} {
		for i, o := range orders {
			for _, source := range owners(i, o.tag) {
				if source.target == "entity @s" {
					// Players refuse /data writes; /item modify changes their live stack.
					c.ChatCommand(`item modify ` + source.target + ` ` + source.slot + ` {type:"set_components",components:{` + step.patch + `}}`)
					servertest.AwaitSystemChatSaying(t, c, "commands.item.entity.modify.success.single")
					continue
				}
				if step.phase == "removed" {
					c.ChatCommand(`data remove ` + source.target + ` ` + source.path + `."minecraft:damage"`)
				} else {
					c.ChatCommand(`data modify ` + source.target + ` ` + source.path + ` merge value {` + step.patch + `}`)
				}
				if source.target == "block 2 64 3" {
					servertest.AwaitSystemChatSaying(t, c, "commands.data.block.modified")
				} else {
					servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
				}
			}
		}
		c.Settle()
		check(c, step.phase)
	}
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	again, connect := servertest.Start(t, cfg, ops)
	servertest.FreezeUntilHeld(t, again)
	client := connect().Join("Alice")
	servertest.HoldTicks(t, again)
	servertest.UntilTicks(t, again, 20, "saved stack owners wake", func() bool {
		return len(again.SelectedEntities("minecraft:pig")) == len(orders) && len(again.SelectedEntities("minecraft:item")) == len(orders) && len(again.SelectedEntities("minecraft:item_display")) == len(orders)
	})
	client.Settle()
	check(client, "reinsert")
}
