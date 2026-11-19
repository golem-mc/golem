package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

const commandCart = "minecraft:command_block_minecart"

func programMinecart(c *servertest.Client, id int32, line string, track bool) {
	c.T.Helper()
	c.Send(protocol.IDPlaySetCommandMinecartIn, func(b *protocol.Buffer) {
		b.PutVarInt(id)
		b.PutString(line)
		b.PutBool(track)
	})
}

// TestACommandMinecartRunsItsCommandOnAPoweredActivatorRail: an op programs
// a summoned cart through the packet, a non-op's packet changes nothing, and
// the cart rolling over a powered activator rail runs the command.
// vanilla: handleSetCommandMinecart, MinecartCommandBlock.activateMinecart
func TestACommandMinecartRunsItsCommandOnAPoweredActivatorRail(t *testing.T) {
	t.Parallel()
	srv, op := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))
	other := servertest.DialServer(t, srv).Join("Bystander")
	servertest.HoldTicks(t, srv)

	for x := int32(2); x <= 8; x++ {
		srv.SetBlock(x, 64, 1, servertest.MustState(t, "minecraft:rail", map[string]string{"shape": "east_west"}))
	}
	srv.SetBlock(5, 64, 1, servertest.MustState(t, "minecraft:activator_rail",
		map[string]string{"shape": "east_west", "powered": "true"}))
	srv.RunLine("summon " + commandCart + " 2.5 64 1.5")
	carts := srv.MinecartsInDimension(world.DimensionName)
	if len(carts) != 1 {
		t.Fatalf("carts = %+v", carts)
	}
	id := carts[0].ID

	const line = "setblock ~ ~-2 ~ minecraft:gold_block"
	programMinecart(other, id, line, true)
	other.Settle()
	if got := entityNBT(t, srv, "Command"); got != `""` {
		t.Fatalf("a non-op's packet set the command to %s", got)
	}

	programMinecart(op, id, line, true)
	servertest.AwaitCommandFeedback(t, op, "advMode.setCommand.success", line)
	if got := entityNBT(t, srv, "Command"); got != `"`+line+`"` {
		t.Fatalf("Command = %s", got)
	}

	// Give the cart a push east onto the activator.
	srv.RunLine("data merge entity @e[type=command_block_minecart,limit=1] {Motion:[0.3d,0d,0d]}")
	gold := servertest.MustState(t, "minecraft:gold_block", nil)
	servertest.UntilTicks(t, srv, 12, "the activated cart places a gold block", func() bool {
		for y := int32(60); y < 64; y++ {
			for x := int32(2); x <= 8; x++ {
				if srv.BlockAt(x, y, 1) == gold {
					return true
				}
			}
		}
		return false
	})
}

// entityNBT reads one top-level field of the first command cart through
// /data get entity.
func entityNBT(t *testing.T, srv interface{ RunLine(string) []protocol.Text }, path string) string {
	t.Helper()
	for _, text := range srv.RunLine("data get entity @e[type=command_block_minecart,limit=1] " + path) {
		if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
			return text.With[1].Plain()
		}
	}
	t.Fatal("no /data get entity answer")
	return ""
}
