package fluids

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestADispenserBucketFacingItsOwnSourceEmptiesIntoIt: a filled bucket
// facing the same fluid's source keeps an empty bucket, drops nothing,
// plays the empty sound and leaves the block as it was.
// vanilla: BucketItem.emptyContents
func TestADispenserBucketFacingItsOwnSourceEmptiesIntoIt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, fluid, item, sound string
	}{
		{"water", "minecraft:water", "minecraft:water_bucket", "minecraft:item.bucket.empty"},
		{"lava", "minecraft:lava", "minecraft:lava_bucket", "minecraft:item.bucket.empty_lava"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			srv, c := servertest.Join(t, cfg, "Watcher")

			if !srv.PlaceContainer(8, 70, 8, "minecraft:dispenser") {
				t.Fatal("the world did not take the dispenser")
			}
			if !srv.SetInventoryAt(8, 70, 8, 0, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, tc.item), Count: 1}) {
				t.Fatal("the dispenser took no bucket")
			}
			source := registry.MustState(tc.fluid, map[string]string{"level": "0"})
			srv.SetBlock(8, 70, 9, source)
			c.Settle()

			srv.Dispense(8, 70, 8, protocol.FaceSouth)

			if got := srv.BlockAt(8, 70, 9); got != source {
				t.Errorf("the source after the dispense = %d, want %d", got, source)
			}
			slots := srv.InventoryAt(8, 70, 8)
			if len(slots) == 0 || slots[0].ItemID != servertest.ItemIDOrDie(t, "minecraft:bucket") || slots[0].Count != 1 {
				t.Errorf("the dispenser's slot = %+v, want an empty bucket", slots)
			}
			if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
				t.Errorf("the pour dropped %d items, want none", len(items))
			}
			servertest.ExpectSoundEvent(t, c, tc.sound)
			c.Disconnect()
		})
	}
}
