package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestEatingUsesTheStacksRemainder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, give string
		item       registry.Item
		count      int32
	}{
		{"override", `minecraft:apple[use_remainder={id:"minecraft:stick",count:2}] 1`, registry.ItemStick, 2},
		{"removed", `minecraft:mushroom_stew[!use_remainder]`, registry.ItemAir, 0},
		{"default", `minecraft:mushroom_stew`, registry.ItemBowl, 1},
		{"own consumable", `minecraft:mushroom_stew[consumable={}]`, registry.ItemBowl, 1},
		{"invalid creation", `minecraft:apple[use_remainder={id:"minecraft:stick",count:65}]`, registry.ItemAir, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := consumableWorld(t, tc.give)
			if !hotbar(srv) {
				t.Fatal("/give put nothing in the hotbar")
			}
			c.UseItemRightClicks(0)
			c.Settle()
			if !srv.Eating("Eater") {
				t.Fatal("using the food starts no meal")
			}
			servertest.RunTicks(t, srv, 35)
			got := srv.InventorySlot("Eater", player.HotbarSlot)
			if got.Count != tc.count || tc.count > 0 && registry.Item(got.ItemID) != tc.item {
				t.Fatalf("held after eating = %+v, want %d of %v", got, tc.count, tc.item)
			}
			if srv.Eating("Eater") {
				t.Fatal("meal did not finish")
			}
		})
	}
}

func TestUseRemainderGoesToInventoryOrDropsWithComponents(t *testing.T) {
	t.Parallel()
	for _, full := range []bool{false, true} {
		t.Run(map[bool]string{false: "inventory", true: "drop"}[full], func(t *testing.T) {
			srv, c := consumableWorld(t, `apple[use_remainder={id:"minecraft:stick",count:2,components:{"minecraft:custom_name":"Leftovers"}}] 2`)
			if full {
				for slot := player.MainSlot; slot < player.OffHandSlot; slot++ {
					if slot != player.HotbarSlot {
						srv.GiveSlot("Eater", slot, protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 64})
					}
				}
			}
			c.UseItemRightClicks(0)
			c.Settle()
			servertest.RunTicks(t, srv, 35)
			if got := srv.InventorySlot("Eater", player.HotbarSlot); registry.Item(got.ItemID) != registry.ItemApple || got.Count != 1 {
				t.Fatalf("remaining food = %+v", got)
			}
			got := srv.InventorySlot("Eater", player.HotbarSlot+1)
			if full {
				items := srv.ItemsInDimension(world.DimensionName)
				if len(items) != 1 {
					t.Fatalf("dropped remainders = %+v", items)
				}
				got = items[0].Stack
			}
			if registry.Item(got.ItemID) != registry.ItemStick || got.Count != 2 || got.CustomName == nil || got.CustomName.Plain() != "Leftovers" {
				t.Fatalf("extra remainder = %+v", got)
			}
		})
	}
}
