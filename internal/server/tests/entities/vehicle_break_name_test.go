package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestABrokenBoatDropsItsItemWithTheVehiclesCustomName: the dropped boat
// item carries the custom_name the hull was summoned with.
// vanilla: VehicleEntity.destroy
func TestABrokenBoatDropsItsItemWithTheVehiclesCustomName(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.BoatPlayer(t, "survival", "Breaker", 0)
	srv.StopTick()
	srv.RunLine(`summon minecraft:oak_boat 4.5 64 4.5 {CustomName:"Bob"}`)
	ids := srv.BoatIDs()
	if len(ids) != 1 {
		t.Fatalf("%d boats stand after the summon, want 1", len(ids))
	}
	srv.DamageBoat(ids[0], 1000)
	srv.TickBoats(1)
	assertDropNamed(t, srv, "minecraft:oak_boat")
}

// TestABrokenMinecartDropsItsItemWithTheVehiclesCustomName: the dropped cart
// item carries the cart's custom_name.
// vanilla: VehicleEntity.destroy
func TestABrokenMinecartDropsItsItemWithTheVehiclesCustomName(t *testing.T) {
	t.Parallel()
	srv, _, _ := cartPlayer(t, "survival", "Breaker", 1)
	srv.StopTick()
	cartID := srv.SpawnMinecart(4.5, 64.0625, 1.5)
	srv.RunLine(`data merge entity @e[type=minecraft:minecart,limit=1] {CustomName:"Bob"}`)
	if !srv.HurtMinecart(cartID, 100, false) {
		t.Fatal("the blow did not land")
	}
	srv.TickMinecarts(1)
	assertDropNamed(t, srv, "minecraft:minecart")
}

func assertDropNamed(t *testing.T, srv *server.Server, item string) {
	t.Helper()
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item != item {
			continue
		}
		if it.Stack.CustomName == nil || it.Stack.CustomName.Text != "Bob" {
			t.Fatalf("the dropped %s's name = %v, want Bob", item, it.Stack.CustomName)
		}
		return
	}
	t.Fatalf("no %s was dropped", item)
}
