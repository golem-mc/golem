package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ItemParser.State.readComponent, CommonDialogData.MAP_CODEC
func TestGiveMalformedDialogPreservesMissingTitle(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)

	for _, tc := range []struct{ component, value string }{
		{"custom_name", `{text:"Bad",click_event:{action:"show_dialog",dialog:{type:"notice"}}}`},
		{"item_name", `[{text:"Bad",click_event:{action:"show_dialog",dialog:{type:"notice"}}}]`},
	} {
		t.Run(tc.component, func(t *testing.T) {
			input := "give @s stone[" + tc.component + "=" + tc.value + "]"
			want := [][]byte{
				servertest.CommandErrorMessageForTest("arguments.item.component.malformed", "minecraft:"+tc.component, `No key title in MapLike[{type:"notice"}]`),
				servertest.CommandErrorContextForTest(input, strings.Index(input, "=")+1),
			}
			c.ChatCommand(input)
			messages := 0
			c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
				switch id {
				case protocol.IDPlayContainerSlot, protocol.IDPlayContainerContent:
					t.Fatal("malformed dialog changed the inventory")
				case protocol.IDPlaySystemChat:
					if !bytes.Equal(data, want[messages]) {
						t.Errorf("error message %d = %q, want %q", messages, data, want[messages])
					}
					messages++
				}
				return messages == len(want)
			})
			if messages != len(want) {
				t.Fatalf("got %d error messages, want %d", messages, len(want))
			}
			c.ChatCommand("clear @s stone 0")
			servertest.AwaitCommandErrorText(t, c, "clear.failed.single", servertest.PlayerDisplayComponent("Tester"))
		})
	}
	c.Disconnect()
}

// vanilla: ItemParser.State.readComponent, ItemStackTemplate.MAP_CODEC
func TestGiveUseRemainderCountDiagnostic(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)

	for _, count := range []string{"0", "100"} {
		input := "give @s apple[use_remainder={id:stick,count:" + count + "}]"
		want := [][]byte{
			servertest.CommandErrorMessageForTest("arguments.item.component.malformed", "minecraft:use_remainder", "Value must be within range [1;99]: "+count),
			servertest.CommandErrorContextForTest(input, strings.Index(input, "=")+1),
		}
		c.ChatCommand(input)
		messages := 0
		c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
			switch id {
			case protocol.IDPlayContainerSlot, protocol.IDPlayContainerContent:
				t.Fatalf("count %s changed the inventory", count)
			case protocol.IDPlaySystemChat:
				if !bytes.Equal(data, want[messages]) {
					t.Errorf("count %s error message %d = %q, want %q", count, messages, data, want[messages])
				}
				messages++
			}
			return messages == len(want)
		})
		if messages != len(want) {
			t.Fatalf("count %s: %d error messages, want %d", count, messages, len(want))
		}
	}

	c.ChatCommand("give @s apple[use_remainder={id:stick,count:2}]")
	c.Settle()
	if got := srv.InventorySlot("Tester", player.HotbarSlot); registry.Item(got.ItemID) != registry.ItemApple {
		t.Fatalf("count 2 gave %+v, want an apple", got)
	}
}
