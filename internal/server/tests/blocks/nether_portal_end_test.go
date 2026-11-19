package blocks

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestANetherPortalInTheEndLeadsToTheNether checks that a creative player
// standing in a nether portal block in the end crosses to the nether.
// vanilla: NetherPortalBlock.getPortalDestination
func TestANetherPortalInTheEndLeadsToTheNether(t *testing.T) {
	t.Parallel()

	portal := registry.MustState("minecraft:nether_portal", map[string]string{"axis": "x"})
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Voyager", servertest.OpsListing(t, "Voyager"))

	srv.MovePlayerToDimension("Voyager", world.EndName, 80.5, 70, -39.5)
	servertest.AwaitViewSent(t, c)
	servertest.UntilTicks(t, srv, 400, "arrival in the end", func() bool {
		return srv.PlayerDimension("Voyager") == world.EndName && srv.ChunkLoadedInDimension(world.EndName, 80, -40)
	})
	for _, y := range []int32{70, 71} {
		if !srv.SetBlockInDimension(world.EndName, 80, y, -40, portal) {
			t.Fatalf("placing the portal block at y=%d failed", y)
		}
	}

	servertest.UntilTicks(t, srv, 400, "crossing to the nether", func() bool {
		return srv.PlayerDimension("Voyager") == world.NetherName
	})

	// The end's scale is the overworld's, so (80, -40) maps to nether (10, -5);
	// a missing /8 would land near x=80. A portal is built or reused within
	// the 16-block search radius of that point.
	// vanilla: NetherPortalBlock.getPortalDestination, PortalForcer.findClosestPortalPosition
	pos := srv.PlayerPosition("Voyager")
	if math.Abs(pos.X-10) > 16 || math.Abs(pos.Z+5) > 16 {
		t.Errorf("arrived at (%.1f, %.1f), want within 16 blocks of nether (10, -5)", pos.X, pos.Z)
	}
}
