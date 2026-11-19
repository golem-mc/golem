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

// vanilla: ShearsDispenseItemBehavior.tryShearBeehive
func TestPackOverridesDispenserHiveHarvest(t *testing.T) {
	// Pack resources are process-wide, so this test must not be parallel.
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "harvests", "minecraft/loot_table/harvest/beehive.json",
		`{"type":"minecraft:block_interact","pools":[{"rolls":1,"conditions":[`+
			`{"condition":"minecraft:match_tool","predicate":{"items":"minecraft:shears"}},`+
			`{"condition":"minecraft:block_state_property","block":"minecraft:beehive","properties":{"honey_level":"5"}},`+
			`{"condition":"minecraft:inverted","term":{"condition":"minecraft:entity_properties","entity":"interacting_entity","predicate":{}}}`+
			`],"entries":[{"type":"minecraft:item","name":"minecraft:stick"}]}]}`)
	srv, c := servertest.Join(t, cfg, "Gardener")
	servertest.HoldTicks(t, srv)
	if !srv.PlaceContainer(3, 64, 0, "minecraft:dispenser") {
		t.Fatal("cannot place dispenser")
	}
	srv.SetBlock(3, 64, 0, servertest.BlockStateWith(t, "minecraft:dispenser", "facing=south,triggered=false"))
	srv.SetBlock(3, 64, 1, servertest.BlockStateWith(t, "minecraft:beehive", "facing=north,honey_level=5"))
	if !srv.SetInventoryAt(3, 64, 0, 0, servertest.ItemStack(t, "minecraft:shears", 1)) {
		t.Fatal("dispenser has no inventory")
	}
	srv.Dispense(3, 64, 0, protocol.FaceSouth)
	c.Settle()
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].Item != "minecraft:stick" || items[0].Count != 1 {
		t.Fatalf("dispenser harvest = %+v, want one stick", items)
	}
	if got := srv.InventoryAt(3, 64, 0)[0]; got.ItemID != int32(registry.ItemShears) || got.Damage != 1 {
		t.Errorf("dispenser tool = %+v, want shears worn once", got)
	}
}

// TestHarvestLootSeesTheInteractingPlayersTag: a pack beehive harvest table
// testing the interacting entity's entity_tags drops only for a tagged player.
// vanilla: Block.dropFromBlockInteractLootTable, EntityPredicate.matches
func TestHarvestLootSeesTheInteractingPlayersTag(t *testing.T) {
	// Pack resources are process-wide, so this test must not be parallel.
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "harvests", "minecraft/loot_table/harvest/beehive.json",
		`{"type":"minecraft:block_interact","pools":[{"rolls":1,"conditions":[`+
			`{"condition":"minecraft:entity_properties","entity":"interacting_entity","predicate":{"minecraft:entity_tags":{"any_of":["keeper"]}}}`+
			`],"entries":[{"type":"minecraft:item","name":"minecraft:stick"}]}]}`)
	srv, c := servertest.Join(t, cfg, "Gardener")
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Gardener", player.HotbarSlot, servertest.ItemStack(t, "minecraft:shears", 1))
	harvest := func(z int32) int32 {
		srv.SetBlock(3, 64, z, servertest.BlockStateWith(t, "minecraft:beehive", "facing=north,honey_level=5"))
		c.UseBlock(3, 64, z)
		c.Settle()
		var sticks int32
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.Item == "minecraft:stick" {
				sticks += it.Count
			}
		}
		return sticks
	}
	if got := harvest(0); got != 0 {
		t.Fatalf("an untagged player's harvest dropped %d sticks, want 0", got)
	}
	srv.RunLine("tag Gardener add keeper")
	if got := harvest(1); got != 1 {
		t.Errorf("a tagged player's harvest dropped %d sticks, want 1", got)
	}
}
