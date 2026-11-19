package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// A head or skull takes POWERED from the neighbour signal, and one set down
// beside power arrives powered.
// vanilla: AbstractSkullBlock.neighborChanged, AbstractSkullBlock.getStateForPlacement

// TestAHeadFollowsTheSignalBesideIt covers both edges, for a floor and a
// wall head.
func TestAHeadFollowsTheSignalBesideIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 5, 64, 4, "dragon_head")
	servertest.Setblock(t, c, 9, 64, 4, "piglin_wall_head[facing=north]")
	c.ChatCommand("setblock 5 64 5 redstone_block")
	c.ChatCommand("setblock 9 64 5 redstone_block")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 5, 64, 4, "minecraft:dragon_head", map[string]string{"powered": "true"})
	servertest.ExpectBlockNamed(t, srv, 9, 64, 4, "minecraft:piglin_wall_head", map[string]string{"powered": "true"})

	c.ChatCommand("setblock 5 64 5 air")
	c.ChatCommand("setblock 9 64 5 air")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 5, 64, 4, "minecraft:dragon_head", map[string]string{"powered": "false"})
	servertest.ExpectBlockNamed(t, srv, 9, 64, 4, "minecraft:piglin_wall_head", map[string]string{"powered": "false"})
}

// TestAHeadSetDownBesideAPoweredBlockStartsPowered is the placement half.
func TestAHeadSetDownBesideAPoweredBlockStartsPowered(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 5, 64, 5, "redstone_block")
	servertest.PlaceFrom(c, "minecraft:dragon_head", 5, 3, 270, 5, 63, 4, protocol.FaceTop)
	servertest.ExpectBlockNamed(t, srv, 5, 64, 4, "minecraft:dragon_head", map[string]string{"powered": "true"})

	servertest.PlaceFrom(c, "minecraft:dragon_head", 8, 3, 270, 8, 63, 4, protocol.FaceTop)
	servertest.ExpectBlockNamed(t, srv, 8, 64, 4, "minecraft:dragon_head", map[string]string{"powered": "false"})
}
