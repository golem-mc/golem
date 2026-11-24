package movement

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A vanilla server reports a rider in the fox's column at any yaw; only the
// height follows the attachment.
// vanilla: EntityTypes.FOX, Fox.BABY_DIMENSIONS, Entity.positionRider
func TestRideSeatsPassengersAtFoxAttachment(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, age string
		attachY   float64
	}{
		{"adult", "0", 0.6375},
		{"baby", "-24000", 0.375},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c := servertest.Join(t, cfg, "Rider", servertest.OpsListing(t, "Rider"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("summon fox 6.5 64 6.5 {Age:" + tc.age + ",NoAI:1b,NoGravity:1b,Rotation:[0f,0f]}")
			_, fox := c.AwaitNextSpawn()
			c.ChatCommand("summon pig 8.5 64 6.5 {Age:0,NoAI:1b,NoGravity:1b}")
			_, pig := c.AwaitNextSpawn()
			check := func(t *testing.T, dx, dz float64) {
				t.Helper()
				fx, fy, fz, ok := srv.MobPos(fox.EntityID)
				if !ok {
					t.Fatal("fox disappeared")
				}
				px, py, pz, ok := srv.MobPos(pig.EntityID)
				if !ok {
					t.Fatal("pig disappeared")
				}
				pos := srv.PlayerPosition("Rider")
				for _, rider := range []struct {
					name    string
					x, y, z float64
					attachY float64
				}{
					{"pig", px, py, pz, 0},
					{"player", pos.X, pos.Y, pos.Z, 0.6},
				} {
					if math.Abs(rider.x-fx-dx) > 1e-6 || math.Abs(rider.y-fy-(tc.attachY-rider.attachY)) > 1e-6 || math.Abs(rider.z-fz-dz) > 1e-6 {
						t.Errorf("%s offset = (%g, %g, %g), want (%g, %g, %g)", rider.name, rider.x-fx, rider.y-fy, rider.z-fz, dx, tc.attachY-rider.attachY, dz)
					}
				}
			}
			for _, facing := range []struct {
				name, rotation string
				dx, dz         float64
			}{
				{"south", "", 0, 0},
				{"west", "90", 0, 0},
			} {
				t.Run(facing.name, func(t *testing.T) {
					if facing.rotation != "" {
						c.ChatCommand("tp @e[type=fox,limit=1] 6.5 64 6.5 " + facing.rotation + " 0")
						servertest.AwaitSystemChatSaying(t, c, "commands.teleport.success.location.single")
					}
					c.ChatCommand("ride @e[type=pig,limit=1] mount @e[type=fox,limit=1]")
					servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
					if got := servertest.AwaitPassengersFor(c, fox.EntityID); len(got) != 1 || got[0] != pig.EntityID {
						t.Fatalf("fox passengers = %v, want pig %d", got, pig.EntityID)
					}
					c.ChatCommand("ride @s mount @e[type=fox,limit=1]")
					servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
					if got := servertest.AwaitPassengersFor(c, fox.EntityID); len(got) != 2 || got[0] != c.PlayerEntityID || got[1] != pig.EntityID {
						t.Fatalf("fox passengers = %v, want player %d then pig %d", got, c.PlayerEntityID, pig.EntityID)
					}
					check(t, facing.dx, facing.dz)
					servertest.RunTicks(t, srv, 2)
					check(t, facing.dx, facing.dz)
					c.ChatCommand("ride @s dismount")
					servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
					servertest.AwaitPassengersFor(c, fox.EntityID)
					c.ChatCommand("ride @e[type=pig,limit=1] dismount")
					servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
					servertest.AwaitPassengersFor(c, fox.EntityID)
				})
			}
		})
	}
}
