package fluids

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: FlowingFluid.tick, LavaFluid.getSpreadDelay, LiquidBlock.onPlace, LevelChunkTicks.schedule
func TestDeepeningLavaKeepsItsPlacementDelay(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		seed int64
	}{
		{"normal delay roll", 3},   // First Intn(4) is 0.
		{"fourfold delay roll", 1}, // First Intn(4) is 1.
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor",
				servertest.OpsListing(t, "Actor"), func(srv *server.Server) {
					servertest.FreezeUntilHeld(t, srv)
					srv.SetGameRuleValue("random_tick_speed", 0)
				})
			servertest.HoldTicks(t, srv)
			servertest.RunTicks(t, srv, 1)
			servertest.Setblock(t, c, 8, 64, 7, "stone")
			servertest.Setblock(t, c, 8, 64, 9, "stone")
			servertest.Setblock(t, c, 9, 64, 8, "stone")
			servertest.Setblock(t, c, 7, 64, 8, "lava strict")
			servertest.Setblock(t, c, 8, 64, 8, "lava[level=4]")
			srv.SetPhysicsSeed(tc.seed)

			assertLevel := func(want string, tick int) {
				t.Helper()
				name, props := servertest.StateProps(srv.BlockAt(8, 64, 8))
				if name != "minecraft:lava" || props["level"] != want {
					t.Fatalf("tick %d: got %s %v, want lava[level=%s]", tick, name, props, want)
				}
			}
			servertest.RunTicks(t, srv, 29)
			assertLevel("4", 29)
			servertest.RunTicks(t, srv, 1)
			assertLevel("2", 30)

			// Strict removal leaves the test cell's existing booking alone.
			servertest.Setblock(t, c, 7, 64, 8, "air strict")
			servertest.RunTicks(t, srv, 29)
			assertLevel("2", 59)
			servertest.RunTicks(t, srv, 1)
			expectAirAt(t, srv, 8, 64, 8, "tick 60")
		})
	}
}
