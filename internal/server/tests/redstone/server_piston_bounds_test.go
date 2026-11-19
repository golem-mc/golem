package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A piston's move ends at the dimension's build height and at the world
// border: the cell in front of it and every block it moves must lie inside.
// vanilla: PistonBaseBlock.isPushable

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
