package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestOpeningAContainerCountsItsStatistic: the right-click that opens a
// container block adds one to that block's custom statistic.
// vanilla: ChestBlock.useWithoutItem, BarrelBlock.useWithoutItem,
// CraftingTableBlock.useWithoutItem
func TestOpeningAContainerCountsItsStatistic(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ block, stat string }{
		{"minecraft:chest", "minecraft:open_chest"},
		{"minecraft:trapped_chest", "minecraft:trigger_trapped_chest"},
		{"minecraft:barrel", "minecraft:open_barrel"},
		{"minecraft:ender_chest", "minecraft:open_enderchest"},
		{"minecraft:shulker_box", "minecraft:open_shulker_box"},
		{"minecraft:dispenser", "minecraft:inspect_dispenser"},
		{"minecraft:dropper", "minecraft:inspect_dropper"},
		{"minecraft:hopper", "minecraft:inspect_hopper"},
		{"minecraft:crafting_table", "minecraft:interact_with_crafting_table"},
		{"minecraft:furnace", "minecraft:interact_with_furnace"},
		{"minecraft:blast_furnace", "minecraft:interact_with_blast_furnace"},
		{"minecraft:smoker", "minecraft:interact_with_smoker"},
		{"minecraft:brewing_stand", "minecraft:interact_with_brewingstand"},
		{"minecraft:anvil", "minecraft:interact_with_anvil"},
		{"minecraft:stonecutter", "minecraft:interact_with_stonecutter"},
		{"minecraft:smithing_table", "minecraft:interact_with_smithing_table"},
		{"minecraft:grindstone", "minecraft:interact_with_grindstone"},
		{"minecraft:loom", "minecraft:interact_with_loom"},
		{"minecraft:cartography_table", "minecraft:interact_with_cartography_table"},
		{"minecraft:beacon", "minecraft:interact_with_beacon"},
	} {
		t.Run(tc.block, func(t *testing.T) {
			t.Parallel()
			srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
			srv.SetBlock(4, 64, 0, registry.MustState(tc.block, nil))
			w.UseBlock(4, 64, 0)
			w.Settle()

			if got := srv.StatValue("Testificate", registry.StatCustom, tc.stat); got != 1 {
				t.Errorf("%s = %d after opening %s, want 1", tc.stat, got, tc.block)
			}
		})
	}
}
