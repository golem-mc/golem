package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
)

// TestADriedSpongeFillsTheBucketInTheFuelSlot smelts a wet sponge beside an
// empty bucket in the fuel slot, then beside any other leftover fuel item.
// vanilla: AbstractFurnaceBlockEntity.burn
func TestADriedSpongeFillsTheBucketInTheFuelSlot(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, fuel, want string
		count            int32
	}{
		{"empty bucket", "minecraft:lava_bucket", "minecraft:water_bucket", 1},
		{"other fuel", "minecraft:coal_block", "minecraft:coal_block", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
			srv.StopTick()
			if !srv.PlaceContainer(4, 64, 0, "minecraft:furnace") {
				t.Fatal("the furnace would not stand up")
			}
			srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:wet_sponge", 1))
			// A lava bucket leaves an empty bucket once lit; a stack of two coal
			// blocks leaves one.
			srv.SetInventoryAt(4, 64, 0, 1, servertest.ItemStack(t, tc.fuel, max(tc.count, 1)))
			for range 250 {
				srv.TickBlockEntities()
			}
			slots := srv.InventoryAt(4, 64, 0)
			if got := slots[2]; !got.Equal(servertest.ItemStack(t, "minecraft:sponge", 1)) {
				t.Fatalf("output = %+v, want one dry sponge", got)
			}
			if got := slots[1]; !got.Equal(servertest.ItemStack(t, tc.want, 1)) {
				t.Errorf("fuel slot = %+v, want %s", got, tc.want)
			}
		})
	}
}
