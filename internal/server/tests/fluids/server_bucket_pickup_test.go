package fluids

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// pickupCase is a block an empty bucket can take something from, the block
// it leaves behind and the bucket it fills.
type pickupCase struct {
	name   string
	before uint16
	after  uint16
	item   string
}

func pickupCases(t *testing.T) []pickupCase {
	t.Helper()
	wet, ok := registry.FindState("minecraft:oak_slab", map[string]string{"type": "bottom", "waterlogged": "true"})
	dry, ok2 := registry.FindState("minecraft:oak_slab", map[string]string{"type": "bottom", "waterlogged": "false"})
	if !ok || !ok2 {
		t.Fatal("the block registry has no oak slab")
	}
	return []pickupCase{
		{"waterlogged slab", wet, dry, "minecraft:water_bucket"},
		{"bubble column", registry.MustState("minecraft:bubble_column", map[string]string{"drag": "true"}), 0, "minecraft:water_bucket"},
		{"powder snow", registry.MustState("minecraft:powder_snow", nil), 0, "minecraft:powder_snow_bucket"},
	}
}

// TestADispenserEmptyBucketPicksUpWhatItFaces: the empty bucket takes
// waterlogged water, a bubble column's water or powder snow from the cell
// the face opens onto and leaves the matching filled bucket.
// vanilla: DispenseItemBehavior (Items.BUCKET), BucketPickup.pickupBlock
func TestADispenserEmptyBucketPicksUpWhatItFaces(t *testing.T) {
	t.Parallel()
	for _, tc := range pickupCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			srv, c := servertest.Join(t, cfg, "Watcher")

			if !srv.PlaceContainer(8, 70, 8, "minecraft:dispenser") {
				t.Fatal("the world did not take the dispenser")
			}
			if !srv.SetInventoryAt(8, 70, 8, 0, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:bucket"), Count: 1}) {
				t.Fatal("the dispenser took no bucket")
			}
			srv.SetBlock(8, 70, 9, tc.before)
			c.Settle()

			srv.Dispense(8, 70, 8, protocol.FaceSouth)

			if got := srv.BlockAt(8, 70, 9); got != tc.after {
				t.Errorf("the cell after the dispense = %d, want %d", got, tc.after)
			}
			slots := srv.InventoryAt(8, 70, 8)
			if len(slots) == 0 || slots[0].ItemID != servertest.ItemIDOrDie(t, tc.item) || slots[0].Count != 1 {
				t.Errorf("the dispenser's slot = %+v, want %s", slots, tc.item)
			}
			c.Disconnect()
		})
	}
}

// TestAPlayerEmptyBucketPicksUpWhatItAimsAt: the same pickups on Use Item.
// vanilla: BucketItem.use, BucketPickup.pickupBlock
func TestAPlayerEmptyBucketPicksUpWhatItAimsAt(t *testing.T) {
	t.Parallel()
	for _, tc := range pickupCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			srv, c := servertest.Join(t, cfg, "Scooper")

			srv.GiveSlot("Scooper", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:bucket"), Count: 1})
			if !srv.SetBlock(4, 63, 4, tc.before) {
				t.Fatal("the world did not take the block")
			}
			c.Settle()
			c.MoveStep(4.5, 64, 4.5, true)

			servertest.UseHeld(c, 0, 90)
			got := c.ExpectBucketUseResult()
			if got.Update.State != tc.after {
				t.Errorf("the block after the scoop = %d, want %d", got.Update.State, tc.after)
			}
			if want := servertest.ItemIDOrDie(t, tc.item); got.Slot.ItemID != want || got.Slot.Count != 1 {
				t.Errorf("the hand after the scoop = %+v, want one %s", got.Slot, tc.item)
			}
			if state := srv.BlockAt(4, 63, 4); state != tc.after {
				t.Errorf("the server's block after the scoop = %d, want %d", state, tc.after)
			}
		})
	}
}
