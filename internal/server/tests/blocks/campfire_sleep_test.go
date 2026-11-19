package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: CampfireBlockEntity.cooldownTick, cookTick
func TestACooledCampfireWakesWhenLit(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, _ := servertest.Join(t, cfg, "Tester")
	servertest.HoldTicks(t, srv)
	srv.SetBlock(3, 64, 0, campfireState(t, "minecraft:campfire", false))
	srv.SetBlockEntityTagAt(3, 64, 0, "minecraft:campfire", nbt.Compound{
		"CookingTimes": nbt.IntArray{6, 0, 0, 0}, "CookingTotalTimes": nbt.IntArray{10, 0, 0, 0},
	})
	srv.SetInventoryAt(3, 64, 0, 0, servertest.ItemStack(t, "minecraft:beef", 1))
	servertest.TickBlockEntitiesByHand(srv, 5)
	be := srv.OverworldWorld().BlockEntity(3, 64, 0)
	if !be.Sleeping() {
		t.Fatal("cooled campfire did not sleep")
	}
	// Inspect the cooled progress only after checking sleep: Use wakes it.
	be.Use(func(b *world.BlockEntity) {
		if b.CampfireProgress[0] != 0 {
			t.Fatalf("cooled progress = %d, want 0", b.CampfireProgress[0])
		}
	})
	servertest.TickBlockEntitiesByHand(srv, 2)
	srv.SetBlock(3, 64, 0, campfireState(t, "minecraft:campfire", true))
	servertest.TickBlockEntitiesByHand(srv, 9)
	if got := countItems(srv, "minecraft:cooked_beef"); got != 0 {
		t.Fatal("campfire finished before its cook time")
	}
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := countItems(srv, "minecraft:cooked_beef"); got != 1 {
		t.Fatalf("lit campfire dropped %d steaks, want 1", got)
	}
}
