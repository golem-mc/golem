package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// countOf sums the item entities of the overworld holding the item.
func countOf(items []server.TestItem, id int32) int32 {
	var n int32
	for _, it := range items {
		if it.Stack.ItemID == id {
			n += it.Count
		}
	}
	return n
}

// spillConfig is the block test world with only the spawn chunk in view.
func spillConfig() *config.Config {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	return cfg
}

// TestAShulkerBoxBurnedInLavaSpillsItsContents: a destroyed container item
// lays its stacks down where it burned. vanilla: BlockItem.onDestroyed
func TestAShulkerBoxBurnedInLavaSpillsItsContents(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, spillConfig(), "Burner")
	servertest.HoldTicks(t, srv)

	ingot := servertest.MustItemID(t, "minecraft:netherite_ingot")
	box := servertest.MustItemID(t, "minecraft:shulker_box")
	if !srv.SetBlock(4, 64, 4, registry.MustState("minecraft:lava", nil)) {
		t.Fatal("the world did not take the lava")
	}
	srv.SpawnItemStack(4.5, 64.5, 4.5, protocol.ItemStack{ItemID: box, Count: 1,
		Container: []protocol.ItemStack{{ItemID: ingot, Count: 5}}})

	servertest.UntilTicks(t, srv, 40, "the ingots spilling", func() bool {
		return countOf(srv.ItemsInDimension("minecraft:overworld"), ingot) == 5
	})
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Stack.ItemID == box {
			t.Errorf("the shulker box survived the lava: %+v", it)
		}
		if it.Stack.ItemID == ingot && it.PickupDelay != 0 {
			t.Errorf("a spilled ingot has pickup delay %d, want 0", it.PickupDelay)
		}
		if it.Stack.ItemID == ingot && (it.X < 3 || it.X > 6 || it.Z < 3 || it.Z > 6) {
			t.Errorf("the ingots spilled at (%v %v %v), want near the lava", it.X, it.Y, it.Z)
		}
	}
}
