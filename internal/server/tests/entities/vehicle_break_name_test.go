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
