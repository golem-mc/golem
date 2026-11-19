package blocks

import (
	"math"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// putPortalAt fills the blocks at (x, y..y+1, z) of dimension with a portal.
func putPortalAt(t *testing.T, srv *server.Server, dimension string, x, y, z int32) {
	t.Helper()
	portal := registry.MustState("minecraft:nether_portal", map[string]string{"axis": "x"})
	for _, dy := range []int32{0, 1} {
		if !srv.SetBlockInDimension(dimension, x, y+dy, z, portal) &&
			srv.LoadBlockInDimension(dimension, x, y+dy, z) != portal {
			t.Fatalf("placing the portal block at y=%d failed", y+dy)
		}
	}
}

// clearPortalAt empties the cells putPortalAt filled.
func clearPortalAt(t *testing.T, srv *server.Server, dimension string, x, y, z int32) {
	t.Helper()
	for _, dy := range []int32{0, 1} {
		if !srv.SetBlockInDimension(dimension, x, y+dy, z, 0) {
			t.Fatalf("clearing the portal block at y=%d failed", y+dy)
		}
	}
}

// TestAllowEnteringNetherUsingPortalsRefusesTheNether checks that with the
// rule off a player in an overworld portal stays put and one in a nether
// portal still arrives in the overworld, and that turning the rule back on
// lets the first one through.
// vanilla: ServerLevel.isAllowedToEnterPortal
func TestAllowEnteringNetherUsingPortalsRefusesTheNether(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Voyager")
	servertest.HoldTicks(t, srv)
	srv.SetGameRule(world.RuleAllowEnteringNether.String(), false)

	pos := srv.PlayerPosition("Voyager")
	putPortalAt(t, srv, world.DimensionName, int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z)))
	servertest.RunTicks(t, srv, 5)
	// The player's trip runs on its own goroutine, off the tick: wait for
	// the server to have decided it.
	servertest.PollUntil(t, 20*time.Second, func() bool { return srv.PortalTripsDecided() == 1 })
	if got := srv.PlayerDimension("Voyager"); got != world.DimensionName {
		t.Fatalf("with the rule off the player is in %s, want to stay in the overworld", got)
	}

	// A refused trip still spends the portal cooldown, which standing in the
	// portal keeps restarting: step out for the 300 ticks it lasts, then in.
	srv.SetGameRule(world.RuleAllowEnteringNether.String(), true)
	clearPortalAt(t, srv, world.DimensionName, int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z)))
	servertest.RunTicks(t, srv, 320)
	putPortalAt(t, srv, world.DimensionName, int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z)))
	servertest.RunTicks(t, srv, 5)
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn arrived for the nether crossing")
	}
	if got := srv.PlayerDimension("Voyager"); got != world.NetherName {
		t.Fatalf("the player is in %s, want the nether", got)
	}
}
