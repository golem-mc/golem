package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// A piston's move ends at the dimension's build height and at the world
// border: the cell in front of it and every block it moves must lie inside.
// vanilla: PistonBaseBlock.isPushable

// TestPistonCannotPushAboveNetherBuildLimit powers a piston facing up under
// stone at the Nether's top block, y 255; the stone has nowhere to go.
func TestPistonCannotPushAboveNetherBuildLimit(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.NetherTestConfig(t, servertest.BlockTestConfig()), "Actor", servertest.OpsListing(t, "Actor"))

	c.ChatCommand("execute in minecraft:the_nether run tp @s 0.5 128 0.5")
	c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
	c.ExpectViewWithinTicks(srv, 200)

	servertest.Setblock(t, c, 2, 254, 0, "piston[facing=up]")
	servertest.Setblock(t, c, 2, 255, 0, "stone")
	servertest.Setblock(t, c, 3, 254, 0, "redstone_block")
	servertest.RunTicks(t, srv, 3)

	if name, _ := servertest.StateProps(srv.BlockInDimension(world.NetherName, 2, 255, 0)); name != "minecraft:stone" {
		t.Errorf("(2, 255, 0) holds %s after the power, want the stone left in place", name)
	}
	want := servertest.BlockStateWith(t, "minecraft:piston", "extended=false,facing=up")
	if got := srv.BlockInDimension(world.NetherName, 2, 254, 0); got != want {
		name, props := servertest.StateProps(got)
		t.Errorf("the piston is %s%v, want it retracted", name, props)
	}
}

// TestPistonCannotExtendAcrossTheWorldBorder powers a piston whose front cell
// lies just outside a shrunk border; it stays retracted though the cell is air.
func TestPistonCannotExtendAcrossTheWorldBorder(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))

	// Size 10 about the origin: x runs -5 to 5, so x 5 is outside.
	c.ChatCommand("worldborder set 10")
	c.ExpectPlay(protocol.IDPlaySetBorderSize)
	servertest.Setblock(t, c, 4, 64, 0, "piston[facing=east]")
	servertest.Setblock(t, c, 3, 64, 0, "redstone_block")
	servertest.RunTicks(t, srv, 3)

	servertest.ExpectBlockNamed(t, srv, 4, 64, 0, "minecraft:piston", map[string]string{"facing": "east", "extended": "false"})
}
