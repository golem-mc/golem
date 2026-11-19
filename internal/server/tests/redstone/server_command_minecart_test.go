package redstone

import (
	"strconv"
	"strings"
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

// TestACommandMinecartRunsAsItselfEveryFourTicksAndFeedsADetectorRail parks a
// programmed cart on a powered activator rail and reads, tick by tick: the
// run is as the cart (@s is it, at its position), runs are four ticks apart,
// the success count and output are saved and reach a comparator on a
// detector rail the cart rests across.
// vanilla: MinecartCommandBlock.activateMinecart, DetectorRailBlock.getAnalogOutputSignal
func TestACommandMinecartRunsAsItselfEveryFourTicksAndFeedsADetectorRail(t *testing.T) {
	t.Parallel()
	srv, op := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))
	servertest.HoldTicks(t, srv)

	// The scene: a powered activator at x=3, and a comparator at x=5 feeding a
	// wire at x=6, its input the detector rail laid at x=4 later. The cart
	// rests at x=3.95: on the activator's block, its box across the detector's.
	servertest.LayComparatorScene(t, op, srv, 5, 64, 0)
	srv.SetBlock(3, 63, 0, servertest.MustState(t, "minecraft:stone", nil))
	srv.SetBlock(3, 64, 0, servertest.MustState(t, "minecraft:activator_rail",
		map[string]string{"shape": "east_west", "powered": "true"}))
	srv.RunLine("summon " + commandCart + " 3.95 64.0625 0.5")
	carts := srv.MinecartsInDimension(world.DimensionName)
	if len(carts) != 1 {
		t.Fatalf("carts = %+v", carts)
	}

	// Only the cart itself, at its own position, places the block.
	const line = "execute if entity @s[type=minecraft:command_block_minecart] run setblock ~ ~-3 ~ minecraft:gold_block"
	programMinecart(op, carts[0].ID, line, true)
	servertest.AwaitCommandFeedback(t, op, "advMode.setCommand.success", line)
	id := carts[0].ID
	cmd, ok := op.MetaEntryWithinTicks(srv, 2, id, protocol.MetadataIndexMinecartCommand)
	if !ok || cmd.Typ != protocol.MetadataString || cmd.StringVal != line {
		t.Fatalf("programming sent command metadata %+v (arrived %v), want %q", cmd, ok, line)
	}

	// The metadata read above ran the first tick (the delta goes out at the
	// end of it); the first run is the fourth.
	gold := servertest.MustState(t, "minecraft:gold_block", nil)
	servertest.RunTicks(t, srv, 2)
	if srv.BlockAt(3, 61, 0) == gold {
		t.Fatal("the cart ran before four ticks passed")
	}
	servertest.UntilTicks(t, srv, 1, "the cart's first run", func() bool { return srv.BlockAt(3, 61, 0) == gold })
	out, ok := op.MetaEntryWithinTicks(srv, 1, id, protocol.MetadataIndexMinecartLastOutput)
	if !ok || out.Typ != protocol.MetadataText || out.TextVal == nil {
		t.Fatalf("the run sent output metadata %+v (arrived %v)", out, ok)
	}
	if got := entityNBT(t, srv, "SuccessCount"); got != "1" {
		t.Fatalf("SuccessCount = %s, want 1", got)
	}
	if got := entityNBT(t, srv, "LastOutput"); got == "" || got == `""` {
		t.Fatalf("LastOutput = %s, want the setblock feedback", got)
	}
	first := entityNBT(t, srv, "LastExecution")
	// The detector rail's comparator reads the cart's success count.
	// The detector is laid after the run so its first read, before the next
	// run clears the count, is the count the run left.
	servertest.Setblock(t, op, 4, 64, 0, "detector_rail[powered=false,shape=east_west,waterlogged=false]")
	op.Settle()
	servertest.UntilTicks(t, srv, 3, "the comparator reads the cart's success count", func() bool {
		return servertest.WirePowerAt(srv, 6, 64, 0) == "1"
	})

	// The next run is exactly four game ticks after the first.
	servertest.UntilTicks(t, srv, 4, "the second run", func() bool { return entityNBT(t, srv, "LastExecution") != first })
	firstTime, err1 := strconv.ParseInt(strings.TrimSuffix(first, "L"), 10, 64)
	secondTime, err2 := strconv.ParseInt(strings.TrimSuffix(entityNBT(t, srv, "LastExecution"), "L"), 10, 64)
	if err1 != nil || err2 != nil || secondTime-firstTime != 4 {
		t.Fatalf("runs at game time %s and %v, want four ticks apart", first, secondTime)
	}
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
