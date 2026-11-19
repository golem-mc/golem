package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: ItemStack.MAP_CODEC, TagValueInput.read, ContainerHelper.loadAllItems
func TestChestDataMergeDefaultsOutOfRangeStackCounts(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	c.ChatCommand("setblock 5 64 0 chest")
	c.ExpectCommandFeedback("commands.setblock.success", "5", "64", "0")
	c.ChatCommand(`data merge block 5 64 0 {Items:[{Slot:0b,id:"minecraft:stone",count:200},{Slot:1b,id:"minecraft:dirt",count:0}]}`)
	c.ExpectCommandFeedback("commands.data.block.modified", "5", "64", "0")
	for _, slot := range []struct{ path, item string }{
		{"Items[0]", "minecraft:stone"},
		{"Items[1]", "minecraft:dirt"},
	} {
		c.ChatCommand("data get block 5 64 0 " + slot.path + ".id")
		c.ExpectCommandFeedback("commands.data.block.query", "5", "64", "0", `"`+slot.item+`"`)
		c.ChatCommand("data get block 5 64 0 " + slot.path + ".count")
		c.ExpectCommandFeedback("commands.data.block.query", "5", "64", "0", "1")
	}
}
