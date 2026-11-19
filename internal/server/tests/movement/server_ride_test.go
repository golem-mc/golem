package movement

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
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

// TestRideRefusesAPlayerVehicleAndAnotherDimension: a player is never a
// vehicle, and a boat of another dimension is out of reach.
func TestRideRefusesAPlayerVehicleAndAnotherDimension(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Rider"))
	c := connect().Join("Rider")
	c.MoveToGround(0.5, 64, 0.5, true)
	connect().Join("Other")
	c.ExpectPlayerAdded("Other")
	c.Settle()

	c.ChatCommand("ride @s mount Other")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.failure.cant_ride_players")

	// The vehicle must be selectable before /ride can reject its dimension.
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 2, 0))
	boat := srv.SpawnBoatInDimension(world.NetherName, 2.5, 64, 0.5, "minecraft:oak_boat")
	if boat == 0 {
		t.Fatal("SpawnBoatInDimension came to nothing")
	}
	c.ChatCommand("ride @s mount @e[type=oak_boat,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.failure.wrong_dimension")
	if got := srv.BoatPassengerIDs(boat); len(got) != 0 {
		t.Errorf("passengers = %v, want none", got)
	}
}

// TestRideSeatsASecondPlayerOnAChestBoat: startRiding's force skips the seat
// count, so a second rider boards a chest boat that seats one.
func TestRideSeatsASecondPlayerOnAChestBoat(t *testing.T) {
	t.Parallel()
	srv, boat, c := rideWorld(t, "minecraft:oak_chest_boat")

	c.ChatCommand("ride @s mount @e[type=oak_chest_boat,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	c.ChatCommand("ride Other mount @e[type=oak_chest_boat,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	if got := srv.BoatPassengerIDs(boat); len(got) != 2 {
		t.Fatalf("passengers = %v, want both aboard", got)
	}

	c.ChatCommand("ride Other dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	if got := srv.BoatPassengerIDs(boat); len(got) != 1 {
		t.Errorf("passengers = %v, want the runner alone", got)
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

// TestRideMountsAndDismountsAMobInAMinecart: a mob target takes a cart's
// seat, a second one forced in beside it, and leaves it again.
// vanilla: Entity.startRiding, force skipping canAddPassenger
func TestRideMountsAndDismountsAMobInAMinecart(t *testing.T) {
	t.Parallel()
	srv, _, c := rideWorld(t, "minecraft:oak_boat")
	cart := srv.SpawnMinecart(0.5, 64, 2.5)
	pig := srv.SpawnMob("minecraft:pig", 6.5, 64, 6.5)
	cow := srv.SpawnMob("minecraft:cow", 6.5, 64, -5.5)
	if cart == 0 || pig == 0 || cow == 0 {
		t.Fatal("spawning the cart, the pig or the cow came to nothing")
	}
	c.Settle()

	c.ChatCommand("ride @e[type=pig,limit=1] mount @e[type=minecart,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	if got := srv.MinecartPassengerIDs(cart); len(got) != 1 || got[0] != pig {
		t.Errorf("passengers = %v, want the pig %d", got, pig)
	}
	if got := srv.MinecartMobRidersInDimension(world.DimensionName); len(got) != 1 || !slices.Equal(got[0], []string{"minecraft:pig"}) {
		t.Errorf("the cart's mob riders = %v, want the pig", got)
	}
	// The cart's tick holds the pig in its seat; the pig's own would walk it.
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 20)
	cx, cy, cz, _, _ := srv.MinecartState(cart)
	for _, m := range srv.MobsInDimension(world.DimensionName) {
		if m.ID == pig && (m.X != cx || m.Z != cz || m.Y < cy || m.Y > cy+0.5) {
			t.Errorf("the pig stands at (%v, %v, %v), want seated in the cart at (%v, %v, %v)", m.X, m.Y, m.Z, cx, cy, cz)
		}
	}

	c.ChatCommand("ride @e[type=cow,limit=1] mount @e[type=minecart,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	if got := srv.MinecartPassengerIDs(cart); !slices.Equal(got, []int32{pig, cow}) {
		t.Errorf("passengers = %v, want the pig %d and the cow %d", got, pig, cow)
	}

	c.ChatCommand("ride @e[type=pig,limit=1] dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	c.ChatCommand("ride @e[type=cow,limit=1] dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	if got := srv.MinecartPassengerIDs(cart); len(got) != 0 {
		t.Errorf("passengers after the dismounts = %v, want none", got)
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

// TestRideSeatsAPlayerAtTheMountsOwnAttachment: the seat height follows the
// mount's passenger attachment less the player's 0.6, not the horse's.
func TestRideSeatsAPlayerAtTheMountsOwnAttachment(t *testing.T) {
	t.Parallel()
	srv, c := rideMobWorld(t)
	pig := srv.SpawnMob("minecraft:pig", 6.5, 64, 6.5)
	cow := srv.SpawnMob("minecraft:cow", 6.5, 64, 9.5)
	c.Settle()

	for _, tc := range []struct {
		sel    string
		id     int32
		attach float64
	}{{"pig", pig, 0.86875}, {"cow", cow, 1.36875}} {
		c.ChatCommand("ride @s mount @e[type=" + tc.sel + ",limit=1]")
		servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
		_, y, _, _ := srv.MobPos(tc.id)
		if got, want := srv.PlayerPosition("Rider").Y-y, tc.attach-0.6; got < want-1e-6 || got > want+1e-6 {
			t.Errorf("the runner sits %g above the %s, want %g", got, tc.sel, want)
		}
		c.ChatCommand("ride @s dismount")
		servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	}
}

// TestRideListsAPlayerFirstAndSeatsASecondOne: a player boards ahead of the
// mobs already aboard, and a forced ride seats a second player behind them.
func TestRideListsAPlayerFirstAndSeatsASecondOne(t *testing.T) {
	t.Parallel()
	srv, c := rideMobWorld(t)
	pig := srv.SpawnMob("minecraft:pig", 6.5, 64, 6.5)
	zombie := srv.SpawnMob("minecraft:zombie", 8.5, 64, 6.5)
	c.Settle()

	c.ChatCommand("ride @e[type=zombie,limit=1] mount @e[type=pig,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	servertest.AwaitPassengersFor(c, pig)
	c.ChatCommand("ride @s mount @e[type=pig,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	if got := servertest.AwaitPassengersFor(c, pig); len(got) != 2 || got[0] != c.PlayerEntityID || got[1] != zombie {
		t.Errorf("passengers of the pig = %v, want the runner %d then the zombie %d", got, c.PlayerEntityID, zombie)
	}

	c.ChatCommand("ride Other mount @e[type=pig,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	got := servertest.AwaitPassengersFor(c, pig)
	if len(got) != 3 || got[0] != c.PlayerEntityID || got[1] != zombie {
		t.Errorf("passengers of the pig = %v, want the second player seated behind both", got)
	}

	c.ChatCommand("ride Other dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	if got := servertest.AwaitPassengersFor(c, pig); len(got) != 2 {
		t.Errorf("passengers of the pig after the second player left = %v, want two", got)
	}
}

// TestRideSetsARiderDownBesideAnAnimalWithFreeGround: a mob and a player
// taken off a pig or a cow with open ground around it stand in the first
// cell the search finds, the one to the animal's right (west, for the south
// facing a spawn has), at the floor's height.
func TestRideSetsARiderDownBesideAnAnimalWithFreeGround(t *testing.T) {
	t.Parallel()
	srv, c := rideMobWorld(t)
	pig := srv.SpawnMob("minecraft:pig", 6.5, 64, 6.5)
	zombie := srv.SpawnMob("minecraft:zombie", 8.5, 64, 6.5)
	cow := srv.SpawnMob("minecraft:cow", 6.5, 64, 9.5)
	c.Settle()

	c.ChatCommand("ride @e[type=zombie,limit=1] mount @e[type=pig,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	c.ChatCommand("ride @e[type=zombie,limit=1] dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	px, py, pz, _ := srv.MobPos(pig)
	x, y, z, _ := srv.MobPos(zombie)
	if x != px-1 || z != pz || y != py {
		t.Errorf("the zombie stands at %g %g %g, want beside the pig at %g %g %g", x, y, z, px-1, py, pz)
	}

	c.ChatCommand("ride @s mount @e[type=cow,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	c.ChatCommand("ride @s dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	cx, cy, cz, _ := srv.MobPos(cow)
	if got := srv.PlayerPosition("Rider"); got.X != cx-1 || got.Z != cz || got.Y != cy {
		t.Errorf("the runner stands at %g %g %g, want beside the cow at %g %g %g", got.X, got.Y, got.Z, cx-1, cy, cz)
	}
}

// TestRideSetsARiderDownOnTopOfAnAnimalWalledIn: with stone in the cells
// around a pig, the search finds no floor under a full block and the rider
// ends on the pig's back; with one cell open it takes that one in the
// search order.
func TestRideSetsARiderDownOnTopOfAnAnimalWalledIn(t *testing.T) {
	t.Parallel()
	srv, c := rideMobWorld(t)
	pig := srv.SpawnMob("minecraft:pig", 6.5, 64, 6.5)
	c.Settle()
	stone := servertest.BlockState(t, "minecraft:stone")
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			if dx != 0 || dz != 0 {
				srv.SetBlock(6+dx, 64, 6+dz, stone)
			}
		}
	}

	c.ChatCommand("ride @s mount @e[type=pig,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	c.ChatCommand("ride @s dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	px, py, pz, _ := srv.MobPos(pig)
	if got := srv.PlayerPosition("Rider"); got.X != px || got.Z != pz || got.Y < py+0.9-1e-6 || got.Y > py+0.9+1e-6 {
		t.Errorf("the runner stands at %g %g %g, want on top of the pig at %g %g %g", got.X, got.Y, got.Z, px, py+0.9, pz)
	}

	// The east cell is the second in the search order.
	srv.SetBlock(7, 64, 6, 0)
	c.ChatCommand("ride @s mount @e[type=pig,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	c.ChatCommand("ride @s dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	if got := srv.PlayerPosition("Rider"); got.X != px+1 || got.Z != pz || got.Y != py {
		t.Errorf("the runner stands at %g %g %g, want in the open cell at %g %g %g", got.X, got.Y, got.Z, px+1, py, pz)
	}
}

// TestRideRefusesALoopTwoSeatsDeepAndAnotherDimension: a mob cannot mount
// the mob that rides on it through another, and a mount in another dimension
// is out of reach.
func TestRideRefusesALoopTwoSeatsDeepAndAnotherDimension(t *testing.T) {
	t.Parallel()
	srv, c := rideMobWorld(t)
	pig := srv.SpawnMob("minecraft:pig", 6.5, 64, 6.5)
	srv.SpawnMob("minecraft:zombie", 8.5, 64, 6.5)
	srv.SpawnMob("minecraft:cow", 6.5, 64, 9.5)
	c.Settle()

	c.ChatCommand("ride @e[type=zombie,limit=1] mount @e[type=pig,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	c.ChatCommand("ride @e[type=cow,limit=1] mount @e[type=zombie,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	c.ChatCommand("ride @e[type=pig,limit=1] mount @e[type=cow,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.failure.loop")
	if got := srv.MobVehicleID(pig); got != 0 {
		t.Errorf("the pig's vehicle = %d, want none", got)
	}

	// The vehicle must be selectable before /ride can reject its dimension.
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 2, 0))
	sheep := srv.SpawnMobInDimension("minecraft:sheep", world.NetherName, 2.5, 64, 0.5)
	if sheep == 0 {
		t.Fatal("SpawnMobInDimension came to nothing")
	}
	c.ChatCommand("ride @e[type=pig,limit=1] mount @e[type=sheep,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.failure.wrong_dimension")
	c.ChatCommand("ride @s mount @e[type=sheep,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.failure.wrong_dimension")
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

// TestRideDismountedMobIsSentAbsolute: a pig carried off in a boat is told
// its place absolute on the first movement tick off the seat, as the client's
// base for it stayed where it boarded.
func TestRideDismountedMobIsSentAbsolute(t *testing.T) {
	t.Parallel()
	srv, _, c := rideWorld(t, "minecraft:oak_boat")
	pig := srv.SpawnMob("minecraft:pig", 6.5, 64, 6.5)
	if pig == 0 {
		t.Fatal("SpawnMob came to nothing")
	}
	c.Settle()

	c.ChatCommand("ride @e[type=pig,limit=1] mount @e[type=oak_boat,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	c.ChatCommand("tp @e[type=oak_boat,limit=1] 22.5 64 0.5")
	c.ReadTicks(srv, 20, func(int32, []byte) bool { return false })

	servertest.HoldTicks(t, srv)
	c.ChatCommand("ride @e[type=pig,limit=1] dismount")
	c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
	moved := servertest.PacketAbout(pig, protocol.IDPlayTeleportEntity,
		protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot)
	var gotID int32
	var got []byte
	if !c.ReadTicks(srv, 10, func(id int32, data []byte) bool {
		if moved(id, data) {
			gotID, got = id, data
			return true
		}
		return false
	}) {
		t.Fatal("no position packet for the pig after the dismount")
	}
	if gotID != protocol.IDPlayTeleportEntity {
		t.Fatalf("first position packet after the dismount = 0x%02X, want Teleport Entity 0x%02X", gotID, protocol.IDPlayTeleportEntity)
	}
	r := protocol.NewReader(got)
	r.ReadVarInt() // entity id
	r.ReadVarInt() // position path
	x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
	wx, wy, wz, ok := srv.MobPos(pig)
	if !ok || x != wx || y != wy || z != wz {
		t.Errorf("Teleport Entity at (%v, %v, %v), want the pig's (%v, %v, %v)", x, y, z, wx, wy, wz)
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

// TestBoardingAVehicleWritesOffTheFall: a body that rides loses its fall each
// tick, so stepping off a cart or a boat deals none of it.
// vanilla: LivingEntity.rideTick
func TestBoardingAVehicleWritesOffTheFall(t *testing.T) {
	t.Parallel()
	t.Run("minecart", func(t *testing.T) {
		t.Parallel()
		srv, _, c := rideWorld(t, "minecraft:oak_boat")
		srv.SpawnMinecart(0.5, 64, 2.5)
		if got := fallWrittenOffByRiding(t, srv, c, "ride @s mount @e[type=minecart,limit=1]"); got != 0 {
			t.Errorf("damage events after the dismount = %d, want 0", got)
		}
	})
	t.Run("boat", func(t *testing.T) {
		t.Parallel()
		srv, _, c := rideWorld(t, "minecraft:oak_boat")
		if got := fallWrittenOffByRiding(t, srv, c, "ride @s mount @e[type=oak_boat,limit=1]"); got != 0 {
			t.Errorf("damage events after the dismount = %d, want 0", got)
		}
	})
}
