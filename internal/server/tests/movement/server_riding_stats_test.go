package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vehicleStats are the distance statistics a vehicle can carry a rider
// toward.
var vehicleStats = []string{
	"minecraft:minecart_one_cm", "minecraft:boat_one_cm", "minecraft:pig_one_cm",
	"minecraft:nautilus_one_cm", "minecraft:horse_one_cm", "minecraft:strider_one_cm", "minecraft:happy_ghast_one_cm",
}

// TestRidersCountTheDistanceTheirVehicleCarriesThem: three blocks of carry
// add 300 to the statistic of the vehicle's kind and to no other, and the
// step onto the seat adds nothing.
// vanilla: ServerPlayer.checkRidingStatistics
func TestRidersCountTheDistanceTheirVehicleCarriesThem(t *testing.T) {
	t.Parallel()
	mobCase := func(kind string) func(*server.Server, *servertest.Client) func() {
		return func(srv *server.Server, c *servertest.Client) func() {
			id := srv.SpawnMob(kind, 6.5, 64, 6.5)
			srv.SetMobNoAI(id)
			c.Settle()
			c.ChatCommand("ride @s mount @e[type=" + kind + ",limit=1]")
			servertest.AwaitSystemChatSaying(c.T, c, "commands.ride.mount.success")
			return func() { srv.MoveMob(id, 9.5, 6.5) }
		}
	}
	for _, tc := range []struct {
		name string
		ride func(*server.Server, *servertest.Client) func()
		stat string
	}{
		{"pig", mobCase("minecraft:pig"), "minecraft:pig_one_cm"},
		{"horse", mobCase("minecraft:horse"), "minecraft:horse_one_cm"},
		{"happy ghast", mobCase("minecraft:happy_ghast"), "minecraft:happy_ghast_one_cm"},
		{"nautilus", mobCase("minecraft:nautilus"), "minecraft:nautilus_one_cm"},
		{"cow", mobCase("minecraft:cow"), ""},
		{"boat", func(srv *server.Server, c *servertest.Client) func() {
			c.ChatCommand("ride @s mount @e[type=oak_boat,limit=1]")
			servertest.AwaitSystemChatSaying(c.T, c, "commands.ride.mount.success")
			return func() { c.ChatCommand("tp @e[type=oak_boat,limit=1] 5.5 64 0.5") }
		}, "minecraft:boat_one_cm"},
		{"minecart", func(srv *server.Server, c *servertest.Client) func() {
			srv.SpawnMinecart(0.5, 64, 2.5)
			c.Settle()
			c.ChatCommand("ride @s mount @e[type=minecart,limit=1]")
			servertest.AwaitSystemChatSaying(c.T, c, "commands.ride.mount.success")
			return func() { c.ChatCommand("tp @e[type=minecart,limit=1] 3.5 64 2.5") }
		}, "minecraft:minecart_one_cm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := rideMobWorld(t)
			carry := tc.ride(srv, c)
			servertest.RunTicks(t, srv, 2)
			for _, key := range vehicleStats {
				if got := customStatOf(srv, "Rider", key); got != 0 {
					t.Errorf("%s = %d before the carry, want 0", key, got)
				}
			}
			carry()
			c.Settle()
			servertest.RunTicks(t, srv, 2)
			for _, key := range vehicleStats {
				want := int32(0)
				if key == tc.stat {
					want = 300
				}
				if got := customStatOf(srv, "Rider", key); got != want {
					t.Errorf("%s = %d, want %d", key, got, want)
				}
			}
		})
	}
}
