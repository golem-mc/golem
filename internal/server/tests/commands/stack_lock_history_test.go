package commands

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestStackLockHistoryThroughPlacedAndPickedContainer(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/stack_map_history.tsv")
	if err != nil {
		t.Fatal(err)
	}
	var fixture []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "lock\t") {
			fixture = strings.Split(line, "\t")
		}
	}
	if len(fixture) != 6 {
		t.Fatal("lock fixture missing")
	}
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	ops := servertest.OpsListing(t, "Alice")
	srv, c := servertest.Join(t, cfg, "Alice", ops)
	servertest.HoldTicks(t, srv)
	servertest.PrepareGameMasterPlacement(c)
	// The lock is server-only; picking must keep it in the server's slot.
	c.ChatCommand(`item replace entity @s weapon.mainhand with chest[lock={items:"minecraft:stick",count:{min:1,max:3},components:{"minecraft:custom_data":{BB:1,Aa:2,v:3,f:4}},predicates:{"minecraft:custom_data":{BB:1,Aa:2,v:3,f:4}}}]`)
	servertest.AwaitSystemChatSaying(t, c, "commands.item.entity.replace.success.single.known_item")
	c.PlaceBlock(3, 63, 0, protocol.FaceTop)
	c.Settle()
	check := func(c *servertest.Client, target, selector, path string) {
		t.Helper()
		for style := range 3 {
			command := "data get " + target + " " + path
			if style > 0 {
				plain := ""
				if style == 2 {
					plain = ",plain:1b"
				}
				command = `tellraw @s {nbt:` + strconv.Quote(path) + `,` + selector + plain + `}`
			}
			c.ChatCommand(command)
			line := servertest.FeedbackComponent(t, c)
			got, want := line.Plain(), fixture[3]
			if style == 0 {
				if len(line.With) == 0 {
					t.Fatalf("%s: %s", command, got)
				}
				got = line.With[len(line.With)-1].Plain()
			}
			if style == 2 {
				want = fixture[4]
			}
			if got != want {
				t.Fatalf("%s: %s, want %s", command, got, want)
			}
		}
	}
	check(c, "block 3 64 0", `block:"3 64 0"`, "lock")
	servertest.PickedStack(t, c, 3, 64, 0, true)
	c.Settle()
	check(c, "entity @s", `entity:"@s"`, `SelectedItem.components."minecraft:lock"`)
	c.PlaceBlock(5, 63, 0, protocol.FaceTop)
	c.Settle()
	check(c, "block 5 64 0", `block:"5 64 0"`, "lock")
	c.Disconnect()
	srv.Shutdown()
	again, connect := servertest.Start(t, cfg, ops)
	servertest.FreezeUntilHeld(t, again)
	c = connect().Join("Alice")
	servertest.HoldTicks(t, again)
	for _, x := range []int{3, 5} {
		check(c, fmt.Sprintf("block %d 64 0", x), fmt.Sprintf(`block:"%d 64 0"`, x), "lock")
	}
}
