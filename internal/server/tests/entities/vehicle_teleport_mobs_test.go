package entities

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestAMobRiddenVehicleTeleportedAcrossCarriesItsRider: a boat or a minecart
// with a pig seated by /ride, teleported into the nether by command, arrives
// with the pig still riding, and no pig is left in the overworld.
// vanilla: Entity.teleportCrossDimension
func TestAMobRiddenVehicleTeleportedAcrossCarriesItsRider(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		spawn  func(srv *server.Server) int32
		riders func(srv *server.Server, dimension string) [][]string
	}{
		{"oak_boat", func(srv *server.Server) int32 {
			return srv.SpawnBoat(2.5, 64, 2.5, "minecraft:oak_boat")
		}, (*server.Server).BoatMobRidersInDimension},
		{"minecart", func(srv *server.Server) int32 {
			return srv.SpawnMinecart(2.5, 64, 2.5)
		}, (*server.Server).MinecartMobRidersInDimension},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.Join(t, servertest.CrossingTestConfig(t), "Ferry")
			// No player stands in the nether to keep the arrival loaded.
			defer srv.HoldChunkInDimension(world.NetherName, 8, 8)()
			servertest.HoldTicks(t, srv)

			if tc.spawn(srv) == 0 || srv.SpawnMob("minecraft:pig", 2.5, 64, 2.5) == 0 {
				t.Fatalf("spawning the %s or the pig failed", tc.name)
			}
			srv.RunLine("ride @e[type=minecraft:pig,limit=1] mount @e[type=minecraft:" + tc.name + ",limit=1]")
			if got := tc.riders(srv, world.DimensionName); len(got) != 1 || !slices.Equal(got[0], []string{"minecraft:pig"}) {
				t.Fatalf("the %s holds %v before the teleport, want the pig", tc.name, got)
			}

			srv.RunLine("execute in minecraft:the_nether run tp @e[type=minecraft:" + tc.name + ",limit=1] 8.5 70 8.5")
			// A tick lets the husk's pass run, which would eject a pig left aboard.
			servertest.RunTicks(t, srv, 2)
			if got := tc.riders(srv, world.NetherName); len(got) != 1 || !slices.Equal(got[0], []string{"minecraft:pig"}) {
				t.Errorf("the %s in the nether holds %v, want the pig still riding", tc.name, got)
			}
			if got := tc.riders(srv, world.DimensionName); len(got) != 0 {
				t.Errorf("vehicles left in the overworld: %v, want none", got)
			}
			for _, m := range srv.MobsInDimension(world.DimensionName) {
				if m.Kind == "minecraft:pig" {
					t.Errorf("a pig was left in the overworld at (%v, %v, %v)", m.X, m.Y, m.Z)
				}
			}
		})
	}
}
