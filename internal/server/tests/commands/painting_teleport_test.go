package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// vanilla: Entity.teleportCrossDimension, BlockAttachedEntity.teleportTo
func TestPaintingTeleportAcrossDimensionsPreservesDistantDestination(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Op", servertest.OpsListing(t, "Op"))
	servertest.HoldTicks(t, srv)
	servertest.FreezeUntilHeld(t, srv)
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 100, 100))
	c.ChatCommand(`summon painting 12 200 5 {variant:"minecraft:kebab",facing:0b,Tags:["travelling"]}`)
	spawn := c.AwaitSpawn(protocol.EntityTypePainting)
	c.AwaitMetadataFor(spawn.EntityID)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.ChatCommand("execute in minecraft:the_nether run tp " + spawn.Uuid.String() + " 100.75 200.25 100.125")
	servertest.AwaitSystemChatSaying(t, c, "commands.teleport.success.location.single")
	// The saved-anchor guard rejects an anchor over 16 blocks from Pos.
	// The transfer must update both before loading the destination body.
	const selector = "@e[type=painting,tag=travelling,distance=..2,limit=1]"
	c.ChatCommand("execute in minecraft:the_nether positioned 100 200 100 run data get entity " + selector + " block_pos")
	c.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:painting", "entity.minecraft.painting", spawn.Uuid),
		servertest.NBTArg("[I; 100, 200, 100]"))
	c.ChatCommand("execute positioned 12 200 5 if entity " + selector)
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.execute.conditional.fail"))
	c.Disconnect()
}
