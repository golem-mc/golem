package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: ItemStackTemplate.MAP_CODEC, BeehiveBlockEntity.saveAdditional,
// VaultBlockEntity.saveAdditional, CrafterBlock.neighborChanged
func TestPlacedBlockEntitiesSaveVanillaDefaults(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		block, path, want string
	}{
		{"decorated_pot", "sherds", `{left: {id: "minecraft:brick"}, back: {id: "minecraft:brick"}, right: {id: "minecraft:brick"}, front: {id: "minecraft:brick"}}`},
		{"beehive", "bees", `[]`},
		{"bee_nest", "bees", `[]`},
		{"vault", "config", `{key_item: {count: 1, id: "minecraft:trial_key"}}`},
		{"crafter", "triggered", `1`},
	} {
		t.Run(tc.block, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)
			servertest.HoldTicks(t, srv)
			if tc.block == "vault" {
				servertest.Setblock(t, c, 5, 64, 0, "vault")
			} else {
				c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:"+tc.block, 1))
				c.Settle()
				c.PlaceBlock(5, 63, 0, protocol.FaceTop)
				c.Settle()
			}
			if tc.block == "crafter" {
				servertest.Setblock(t, c, 5, 64, 1, "redstone_block")
			}
			c.ChatCommand("data get block 5 64 0 " + tc.path)
			if tc.block == "decorated_pot" {
				// A map-based NBTArg loses the codec's side insertion history.
				feedback := servertest.FeedbackComponent(t, c)
				if feedback.Translate != "commands.data.block.query" || len(feedback.With) != 4 {
					t.Fatalf("unexpected block query feedback: %+v", feedback)
				}
				for i, want := range []string{"5", "64", "0", tc.want} {
					if got := feedback.With[i].Plain(); got != want {
						t.Errorf("block query argument %d = %s, want %s", i, got, want)
					}
				}
			} else {
				c.ExpectCommandFeedback("commands.data.block.query", "5", "64", "0", tc.want)
			}
			if tc.block == "crafter" {
				servertest.Setblock(t, c, 5, 64, 1, "air")
				c.ChatCommand("data get block 5 64 0 triggered")
				c.ExpectCommandFeedback("commands.data.block.query", "5", "64", "0", "0")
			}
		})
	}
}

// vanilla: CrafterBlock.newBlockEntity, CrafterBlockEntity.loadAdditional
func TestCrafterPlacedIntoPowerSavesItsTrigger(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	servertest.Setblock(t, c, 5, 64, 1, "redstone_block")
	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:crafter", 1))
	c.Settle()
	c.PlaceBlock(5, 63, 0, protocol.FaceTop)
	c.Settle()
	c.ChatCommand("data get block 5 64 0 triggered")
	c.ExpectCommandFeedback("commands.data.block.query", "5", "64", "0", "1")

	// A data write owns the saved flag until the next power edge.
	c.ChatCommand("data merge block 5 64 0 {triggered:0}")
	c.ExpectCommandFeedback("commands.data.block.modified", "5", "64", "0")
	c.ChatCommand("data get block 5 64 0 triggered")
	c.ExpectCommandFeedback("commands.data.block.query", "5", "64", "0", "0")
	servertest.Setblock(t, c, 5, 64, 1, "air")
	servertest.Setblock(t, c, 5, 64, 1, "redstone_block")
	c.ChatCommand("data get block 5 64 0 triggered")
	c.ExpectCommandFeedback("commands.data.block.query", "5", "64", "0", "1")
}
