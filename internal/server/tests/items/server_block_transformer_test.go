package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Item.useOn, BlockTransformer.transformBlock
func TestBuiltInBlockTransformerStack(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"survival", "creative"} {
		t.Run(mode, func(t *testing.T) {
			cfg := servertest.SurvivalTestConfig()
			cfg.GameMode = mode
			srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Transformer"))
			c := connect().Join("Transformer")
			servertest.HoldTicks(t, srv)
			for _, tc := range []struct {
				item, before, after string
				count, damage       int32
			}{
				{`stick[block_transformer="minecraft:axe"] 4`, "oak_log", "stripped_oak_log", 3, 0},
				{`wooden_axe[block_transformer="minecraft:shovel"]`, "dirt", "dirt_path", 1, 1},
				{`stick[block_transformer="minecraft:hoe"] 4`, "dirt", "farmland", 3, 0},
				{`wooden_axe[!block_transformer]`, "oak_log", "oak_log", 1, 0},
				{`wooden_axe`, "oak_log", "stripped_oak_log", 1, 1},
				{`stick[block_transformer="minecraft:axe"] 4`, "stone", "stone", 4, 0},
			} {
				t.Run(tc.item+"/"+tc.before, func(t *testing.T) {
					srv.GiveSlot("Transformer", player.HotbarSlot, protocol.ItemStack{})
					c.ChatCommand("give @s " + tc.item)
					c.Settle()
					given := srv.InventorySlot("Transformer", player.HotbarSlot)
					if given.Empty() {
						t.Fatal("/give did not supply the transformer stack")
					}
					if mode == "survival" && tc.before == "oak_log" && tc.item != "wooden_axe" {
						// Reload the player's saved patch, including removals, before use.
						c.Disconnect()
						servertest.UntilTicks(t, srv, 20, "disconnect", func() bool { return srv.PlayerCount() == 0 })
						c = connect().Join("Transformer")
						if got := srv.InventorySlot("Transformer", player.HotbarSlot); !got.Equal(given) {
							t.Fatalf("saved stack = %+v, want %+v", got, given)
						}
					}
					before := registry.BlockStates["minecraft:"+tc.before]
					after := registry.BlockStates["minecraft:"+tc.after]
					if tc.before == "oak_log" {
						before = servertest.BlockStateWith(t, "minecraft:oak_log", "axis=x")
						after = servertest.BlockStateWith(t, "minecraft:"+tc.after, "axis=x")
					}
					srv.SetBlock(3, 63, 0, before)
					srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:air"])
					c.UseBlock(3, 63, 0)
					c.Settle()
					if got := srv.BlockAt(3, 63, 0); got != after {
						t.Errorf("block = %d, want %d", got, after)
					}
					count, damage := tc.count, tc.damage
					if mode == "creative" {
						count, damage = given.Count, 0
					}
					if got := srv.InventorySlot("Transformer", player.HotbarSlot); got.Count != count || got.Damage != damage {
						t.Errorf("cost left count=%d damage=%d, want count=%d damage=%d", got.Count, got.Damage, count, damage)
					}
				})
			}
		})
	}
}
