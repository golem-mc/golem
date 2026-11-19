package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: AbstractFurnaceBlockEntity.serverTick, setItem, loadAdditional
func TestIdleCookingBlocksWakeForFuelAndLoadedProgress(t *testing.T) {
	for _, tc := range []struct {
		block, input, output string
		cookTicks            int
	}{
		{"furnace", "raw_iron", "iron_ingot", 200},
		{"blast_furnace", "raw_iron", "iron_ingot", 100},
		{"smoker", "beef", "cooked_beef", 100},
	} {
		t.Run(tc.block, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, _ := servertest.Join(t, cfg, "Tester")
			servertest.HoldTicks(t, srv)
			srv.PlaceContainer(4, 64, 0, "minecraft:"+tc.block)
			srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:"+tc.input, 1))
			servertest.TickBlockEntitiesByHand(srv, 5)
			be := srv.OverworldWorld().BlockEntity(4, 64, 0)
			if !be.Sleeping() {
				t.Fatal("unfuelled cooking block did not sleep")
			}
			// An oak door fuels exactly one recipe (200 ticks, halved in fast furnaces).
			srv.SetInventoryAt(4, 64, 0, 1, servertest.ItemStack(t, "minecraft:oak_door", 1))
			servertest.TickBlockEntitiesByHand(srv, tc.cookTicks)
			if got := srv.InventoryAt(4, 64, 0)[2]; !got.Equal(servertest.ItemStack(t, "minecraft:"+tc.output, 1)) {
				t.Fatalf("output after adding fuel = %+v", got)
			}
			// One turn extinguishes the block; the next can sleep after that write's wake.
			servertest.TickBlockEntitiesByHand(srv, 2)
			if !be.Sleeping() {
				t.Fatal("spent cooking block did not return to sleep")
			}
			srv.SetBlockEntityTagAt(4, 64, 0, "minecraft:"+tc.block, nbt.Compound{
				"cooking_time_spent": nbt.Int(6), "cooking_total_time": nbt.Int(200),
			})
			servertest.TickBlockEntitiesByHand(srv, 3)
			if spent, _, _, _, _ := srv.FurnaceAt(4, 64, 0); spent != 0 {
				t.Fatalf("loaded progress after cooling = %d, want 0", spent)
			}
			if registry.StateBool(srv.OverworldWorld().BlockAt(4, 64, 0), registry.PropLit) {
				t.Fatal("spent cooking block stayed lit")
			}
		})
	}
}
