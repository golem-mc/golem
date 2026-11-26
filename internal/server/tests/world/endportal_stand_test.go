package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestEndPortalCarriesAStandToTheVanillaSpawnPoint: a non-player arrives at
// END_SPAWN_POINT (y=50), a block above where a player lands.
// vanilla: EndPortalBlock.getPortalDestination
func TestEndPortalCarriesAStandToTheVanillaSpawnPoint(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, _ := servertest.Join(t, cfg, "Stander")
	servertest.HoldTicks(t, srv)
	endPortal := srv.EndPortalState()
	if !srv.SetBlock(10, servertest.PortalFloorY-1, 10, endPortal) {
		t.Fatal("placing the end portal failed")
	}
	srv.SpawnArmorStand(10.5, servertest.PortalFloorY-1, 10.5)
	srv.TickArmorStands(1)
	stands := srv.ArmorStandsInDimension(world.EndName)
	if len(stands) != 1 {
		t.Fatalf("stands in the end = %d, want 1", len(stands))
	}
	arrival := world.EndArrival()
	if s := stands[0]; s.X != float64(arrival.X)+0.5 || s.Y != float64(arrival.Y)+1 || s.Z != float64(arrival.Z)+0.5 {
		t.Errorf("stand arrived at (%v, %v, %v), want (%v, %v, %v)", s.X, s.Y, s.Z,
			float64(arrival.X)+0.5, arrival.Y+1, float64(arrival.Z)+0.5)
	}
}
