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

// TestADispenserWaterFacingItsOwnSourceEvaporatesInTheNether: the nether's
// evaporation check runs before the same-source early out, so the hiss
// sounds instead of the bucket's empty sound.
// vanilla: BucketItem.emptyContents
func TestADispenserWaterFacingItsOwnSourceEvaporatesInTheNether(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Watcher")
	servertest.CarryIntoNether(t, srv, c, "Watcher", 8, 10)

	if !srv.PlaceContainerInDimension(world.NetherName, 8, 40, 8, "minecraft:dispenser") {
		t.Fatal("the nether did not take the dispenser")
	}
	stack := protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:water_bucket"), Count: 1}
	if !srv.SetInventoryInDimension(world.NetherName, 8, 40, 8, 0, stack) {
		t.Fatal("the dispenser took no water bucket")
	}
	water := registry.MustState("minecraft:water", map[string]string{"level": "0"})
	srv.SetBlockInDimension(world.NetherName, 8, 40, 9, water)
	c.Settle()

	srv.DispenseInDimension(world.NetherName, 8, 40, 8, protocol.FaceSouth)

	extinguish, _ := registry.SoundEventID("minecraft:block.fire.extinguish")
	empty, _ := registry.SoundEventID("minecraft:item.bucket.empty")
	heard := servertest.CollectBucketPourEffects(c)
	if n := servertest.CountSound(heard, extinguish); n != 1 {
		t.Errorf("extinguish hisses = %d, want one; sounds %v", n, heard.Sounds)
	}
	if n := servertest.CountSound(heard, empty); n != 0 {
		t.Errorf("bucket empty sounds = %d, want none", n)
	}
	if got := srv.BlockInDimension(world.NetherName, 8, 40, 9); got != water {
		t.Errorf("the source after the dispense = %d, want %d", got, water)
	}
	c.Disconnect()
}
