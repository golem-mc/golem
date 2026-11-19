package tools

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Item.useOn, BlockItem.useOn, HoneycombItem.useOn, ShearsItem.useOn,
// CompassItem.useOn, MapItem.useOn
func TestBlockTransformerRespectsUseOnOverrides(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Transformer", servertest.OpsListing(t, "Transformer"))
	servertest.HoldTicks(t, srv)
	for _, tc := range []struct {
		item, transformer, before, after, above, wantAbove string
	}{
		{"ender_eye", "axe", "oak_log", "oak_log", "air", "air"},
		{"honeycomb", "axe", "oak_log", "oak_log", "air", "air"},
		{"honeycomb", "axe", "exposed_copper", "waxed_exposed_copper", "air", "air"},
		{"bone_meal", "axe", "oak_log", "oak_log", "air", "air"},
		{"lead", "axe", "oak_log", "oak_log", "air", "air"},
		{"potion", "axe", "oak_log", "oak_log", "air", "air"},
		{"minecart", "axe", "oak_log", "oak_log", "air", "air"},
		{"end_crystal", "axe", "oak_log", "oak_log", "air", "air"},
		{"flint_and_steel", "axe", "oak_log", "oak_log", "stone", "stone"},
		{"fire_charge", "axe", "oak_log", "oak_log", "stone", "stone"},
		{"compass", "axe", "oak_log", "stripped_oak_log", "air", "air"},
		{"shears", "axe", "oak_log", "stripped_oak_log", "air", "air"},
		{"filled_map", "axe", "oak_log", "stripped_oak_log", "air", "air"},
		{"compass", "hoe", "grass_block", "farmland", "air", "air"},
		{"filled_map", "shovel", "grass_block", "dirt_path", "air", "air"},
		{"dirt", "hoe", "grass_block", "grass_block", "air", "dirt"},
		{"dirt", "axe", "oak_log", "oak_log", "air", "dirt"},
		// A seed cannot place its crop on grass, so BlockItem calls Item.useOn.
		{"wheat_seeds", "hoe", "grass_block", "farmland", "air", "air"},
		{"dirt", "axe", "oak_log", "stripped_oak_log", "stone", "stone"},
		{"shears", "axe", "weeping_vines", "weeping_vines", "air", "air"},
	} {
		t.Run(tc.item+"/"+tc.transformer+"/"+tc.before+"/"+tc.above, func(t *testing.T) {
			srv.GiveSlot("Transformer", player.HotbarSlot, protocol.ItemStack{})
			c.ChatCommand(`give @s ` + tc.item + `[block_transformer="minecraft:` + tc.transformer + `"]`)
			c.Settle()
			if given := srv.InventorySlot("Transformer", player.HotbarSlot); given.Empty() {
				t.Fatal("/give did not supply the transformer stack")
			}
			before := servertest.BlockState(t, "minecraft:"+tc.before)
			after := servertest.BlockState(t, "minecraft:"+tc.after)
			if tc.item == "shears" && tc.before == "weeping_vines" {
				before = servertest.BlockStateWith(t, "minecraft:weeping_vines", "age=0")
				after = servertest.BlockStateWith(t, "minecraft:weeping_vines", "age=25")
			}
			srv.SetBlock(3, 63, 0, before)
			srv.SetBlock(3, 64, 0, servertest.BlockState(t, "minecraft:"+tc.above))
			c.UseBlock(3, 63, 0)
			c.Settle()
			if got := srv.BlockAt(3, 63, 0); got != after {
				t.Errorf("clicked block = %d, want %d (%s)", got, after, tc.after)
			}
			if got, want := srv.BlockAt(3, 64, 0), servertest.BlockState(t, "minecraft:"+tc.wantAbove); got != want {
				t.Errorf("block above = %d, want %d (%s)", got, want, tc.wantAbove)
			}
		})
	}
	t.Run("placement obstructed by player", func(t *testing.T) {
		srv.SetBlock(3, 63, 0, servertest.BlockState(t, "minecraft:oak_log"))
		srv.SetBlock(3, 64, 0, 0)
		c.MoveToGround(3.5, 64, 0.5, true)
		srv.GiveSlot("Transformer", player.HotbarSlot, protocol.ItemStack{})
		c.ChatCommand(`give @s dirt[block_transformer="minecraft:axe"]`)
		c.Settle()
		c.UseBlock(3, 63, 0)
		c.Settle()
		servertest.ExpectBlockNamed(t, srv, 3, 63, 0, "minecraft:stripped_oak_log", nil)
		servertest.ExpectBlockNamed(t, srv, 3, 64, 0, "minecraft:air", nil)
	})
}

// vanilla: BoneMealItem.useOn, GrassBlock.performBonemeal
func TestBlockTransformerBoneMealGrowsWithoutTilling(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))
	servertest.HoldTicks(t, srv)
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	grass := servertest.BlockState(t, "minecraft:grass_block")
	for x := int32(2); x <= 4; x++ {
		for z := int32(-1); z <= 1; z++ {
			srv.SetBlock(x, 63, z, grass)
			srv.SetBlock(x, 64, z, 0)
		}
	}
	c.ChatCommand(`give @s bone_meal[block_transformer="minecraft:hoe"]`)
	c.Settle()
	c.UseBlock(3, 63, 0)
	c.Settle()
	if got := srv.BlockAt(3, 63, 0); got != grass {
		t.Errorf("clicked block = %d, want grass %d", got, grass)
	}
	if block := registry.BlockOf(srv.BlockAt(3, 64, 0)); block != registry.BlockShortGrass && block != registry.BlockTallGrass &&
		!block.InTag(registry.BlockTagOf("minecraft:flowers")) {
		t.Errorf("block above the clicked grass = %v, want grass or a flower", block)
	}
}
