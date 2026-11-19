package tools

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// vanilla: BeehiveBlock.dropHoneycomb, BlockTransformer.transformBlock
func TestPackOverridesHiveAndRootedDirtHarvests(t *testing.T) {
	// Pack resources are process-wide, so this test must not be parallel.
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	for _, harvest := range []struct{ table, block, props, tool string }{
		{"harvest/beehive", "beehive", `,"properties":{"honey_level":"5"}`, "shears"},
		{"till/rooted_dirt", "rooted_dirt", "", "iron_hoe"},
	} {
		servertest.WritePackFile(t, cfg, "harvests", "minecraft/loot_table/"+harvest.table+".json",
			`{"type":"minecraft:block_interact","pools":[{"rolls":1,"conditions":[`+
				`{"condition":"minecraft:block_state_property","block":"minecraft:`+harvest.block+`"`+harvest.props+`},`+
				`{"condition":"minecraft:match_tool","predicate":{"items":"minecraft:`+harvest.tool+`"}},`+
				`{"condition":"minecraft:entity_properties","entity":"interacting_entity","predicate":{"minecraft:entity_type":"minecraft:player"}}`+
				`],"entries":[{"type":"minecraft:item","name":"minecraft:stick"}]}]}`)
	}
	srv, c := servertest.Join(t, cfg, "Gardener")
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Gardener", player.HotbarSlot, servertest.ItemStack(t, "minecraft:shears", 1))
	srv.SetBlock(3, 64, 0, servertest.BlockStateWith(t, "minecraft:beehive", "facing=north,honey_level=5"))
	c.UseBlock(3, 64, 0)
	c.Settle()
	if got := srv.BlockAt(3, 64, 0); got != registry.BlockBeehive.DefaultState() {
		t.Errorf("sheared hive = %d, want empty hive", got)
	}
	srv.GiveSlot("Gardener", player.HotbarSlot, servertest.ItemStack(t, "minecraft:iron_hoe", 1))
	srv.SetBlock(3, 64, 1, registry.BlockRootedDirt.DefaultState())
	c.UseBlockSeq(3, 64, 1, protocol.FaceNorth, 1)
	c.Settle()
	if got := srv.BlockAt(3, 64, 1); got != registry.BlockDirt.DefaultState() {
		t.Errorf("tilled block = %d, want dirt", got)
	}
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 2 {
		t.Fatalf("harvests = %+v, want two stick entities", items)
	}
	for _, item := range items {
		if item.Item != "minecraft:stick" || item.Count != 1 {
			t.Errorf("harvest = %+v, want a stick and no honeycomb or hanging roots", item)
		}
	}
}
