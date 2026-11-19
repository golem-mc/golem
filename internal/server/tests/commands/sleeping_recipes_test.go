package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestReloadWakesCookingBlocksWithUnchangedContents(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := packWorld(t)
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, w := servertest.OpenWindowedOps(t, cfg)
	servertest.HoldTicks(t, srv)
	srv.PlaceContainer(4, 64, 0, "minecraft:furnace")
	srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:clay_ball", 1))
	srv.SetInventoryAt(4, 64, 0, 1, servertest.ItemStack(t, "minecraft:coal", 1))
	// Block the built-in clay-to-brick recipe, then replace it on reload.
	srv.SetInventoryAt(4, 64, 0, 2, servertest.ItemStack(t, "minecraft:diamond", 1))
	servertest.TickBlockEntitiesByHand(srv, 5)
	be := srv.OverworldWorld().BlockEntity(4, 64, 0)
	if !be.Sleeping() {
		t.Fatal("blocked furnace did not sleep")
	}
	recipeFile(t, cfg.LevelName, "minecraft:brick", `{"type":"minecraft:smelting","ingredient":"minecraft:clay_ball","result":"minecraft:diamond","cookingtime":2}`)
	w.ChatCommand("reload")
	w.SettleAndCollect(func(id int32, data []byte) { w.Record(id, data) })
	if be.Sleeping() {
		t.Fatal("reload did not wake the furnace")
	}
	servertest.TickBlockEntitiesByHand(srv, 2)
	if got := srv.InventoryAt(4, 64, 0)[2]; !got.Equal(servertest.ItemStack(t, "minecraft:diamond", 2)) {
		t.Fatalf("output after recipe reload = %+v, want two diamonds", got)
	}
}
