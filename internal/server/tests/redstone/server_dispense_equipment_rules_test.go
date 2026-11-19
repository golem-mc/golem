package redstone

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestADispenserKeepsAHeadPieceNobodyWears checks a carved pumpkin or wither
// skull with no wearer stays in the dispenser with the fail click, where an
// iron helmet is dropped.
// vanilla: DispenseItemBehavior (carved pumpkin, wither skeleton skull)
func TestADispenserKeepsAHeadPieceNobodyWears(t *testing.T) {
	t.Parallel()
	for _, item := range []string{"minecraft:carved_pumpkin", "minecraft:wither_skeleton_skull"} {
		t.Run(item, func(t *testing.T) {
			t.Parallel()
			srv, items, events := equipDispense(t, item, nil)
			if got := srv.InventoryAt(4, 64, 0)[0]; got.ItemID != registry.MustItem(item) || got.Count != 1 {
				t.Errorf("the dispenser holds %+v, want the item kept", got)
			}
			if n := countDropped(items, item); n != 0 {
				t.Errorf("%d dropped, want 0", n)
			}
			if !slices.Contains(events, protocol.WorldEventDispenseFail) || slices.Contains(events, protocol.WorldEventDispense) {
				t.Errorf("events %v, want the fail click alone", events)
			}
		})
	}
	var zombie int32
	srv, _, _ := equipDispense(t, "minecraft:carved_pumpkin", func(srv *server.Server, _ *servertest.WindowedClient) {
		zombie = srv.SpawnMob("minecraft:zombie", 4.5, 64, 1.5)
		srv.SetMobCanPickUpLoot(zombie)
	})
	if got := srv.MobEquipment(zombie, "head"); got.ItemID != registry.MustItem("minecraft:carved_pumpkin") {
		t.Errorf("the zombie wears %+v, want the pumpkin", got)
	}
}
