package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: ShulkerBoxBlockEntity.saveAdditional, ContainerHelper.saveAllItems
func TestShulkerBoxSavesItemsOnlyWhileFilled(t *testing.T) {
	t.Parallel()
	for _, block := range []string{"shulker_box", "purple_shulker_box"} {
		t.Run(block, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)
			servertest.HoldTicks(t, srv)
			c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:"+block, 1))
			c.Settle()
			c.PlaceBlock(5, 63, 0, protocol.FaceTop)
			c.Settle()

			c.ChatCommand("data get block 5 64 0 Items")
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "Items"))

			c.ChatCommand(`data merge block 5 64 0 {Items:[{Slot:0b,id:"minecraft:diamond",count:3}]}`)
			c.ExpectCommandFeedback("commands.data.block.modified", "5", "64", "0")
			c.ChatCommand("data get block 5 64 0 Items[0].id")
			c.ExpectCommandFeedback("commands.data.block.query", "5", "64", "0", `"minecraft:diamond"`)
			c.ChatCommand("data get block 5 64 0 Items[0].count")
			c.ExpectCommandFeedback("commands.data.block.query", "5", "64", "0", "3")

			c.ChatCommand("data remove block 5 64 0 Items[0]")
			c.ExpectCommandFeedback("commands.data.block.modified", "5", "64", "0")
			c.ChatCommand("data get block 5 64 0 Items")
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "Items"))
		})
	}
}
