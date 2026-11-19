package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/server"
)

// rideWorld joins the operator Rider and a second player, Other, in a flat
// world with a boat of the kind given standing beside them.
func rideWorld(t *testing.T, kind string) (srv *server.Server, boat int32, c *servertest.Client) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Rider"))
	c = connect()
	c.Join("Rider")
	c.MoveToGround(0.5, 64, 0.5, true)
	connect().Join("Other")
	c.ExpectPlayerAdded("Other")
	c.Settle()
	boat = srv.SpawnBoat(2.5, 64, 0.5, kind)
	if boat == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	return srv, boat, c
}

// riderOf is the name of the boat's driver, empty for an empty boat.
func riderOf(srv *server.Server, boat int32) string {
	name, _ := srv.BoatRider(boat)
	return name
}

func TestTeleportTakesARidingPlayerOffTheirMount(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"pig", "oak_boat", "minecart", "cushion"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Rider", servertest.OpsListing(t, "Rider"))
			servertest.HoldTicks(t, srv)
			c.MoveToGround(0.5, 64, 0.5, true)
			c.ChatCommand("summon minecraft:" + kind + " 0.5 64 0.5 {NoAI:1b,NoGravity:1b}")
			_, mount := c.AwaitNextSpawn()
			c.ChatCommand("ride @s mount @e[type=" + kind + ",limit=1]")
			servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
			if got := servertest.AwaitPassengersFor(c, mount.EntityID); len(got) != 1 || got[0] != c.PlayerEntityID {
				t.Fatalf("mount passengers = %v, want the rider", got)
			}
			c.Settle()

			c.ChatCommand("tp @s 10 64 0.5")
			dismounted := false
			if !c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
				switch id {
				case protocol.IDPlaySetPassengers:
					vehicle, riders := servertest.ReadPassengers(c, data)
					if vehicle == mount.EntityID {
						dismounted = len(riders) == 0
					}
				case protocol.IDPlaySyncPosition:
					if !dismounted {
						t.Error("teleport position arrived before the mount's empty Set Passengers")
					}
					pos := servertest.ReadSyncPosition(t, data)
					if pos.X != 10.5 || pos.Y != 64 || pos.Z != 0.5 {
						t.Errorf("teleport position = %+v, want 10.5 64 0.5", pos)
					}
					return true
				}
				return false
			}) {
				t.Fatal("no teleport position arrived")
			}
			servertest.RunTicks(t, srv, 2)
			if pos := srv.PlayerPosition("Rider"); pos.X != 10.5 || pos.Y != 64 || pos.Z != 0.5 {
				t.Errorf("rider position after two ticks = %+v, want 10.5 64 0.5", pos)
			}
			if dismounted {
				c.ChatCommand("ride @s dismount")
				servertest.AwaitSystemChatSaying(t, c, "commands.ride.not_riding")
			}
		})
	}
}

// TestRideMountsAndDismountsAPlayerOnABoat has /ride seat the runner on a
// boat and take them off again, with vanilla's feedback and the refusals of
// a rider mounting twice and a body not riding.
func TestRideMountsAndDismountsAPlayerOnABoat(t *testing.T) {
	t.Parallel()
	srv, boat, c := rideWorld(t, "minecraft:oak_boat")

	c.ChatCommand("ride @s dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.not_riding")

	c.ChatCommand("ride @s mount @e[type=oak_boat,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	if got := servertest.AwaitPassengersFor(c, boat); len(got) != 1 || got[0] != c.PlayerEntityID {
		t.Errorf("passengers = %v, want the runner's own %d", got, c.PlayerEntityID)
	}
	if got := riderOf(srv, boat); got != "Rider" {
		t.Errorf("the boat's rider = %q, want Rider", got)
	}

	c.ChatCommand("ride @s mount @e[type=oak_boat,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.already_riding")

	c.ChatCommand("ride @s dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	if got := srv.BoatPassengerIDs(boat); len(got) != 0 {
		t.Errorf("passengers after the dismount = %v, want none", got)
	}
}

// TestRideMountsAndDismountsAMobOnABoat: a mob target takes a boat seat and
// leaves it again, a mob already aboard being refused a second boarding.
func TestRideMountsAndDismountsAMobOnABoat(t *testing.T) {
	t.Parallel()
	srv, boat, c := rideWorld(t, "minecraft:oak_boat")
	pig := srv.SpawnMob("minecraft:pig", 6.5, 64, 6.5)
	if pig == 0 {
		t.Fatal("SpawnMob came to nothing")
	}
	c.Settle()

	c.ChatCommand("ride @e[type=pig,limit=1] dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.not_riding")

	c.ChatCommand("ride @e[type=pig,limit=1] mount @e[type=oak_boat,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	if got := srv.BoatPassengerIDs(boat); len(got) != 1 || got[0] != pig {
		t.Errorf("passengers = %v, want the pig %d", got, pig)
	}

	c.ChatCommand("ride @e[type=pig,limit=1] mount @e[type=oak_boat,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.already_riding")

	c.ChatCommand("ride @e[type=pig,limit=1] dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	if got := srv.BoatPassengerIDs(boat); len(got) != 0 {
		t.Errorf("passengers after the dismount = %v, want none", got)
	}
}

// TestRideSeatsAPlayerInAMinecart: a cart is a seat /ride fills and empties,
// and a rider in one is refused a second mount.
func TestRideSeatsAPlayerInAMinecart(t *testing.T) {
	t.Parallel()
	srv, _, c := rideWorld(t, "minecraft:oak_boat")
	cart := srv.SpawnMinecart(0.5, 64, 2.5)

	c.ChatCommand("ride @s mount @e[type=minecart,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	if name, _ := srv.MinecartRider(cart); name != "Rider" {
		t.Errorf("the cart's rider = %q, want Rider", name)
	}

	c.ChatCommand("ride @s mount @e[type=oak_boat,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.already_riding")

	c.ChatCommand("ride @s dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	if got := srv.MinecartPassengerIDs(cart); len(got) != 0 {
		t.Errorf("passengers after the dismount = %v, want none", got)
	}
}

// TestRideRefusesABodyAsItsOwnVehicle: a body is never its own vehicle.
func TestRideRefusesABodyAsItsOwnVehicle(t *testing.T) {
	t.Parallel()
	srv, _, c := rideWorld(t, "minecraft:oak_boat")
	srv.SpawnMob("minecraft:pig", 6.5, 64, 6.5)
	c.Settle()

	c.ChatCommand("ride @e[type=pig,limit=1] mount @e[type=pig,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.failure.loop")
}

// TestRideSeatsAMobOnAMobAndTheRunnerOnAMob: a zombie sits on a pig, the
// runner sits on a cow, each Set Passengers is sent, and a stack that would
// close on itself is refused as a loop.
func TestRideSeatsAMobOnAMobAndTheRunnerOnAMob(t *testing.T) {
	t.Parallel()
	srv, _, c := rideWorld(t, "minecraft:oak_boat")
	pig := srv.SpawnMob("minecraft:pig", 6.5, 64, 6.5)
	zombie := srv.SpawnMob("minecraft:zombie", 8.5, 64, 6.5)
	cow := srv.SpawnMob("minecraft:cow", 6.5, 64, 9.5)
	if pig == 0 || zombie == 0 || cow == 0 {
		t.Fatal("SpawnMob came to nothing")
	}
	c.Settle()

	c.ChatCommand("ride @e[type=zombie,limit=1] mount @e[type=pig,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	if got := servertest.AwaitPassengersFor(c, pig); len(got) != 1 || got[0] != zombie {
		t.Errorf("passengers of the pig = %v, want the zombie %d", got, zombie)
	}
	if got := srv.MobVehicleID(zombie); got != pig {
		t.Errorf("the zombie's vehicle = %d, want the pig %d", got, pig)
	}

	c.ChatCommand("ride @e[type=zombie,limit=1] mount @e[type=cow,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.already_riding")
	c.ChatCommand("ride @e[type=pig,limit=1] mount @e[type=zombie,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.failure.loop")

	c.ChatCommand("ride @s mount @e[type=cow,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	if got := servertest.AwaitPassengersFor(c, cow); len(got) != 1 || got[0] != c.PlayerEntityID {
		t.Errorf("passengers of the cow = %v, want the runner %d", got, c.PlayerEntityID)
	}

	c.ChatCommand("ride @e[type=zombie,limit=1] dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	if got := srv.MobPassengerIDs(pig); len(got) != 0 {
		t.Errorf("passengers of the pig after the dismount = %v, want none", got)
	}
	c.ChatCommand("ride @s dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	if got := srv.MobPassengerIDs(cow); len(got) != 0 {
		t.Errorf("passengers of the cow after the dismount = %v, want none", got)
	}
}

// rideMobWorld is rideWorld with the loop held, so a mob stands where the
// test put it until a tick is asked for.
func rideMobWorld(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, _, c := rideWorld(t, "minecraft:oak_boat")
	servertest.HoldTicks(t, srv)
	return srv, c
}

// TestRideKeepsARiderOnADyingMountUntilItIsRemoved: the rider leaves a killed
// mob when the corpse is removed twenty ticks later, not when it dies.
func TestRideKeepsARiderOnADyingMountUntilItIsRemoved(t *testing.T) {
	t.Parallel()
	srv, c := rideMobWorld(t)
	pig := srv.SpawnMob("minecraft:pig", 6.5, 64, 6.5)
	c.Settle()
	c.ChatCommand("ride @s mount @e[type=pig,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	servertest.AwaitPassengersFor(c, pig)

	if !srv.KillMob(pig) {
		t.Fatal("the pig was not killed")
	}
	emptied := func(id int32, data []byte) bool {
		if id != protocol.IDPlaySetPassengers {
			return false
		}
		vehicle, riders := servertest.ReadPassengers(c, data)
		return vehicle == pig && len(riders) == 0
	}
	if c.ReadTicks(srv, 19, emptied) {
		t.Fatal("the rider left the pig before its corpse was removed")
	}
	if !c.ReadTicks(srv, 4, emptied) {
		t.Error("the rider stayed on the pig after its corpse was removed")
	}
}

// fallWrittenOffByRiding drops from y=90 to y=66 in the air, boards with the
// ride command given, rides 2 ticks, dismounts and lands on the spot it is put
// at, and returns the damage events that arrived.
func fallWrittenOffByRiding(t *testing.T, srv *server.Server, c *servertest.Client, mount string) int {
	t.Helper()
	srv.SetPlayerGameMode("Rider", protocol.GameModeSurvival)
	servertest.HoldTicks(t, srv)
	for y := 90.0; y >= 66; y -= 6 {
		c.MoveToGround(0.5, y, 0.5, false)
	}
	// The reply is what says the command ran: a bare RunTicks could pass its
	// ticks before the command's packet is read.
	c.ChatCommand(mount)
	c.ExpectWithinTicks(srv, 10, protocol.IDPlaySystemChat)
	servertest.RunTicks(t, srv, 2)
	c.ChatCommand("ride @s dismount")
	c.ExpectWithinTicks(srv, 10, protocol.IDPlaySystemChat)
	pos := c.LastTeleport
	c.MoveToGround(pos.X, pos.Y, pos.Z, true)
	servertest.RunTicks(t, srv, 2)
	return servertest.DrainDamage(c)
}
