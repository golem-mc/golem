package world

import (
	"bytes"
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The nether portals end to end: a flint and steel lighting a valid frame, a
// frame broken while its portal stands, a player standing in the portal until
// it takes them through, a portal built where there was none and found again
// on the way back — and the end portal's two ways.

// blockOfFloor is the block a coordinate is in, for reading the world under a
// player standing at a block's centre; negative coordinates need the floor,
// not the truncation.
func blockOfFloor(coordinate float64) int32 {
	return int32(math.Floor(coordinate))
}

// TestLightingAFilledFrameAndBreakingIt checks the making and the unmaking
// of a portal: a flint and steel struck inside a valid obsidian frame fills
// the interior with nether portal blocks — the fire's own cell included — and
// breaking a frame block takes the whole portal out again, the frame having
// lost what the portal stood on.
func TestLightingAFilledFrameAndBreakingIt(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig() // creative, so the break drops nothing
	srv, c := servertest.Join(t, cfg, "Lighter")

	_, portalX, _, _ := servertest.PortalStates(t)
	servertest.BuildFrame(t, srv)

	// The steel is struck inside the frame: the top face of the floor
	// obsidian, which is where the fire would have to start for the frame
	// to light from within.
	servertest.Flint(t, c)
	c.PlaceBlock(servertest.PortalInnerX0, servertest.PortalFloorY-1, servertest.PortalInnerZ, protocol.FaceTop)
	c.Settle()

	// The interior is filled, with the axis the frame runs along.
	for _, cell := range servertest.FrameInterior() {
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got != portalX {
			t.Errorf("interior block at (%d, %d, %d) = %d, want the portal's %d",
				cell[0], cell[1], cell[2], got, portalX)
		}
	}

	// Breaking one block of the frame takes the whole portal with it: the
	// interior goes back to air, however far it runs from the corner.
	c.StartDigging(servertest.PortalInnerX0-1, servertest.PortalFloorY, servertest.PortalInnerZ)
	c.Settle()

	for _, cell := range servertest.FrameInterior() {
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got != 0 {
			t.Errorf("interior block at (%d, %d, %d) = %d after the frame broke, want air",
				cell[0], cell[1], cell[2], got)
		}
	}
}

// TestLightingAFrameFilledWithCaveAir checks the frame behind the air gate:
// a complete obsidian frame the generator left with cave air inside — what a
// cave-carved frame holds — lights from a strike at its floor, wearing the
// flint and steel one. vanilla: BaseFireBlock.canBePlacedAt
func TestLightingAFrameFilledWithCaveAir(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Spelunker")

	_, portalX, _, _ := servertest.PortalStates(t)
	servertest.BuildFrame(t, srv)
	caveAir, ok := registry.FindState("minecraft:cave_air", nil)
	if !ok {
		t.Fatal("the block registry has no cave_air")
	}
	for _, cell := range servertest.FrameInterior() {
		if !srv.SetBlock(cell[0], cell[1], cell[2], caveAir) {
			t.Fatalf("the chunk did not take the cave air at %v", cell)
		}
	}
	flintID, ok := registry.ItemID("minecraft:flint_and_steel")
	if !ok {
		t.Fatal("the item registry has no flint and steel")
	}
	srv.GiveSlot("Spelunker", player.HotbarSlot, protocol.ItemStack{ItemID: flintID, Count: 1})
	c.Settle()

	// Struck on the top face of the floor obsidian, the cell the fire would
	// start in.
	c.PlaceBlock(servertest.PortalInnerX0, servertest.PortalFloorY-1, servertest.PortalInnerZ, protocol.FaceTop)
	c.Settle()

	for _, cell := range servertest.FrameInterior() {
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got != portalX {
			t.Errorf("interior block at %v = %d, want the portal's %d", cell, got, portalX)
		}
	}
	if got := srv.InventorySlot("Spelunker", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the flint and steel's wear = %d, want 1", got.Damage)
	}
}

func TestLightingAFrameHoldingSoulFire(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Spelunker")

	_, portalX, _, _ := servertest.PortalStates(t)
	servertest.BuildFrame(t, srv)
	soulFire, ok := registry.FindState("minecraft:soul_fire", nil)
	if !ok {
		t.Fatal("the block registry has no soul_fire")
	}
	cells := servertest.FrameInterior()
	top := cells[len(cells)-1]
	if !srv.SetBlock(top[0], top[1], top[2], soulFire) {
		t.Fatalf("the chunk did not take the soul fire at %v", top)
	}
	flintID, ok := registry.ItemID("minecraft:flint_and_steel")
	if !ok {
		t.Fatal("the item registry has no flint and steel")
	}
	srv.GiveSlot("Spelunker", player.HotbarSlot, protocol.ItemStack{ItemID: flintID, Count: 1})
	c.Settle()

	c.PlaceBlock(servertest.PortalInnerX0, servertest.PortalFloorY-1, servertest.PortalInnerZ, protocol.FaceTop)
	c.Settle()

	for _, cell := range cells {
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got != portalX {
			t.Errorf("interior block at %v = %d, want the portal's %d", cell, got, portalX)
		}
	}
}

// fireWithoutAFrame checks the other thing a flint and steel makes — plain
// fire — as a helper's assertion: striking where no frame stands leaves the
// fire, and nothing else.
func TestFlintAndSteelLightsPlainFire(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Camper")
	// Where the struck ground lies within the block interaction's reach of
	// the hand: vanilla refuses a use past the reach.
	c.MoveToGround(11.5, 64, 11.5, true)
	c.Settle()

	fire, ok := registry.FindState("minecraft:fire", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:fire")
	}
	servertest.Flint(t, c)
	// On the flat stone out in the open, where no frame is standing: the
	// target is the air above the clicked ground.
	c.PlaceBlock(12, servertest.PortalFloorY-1, 12, protocol.FaceTop)
	c.Settle()

	if got := srv.BlockAt(12, servertest.PortalFloorY, 12); got != fire {
		t.Errorf("the open ground lights %d, want the plain fire %d", got, fire)
	}
	// And striking where fire would not survive lights nothing.
	c.PlaceBlock(20, servertest.PortalFloorY+5, 20, protocol.FaceWest)
	c.Settle()
	if got := srv.BlockAt(19, servertest.PortalFloorY+5, 20); got != 0 {
		t.Errorf("fire standing on nothing = %d, want no block", got)
	}
}

// TestStandingInAPortalTravelsToTheNether checks what standing in a lit
// portal does: the Respawn naming the nether with the client's data kept,
// the arrival an eighth of the coordinates in, inside a portal the server
// built where the nether can be stood on, over chunks the nether's own
// generator made.
func TestStandingInAPortalTravelsToTheNether(t *testing.T) {
	t.Parallel()
	cfg := servertest.NetherTestConfig(t, servertest.BlockTestConfig())
	srv, c := servertest.Join(t, cfg, "Traveller")

	_, portalX, portalZ, bedrock := servertest.PortalStates(t)
	servertest.BuildFrame(t, srv)
	servertest.Flint(t, c)
	c.PlaceBlock(servertest.PortalInnerX0, servertest.PortalFloorY-1, servertest.PortalInnerZ, protocol.FaceTop)
	c.Settle()

	// Stand in it, the way the client falls into the block from the front.
	// Creative crosses on the first tick of standing.
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ+0.5, 0, 0)

	// The crossing arrives as a Respawn naming the nether, the data kept
	// (0x03, the docs' "portals or teleports") and the cooldown armed
	// against an immediate bounce.
	data := c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != registry.MustID("minecraft:dimension_type", world.NetherType) {
		t.Errorf("respawn dimension type = %d, want the nether's", got)
	}
	if got := r.ReadString(32767); got != world.NetherName {
		t.Errorf("respawn dimension = %q, want %q", got, world.NetherName)
	}
	r.ReadI64() // hashed seed
	if mode := uint8(r.ReadVarInt()); mode != protocol.GameModeCreative {
		t.Errorf("respawn game mode = %d, want creative", mode)
	}
	r.ReadVarInt() // previous game mode
	r.ReadBool()
	r.ReadBool()
	r.ReadBool() // debug, superflat, death location
	// The cooldown was armed at the player's 10 ticks and has been ticking
	// down since; the packet carries what is left when it is built, so a
	// travel that took a moment sending (its destination search and build)
	// shows a little less.
	if cooldown := r.ReadVarInt(); cooldown <= 0 || cooldown > 10 {
		t.Errorf("respawn portal cooldown = %d, want the player's 10-tick cooldown partly spent", cooldown)
	}
	r.ReadVarInt() // sea level
	if kept := r.ReadU8(); kept != 0x03 {
		t.Errorf("respawn data kept = %#02x, want 0x03 (a living crossing keeps everything)", kept)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("respawn: %v", err)
	}

	// The nether's own stream arrives around the place the crossing put the
	// player, and is read through the way a client does — the arrival is
	// not standing anywhere until it has.
	c.ExpectViewWithinTicks(srv, 200)

	// The arrival: in the nether, standing in a portal the server built
	// where the nether can stand on something, an eighth of the way in.
	if got := srv.PlayerDimension("Traveller"); got != world.NetherName {
		t.Fatalf("dimension = %q, want the nether", got)
	}
	pos := srv.PlayerPosition("Traveller")
	feet := srv.BlockInDimension(world.NetherName, blockOfFloor(pos.X), int32(pos.Y), blockOfFloor(pos.Z))
	if feet != portalX && feet != portalZ {
		t.Errorf("the nether arrival stands in block %d, want a nether portal's", feet)
	}
	// An eighth of the way in from x 4 lands around x 0, and wherever the
	// standing room of the nether takes the portal, it stays within the
	// safe-spot search of that.
	if pos.X < -160 || pos.X > 160 || pos.Z < -160 || pos.Z > 160 {
		t.Errorf("nether arrival = (%v, %v), want within the safe-spot search of x/8", pos.X, pos.Z)
	}

	// The chunk under the arrival is the nether's own: bedrock at y 0.
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.LoadBlockInDimension(world.NetherName, blockOfFloor(pos.X), 0, blockOfFloor(pos.Z)) == bedrock
	})
}

// TestNetherPortalPreservesPlayerOffsetAndLook checks both exit axes and the return trip.
func TestNetherPortalPreservesPlayerOffsetAndLook(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name              string
		rotated, crouched bool
	}{
		{name: "aligned"},
		{name: "rotated", rotated: true},
		{name: "crouched-larger-exit", crouched: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.NetherTestConfig(t, servertest.BlockTestConfig())
			srv, c := servertest.Join(t, cfg, "Traveller")
			servertest.HoldTicks(t, srv)
			servertest.PortalFrame(t, srv)
			releaseExit := srv.HoldChunkInDimension(world.NetherName, 0, 0)
			obsidian, portalX, portalZ, _ := servertest.PortalStates(t)
			// Clear only the exit's immediate surroundings, leaving the fixture's
			// other chunks untouched.
			for x := int32(2); x <= 7; x++ {
				for y := int32(69); y <= 74; y++ {
					for z := int32(2); z <= 7; z++ {
						srv.SetBlockInDimension(world.NetherName, x, y, z, 0)
					}
				}
			}
			exitWidth, exitHeight := int32(2), int32(3)
			if tc.crouched {
				exitWidth, exitHeight = 3, 4
				c.SendSneak(true)
				c.Settle()
			}
			for along := int32(-1); along <= exitWidth; along++ {
				for y := int32(69); y <= 70+exitHeight; y++ {
					x, z, state := 4+along, int32(4), obsidian
					if along >= 0 && along < exitWidth && y >= 70 && y < 70+exitHeight {
						state = portalX
						if tc.rotated {
							state = portalZ
						}
					}
					if tc.rotated {
						x, z = z, x
					}
					if !srv.SetBlockInDimension(world.NetherName, x, y, z, state) {
						t.Fatal("building the exit failed")
					}
				}
			}
			releaseExit()
			srv.FlushSaves()
			entry := player.Position{X: 4.6, Y: 64.25, Z: 4.6, Yaw: 37, Pitch: 24}
			c.MoveTo(entry.X, entry.Y, entry.Z, entry.Yaw, entry.Pitch)
			c.Settle()
			cross := func(trips int64, dimension string) {
				t.Helper()
				// The search and chunk reload run off the tick loop; pace the
				// tick budget while they finish, then hold again for assertions.
				servertest.ReleaseTicks(t, srv)
				c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
				c.ExpectWithinTicks(srv, 200, protocol.IDPlaySyncPosition)
				servertest.UntilTicks(t, srv, 200, "portal travel to finish", func() bool {
					return srv.PortalTripsDecided() >= trips
				})
				servertest.HoldTicks(t, srv)
				if got := srv.PlayerDimension("Traveller"); got != dimension {
					t.Fatalf("dimension = %q, want %q", got, dimension)
				}
			}
			cross(1, world.NetherName)
			want := player.Position{X: 4.6, Y: 70.25, Z: 4.5, Yaw: 37, Pitch: 24}
			if tc.rotated {
				want.X, want.Z, want.Yaw = 4.5, 4.6, 127
			}
			if tc.crouched {
				// A 0.6x1.5 body maps from a 2x3 opening to a 3x4 one.
				want.X, want.Y = 4.814285714285714, 70.41666666666667
			}
			assertArrival := func(want player.Position) {
				t.Helper()
				for _, got := range []player.Position{c.LastTeleport, srv.PlayerPosition("Traveller")} {
					if math.Abs(got.X-want.X) > 1e-6 || math.Abs(got.Y-want.Y) > 1e-6 || math.Abs(got.Z-want.Z) > 1e-6 || math.Mod(float64(got.Yaw-want.Yaw), 360) != 0 || got.Pitch != want.Pitch {
						t.Errorf("arrival = %+v, want %+v", got, want)
					}
				}
			}
			assertArrival(want)

			// The loop is held: lift the cooldown and let the next portal tick
			// take the same offset back through the original opening.
			srv.SetPortalCooldown("Traveller", 0)
			cross(2, world.DimensionName)
			entry.Z = 4.5
			if tc.rotated {
				entry.Yaw = 217
			}
			assertArrival(entry)
		})
	}
}

// TestAPortalCrossingPlaysTheTravelSound checks the portal-travel sound
// (world event 1032 at the origin, data 0) a crossing player hears after
// arriving: through a nether portal and into the end, but not on foot out of
// the end, whose respawn search plays nothing.
// vanilla: TeleportTransition.playPortalSound
func TestAPortalCrossingPlaysTheTravelSound(t *testing.T) {
	t.Parallel()
	travelSound := func(t *testing.T, srv *server.Server, c *servertest.Client) bool {
		t.Helper()
		return c.ReadTicks(srv, 20, func(id int32, data []byte) bool {
			if id != protocol.IDPlayWorldEvent {
				return false
			}
			event, x, y, z, d := servertest.ReadWorldEvent(t, data)
			if event != protocol.WorldEventPortalTravel {
				return false
			}
			if x != 0 || y != 0 || z != 0 || d != 0 {
				t.Errorf("travel sound at (%d, %d, %d) data %d, want (0, 0, 0) data 0", x, y, z, d)
			}
			return true
		})
	}

	t.Run("nether", func(t *testing.T) {
		t.Parallel()
		cfg := servertest.NetherTestConfig(t, servertest.BlockTestConfig())
		srv, c := servertest.Join(t, cfg, "Traveller")
		servertest.BuildFrame(t, srv)
		servertest.Flint(t, c)
		c.PlaceBlock(servertest.PortalInnerX0, servertest.PortalFloorY-1, servertest.PortalInnerZ, protocol.FaceTop)
		c.Settle()
		c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ+0.5, 0, 0)
		c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
		if !travelSound(t, srv, c) {
			t.Error("no portal-travel sound after crossing into the nether")
		}
	})

	t.Run("end", func(t *testing.T) {
		t.Parallel()
		srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Ender")
		if !srv.SetBlock(10, servertest.PortalFloorY-1, 10, srv.EndPortalState()) {
			t.Fatal("placing the end portal block failed")
		}
		c.MoveTo(10.5, servertest.PortalFloorY-1, 10.5, 0, 0)
		c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
		if !travelSound(t, srv, c) {
			t.Error("no portal-travel sound after entering the end")
		}

		// Past the credits, the way back on foot crosses at once, silently.
		servertest.WaitFor(t, srv, 200, func() bool {
			return srv.PlayerDimension("Ender") == world.EndName
		})
		srv.SetSeenCredits("Ender", true)
		srv.TravelByEndPortalFrom("Ender", world.EndName)
		c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
		if travelSound(t, srv, c) {
			t.Error("the on-foot return from the end played the portal-travel sound")
		}
	})
}

// TestReturningFindsTheExistingPortal checks the other leg of a crossing: the
// player who walks back into the portal they arrived at is taken out at the
// portal they came by — the portal standing within the search around the
// destination, found rather than built — once the arrival's cooldown is out
// of the way.
func TestReturningFindsTheExistingPortal(t *testing.T) {
	t.Parallel()
	cfg := servertest.NetherTestConfig(t, servertest.BlockTestConfig())
	srv, c := servertest.Join(t, cfg, "Returner")

	_, portalX, _, _ := servertest.PortalStates(t)
	servertest.BuildFrame(t, srv)
	servertest.Flint(t, c)
	c.PlaceBlock(servertest.PortalInnerX0, servertest.PortalFloorY-1, servertest.PortalInnerZ, protocol.FaceTop)
	c.Settle()

	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ+0.5, 0, 0)
	// The crossing itself, and the nether's stream read through to its end
	// the way a client does — the player is not standing anywhere in the
	// nether until the chunks of it have arrived.
	c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
	c.ExpectViewWithinTicks(srv, 200)
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.PlayerDimension("Returner") == world.NetherName
	})

	// The way back, once the cooldown is not in the way any more: the test
	// lifts it rather than waiting out its 15 seconds. The player never
	// stepped out of the portal they arrived in, so standing is standing.
	srv.SetPortalCooldown("Returner", 0)
	servertest.WaitFor(t, srv, 400, func() bool {
		return srv.PlayerDimension("Returner") == world.DimensionName
	})
	// The way back's own stream, read through as well.
	c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
	c.ExpectViewWithinTicks(srv, 200)

	// The way out ended inside the portal they came by — the one the search
	// found standing within 128 blocks of the destination, rather than a
	// new one built beside it. The overworld chunk around the origin has
	// been unloaded and saved since everyone left it, so it is read back
	// the way a returning player's own server does: by loading it.
	pos := srv.PlayerPosition("Returner")
	x, y, z := blockOfFloor(pos.X), int32(pos.Y), blockOfFloor(pos.Z)
	if got := srv.LoadBlockInDimension(world.DimensionName, x, y, z); got != portalX {
		t.Fatalf("the return stands in block %d at (%d, %d, %d), want the original portal's",
			got, x, y, z)
	}
	if x < servertest.PortalInnerX0 || x > servertest.PortalInnerX1 || y != servertest.PortalFloorY || z != servertest.PortalInnerZ {
		t.Errorf("the return arrived at (%d, %d, %d), want the original portal's interior at its foot",
			x, y, z)
	}
}

// crossIntoPreparedNether stands two-block lit portal columns (foot at
// PortalFloorY) in the nether at the columns given, sends the player through
// the overworld portal and returns the block their feet are in on arrival,
// and whether it is a portal block. The arrival's search is centred on
// (0, PortalFloorY, 0): the interior's x 4..5 and z 4 scaled by 1/8.
func crossIntoPreparedNether(t *testing.T, name string, columns [][2]int32) (x, y, z int32, inPortal bool) {
	t.Helper()
	cfg := servertest.NetherTestConfig(t, servertest.BlockTestConfig())
	srv, c := servertest.Join(t, cfg, name)

	_, portalX, _, _ := servertest.PortalStates(t)
	for _, col := range columns {
		t.Cleanup(srv.HoldChunkInDimension(world.NetherName, col[0], col[1]))
		for dy := range int32(2) {
			if !srv.SetBlockInDimension(world.NetherName, col[0], servertest.PortalFloorY+dy, col[1], portalX) {
				t.Fatalf("placing a nether portal block at (%d, %d) failed", col[0], col[1])
			}
		}
	}
	servertest.BuildFrame(t, srv)
	servertest.Flint(t, c)
	c.PlaceBlock(servertest.PortalInnerX0, servertest.PortalFloorY-1, servertest.PortalInnerZ, protocol.FaceTop)
	c.Settle()

	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ+0.5, 0, 0)
	c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.PlayerDimension(name) == world.NetherName
	})
	pos := srv.PlayerPosition(name)
	x, y, z = blockOfFloor(pos.X), blockOfFloor(pos.Y), blockOfFloor(pos.Z)
	return x, y, z, srv.LoadBlockInDimension(world.NetherName, x, y, z) == portalX
}

// TestArrivalBuildsPastAPortalOutsideTheSquare checks the reach of the
// arrival's search into the nether: a lit portal 20 blocks along X from its
// centre is outside the 16-block square, so the crossing builds a portal of
// its own.
// vanilla: PortalForcer.findClosestPortalPosition
func TestArrivalBuildsPastAPortalOutsideTheSquare(t *testing.T) {
	t.Parallel()
	x, y, z, inPortal := crossIntoPreparedNether(t, "Faraway", [][2]int32{{20, 0}})
	if x == 20 && z == 0 {
		t.Fatalf("arrived at (%d, %d, %d), in the portal 20 blocks out; want a new one", x, y, z)
	}
	if !inPortal {
		t.Errorf("arrived at (%d, %d, %d), not in a portal block; want in the one built", x, y, z)
	}
}

// TestArrivalTakesTheNearestPortal checks the choice between two portals in
// the square: the one 10 blocks out wins over the one 14 blocks out, though
// the farther one stands in the chunk of the search's centre.
// vanilla: PortalForcer.findClosestPortalPosition
func TestArrivalTakesTheNearestPortal(t *testing.T) {
	t.Parallel()
	x, y, z, inPortal := crossIntoPreparedNether(t, "Chooser", [][2]int32{{0, 14}, {-10, 0}})
	if x != -10 || y != servertest.PortalFloorY || z != 0 || !inPortal {
		t.Errorf("arrived at (%d, %d, %d) (in a portal: %v), want the foot of the portal 10 blocks out at (-10, %d, 0)",
			x, y, z, inPortal, servertest.PortalFloorY)
	}
}

// TestEndPortalGoesBothWays checks the way into the end and the way out: an
// end portal block takes whoever stands in it to the end's arrival — its
// platform in place under them — and in the end the same block holds the end
// credits over the way back, whose ending respawns the player to their own
// spawn. Both blocks are placed by the test, there being no item or frame
// here to make one from.
func TestEndPortalGoesBothWays(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Ender")

	endPortal := srv.EndPortalState()
	arrival := world.EndArrival()
	obsidian, _, _, _ := servertest.PortalStates(t)

	// An end portal in the overworld, a few steps out for the fall into it.
	if !srv.SetBlock(10, servertest.PortalFloorY-1, 10, endPortal) {
		t.Fatal("placing the end portal block failed")
	}

	c.MoveTo(10.5, servertest.PortalFloorY-1, 10.5, 0, 0)
	// The crossing: a Respawn naming the end, and the stream of it read
	// through the way a client does.
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn)
	if !ok {
		t.Fatal("no respawn arrived for standing in the end portal")
	}
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != registry.MustID("minecraft:dimension_type", world.EndType) {
		t.Errorf("respawn dimension type = %d, want the end's", got)
	}
	if got := r.ReadString(32767); got != world.EndName {
		t.Errorf("respawn dimension = %q, want %q", got, world.EndName)
	}
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the end never arrived around the crossing")
	}
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.PlayerDimension("Ender") == world.EndName
	})

	// The arrival, at the platform: the obsidian of it under the feet.
	pos := srv.PlayerPosition("Ender")
	if pos.X != float64(arrival.X)+0.5 || pos.Y != float64(arrival.Y) || pos.Z != float64(arrival.Z)+0.5 {
		t.Errorf("end arrival = (%v, %v, %v), want the arrival platform (%v, %v, %v)",
			pos.X, pos.Y, pos.Z, float64(arrival.X)+0.5, arrival.Y, float64(arrival.Z)+0.5)
	}
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.LoadBlockInDimension(world.EndName, arrival.X, arrival.Y-1, arrival.Z) == obsidian
	})

	// The way back: an end portal block in the end, stood in, holds the
	// credits on the first exit — the client rolls them and ends them with
	// the respawn request, which takes the player to their spawn — this
	// player has none of their own, so the world's. The portal goes into
	// the arrival cell the feet are standing in, once the chunks around
	// the arrival have loaded far enough to take it.
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.BlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z) == 0 &&
			srv.BlockInDimension(world.EndName, arrival.X, arrival.Y-1, arrival.Z) == obsidian
	})
	if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z, endPortal) {
		t.Fatal("placing the end's own end portal failed")
	}
	servertest.ExpectEndCredits(t, c, srv)
	c.PerformRespawn()
	servertest.WaitFor(t, srv, 400, func() bool {
		return srv.PlayerDimension("Ender") == world.DimensionName
	})
	// The way back's own stream, read through too.
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn arrived for the way back from the end")
	}
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the overworld never arrived around the way back")
	}
	back := srv.PlayerPosition("Ender")
	if back.X != 0.5 || back.Y != 64 || back.Z != 0.5 {
		t.Errorf("the return from the end = (%v, %v, %v), want the world spawn (0.5, 64, 0.5)",
			back.X, back.Y, back.Z)
	}
}

// TestALateEndPortalTravelLeavesTheBodyWhereItLanded runs a travel fired by
// an end portal in the end only once the body is home in the overworld, the
// order a loaded machine can run a second firing in: it must do nothing,
// not take the overworld for the way in and send the body back to the end.
func TestALateEndPortalTravelLeavesTheBodyWhereItLanded(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Ender")

	srv.TravelByEndPortalFrom("Ender", world.EndName)
	c.Settle()
	if got := srv.PlayerDimension("Ender"); got != world.DimensionName {
		t.Fatalf("a travel fired in the end took a body in the overworld to %s", got)
	}
}

// TestEndPortalReturnUsesTheBedStandUp checks the way back from the end runs
// the same stand-up search a death at the bed would: a player with a bed
// spawn comes back beside the bed rather than inside it. vanilla:
// EndPortalBlock calls findRespawnPositionAndUseSpawnBlock.
func TestEndPortalReturnUsesTheBedStandUp(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Ender")

	servertest.StandABedAndSetTheSpawn(t, srv, c)

	endPortal := srv.EndPortalState()
	arrival := world.EndArrival()
	obsidian, _, _, _ := servertest.PortalStates(t)

	// An end portal in the overworld, a few steps out for the fall into it.
	if !srv.SetBlock(10, servertest.PortalFloorY-1, 10, endPortal) {
		t.Fatal("placing the end portal block failed")
	}
	c.MoveTo(10.5, servertest.PortalFloorY-1, 10.5, 0, 0)
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn arrived for standing in the end portal")
	}
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the end never arrived around the crossing")
	}
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.PlayerDimension("Ender") == world.EndName
	})
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.LoadBlockInDimension(world.EndName, arrival.X, arrival.Y-1, arrival.Z) == obsidian
	})

	// The way back: an end portal block in the end takes the player to their
	// bed's stand-up cell, loaded back from the overworld the walk away saved —
	// held on the end credits until its respawn request ends them.
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.BlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z) == 0 &&
			srv.BlockInDimension(world.EndName, arrival.X, arrival.Y-1, arrival.Z) == obsidian
	})
	if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z, endPortal) {
		t.Fatal("placing the end's own end portal failed")
	}
	servertest.ExpectEndCredits(t, c, srv)
	c.PerformRespawn()
	servertest.WaitFor(t, srv, 400, func() bool {
		return srv.PlayerDimension("Ender") == world.DimensionName
	})
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn arrived for the way back from the end")
	}
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the overworld never arrived around the way back")
	}
	back := srv.PlayerPosition("Ender")
	if back.X != 4.5 || back.Y != 64 || back.Z != 1.5 {
		t.Errorf("the return from the end = (%v, %v, %v), want the bed's stand-up cell (4.5, 64, 1.5)",
			back.X, back.Y, back.Z)
	}
}

// TestEndPortalReturnWithoutABedSendsNoRespawnBlock breaks the bed that holds
// the player's spawn point, takes the End portal home, and checks the
// missing-block path: the client is told no respawn block is available and the
// body comes back at the world spawn. vanilla: ServerPlayer.teleport
func TestEndPortalReturnWithoutABedSendsNoRespawnBlock(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Ender")

	servertest.StandABedAndSetTheSpawn(t, srv, c)

	// Both halves go, so the point's cell reads as gone by the way back.
	if !srv.SetBlock(5, 64, 0, 0) || !srv.SetBlock(5, 64, 1, 0) {
		t.Fatal("could not break the bed")
	}

	endPortal := srv.EndPortalState()
	arrival := world.EndArrival()
	obsidian, _, _, _ := servertest.PortalStates(t)

	// An end portal in the overworld, a few steps out for the fall into it.
	if !srv.SetBlock(10, servertest.PortalFloorY-1, 10, endPortal) {
		t.Fatal("placing the end portal block failed")
	}
	c.MoveTo(10.5, servertest.PortalFloorY-1, 10.5, 0, 0)
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn arrived for standing in the end portal")
	}
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the end never arrived around the crossing")
	}
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.PlayerDimension("Ender") == world.EndName
	})
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.LoadBlockInDimension(world.EndName, arrival.X, arrival.Y-1, arrival.Z) == obsidian
	})

	// The way back: an end portal block in the end, with the bed gone.
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.BlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z) == 0 &&
			srv.BlockInDimension(world.EndName, arrival.X, arrival.Y-1, arrival.Z) == obsidian
	})
	if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z, endPortal) {
		t.Fatal("placing the end's own end portal failed")
	}
	servertest.ExpectEndCredits(t, c, srv)
	c.PerformRespawn()
	back, noBlock := c.ExpectRespawnThroughTeleport()
	if !noBlock {
		t.Error("the return from the end without a bed did not send the no-respawn-block game event")
	}
	if back.X != 0.5 || back.Y != 64 || back.Z != 0.5 {
		t.Errorf("the return from the end = (%v, %v, %v), want the world spawn (0.5, 64, 0.5)",
			back.X, back.Y, back.Z)
	}
}

// TestTheEndsExitPortalShowsTheCreditsFirst checks the first exit by the
// end's own portal: a player who has not seen the credits gets the WIN_GAME
// event and stays in the end, out of the world until the Client Command the
// credits end with respawns them — a living respawn, keeping everything — to
// their respawn point.
// vanilla: EndPortalBlock.entityInside, ServerPlayer.showEndCredits,
// PlayerList.respawn(player, keepAllPlayerData=true)
func TestTheEndsExitPortalShowsTheCreditsFirst(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, c := servertest.Join(t, cfg, "Ender")

	arrival := servertest.EnterTheEndByItsPortal(t, srv, c, "Ender")
	endPortal := srv.EndPortalState()

	// The exit portal goes into the cell the feet are standing in.
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.BlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z) == 0
	})
	if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z, endPortal) {
		t.Fatal("placing the end's own end portal failed")
	}

	servertest.ExpectEndCredits(t, c, srv)
	// The player stays in the end: no Respawn crosses them out while the
	// credits hold them.
	c.ExpectNoneWithinTicks(srv, 40, servertest.PacketIs(protocol.IDPlayRespawn))
	if got := srv.PlayerDimension("Ender"); got != world.EndName {
		t.Errorf("dimension while the credits hold = %q, want %q", got, world.EndName)
	}
	// The body is out of the world, so its steps go nowhere.
	c.MoveTo(60.5, servertest.PortalFloorY, 60.5, 0, 0)
	servertest.AwaitTicks(t, srv, 2)
	pos := srv.PlayerPosition("Ender")
	if pos.X != float64(arrival.X)+0.5 || pos.Y != float64(arrival.Y) || pos.Z != float64(arrival.Z)+0.5 {
		t.Errorf("a step while the credits hold moved the body to (%v, %v, %v), want the arrival platform",
			pos.X, pos.Y, pos.Z)
	}

	// The command that ends the credits is a respawn that keeps
	// everything a living body holds, to the world spawn this player has.
	c.PerformRespawn()
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn)
	if !ok {
		t.Fatal("no respawn arrived for the command that ends the credits")
	}
	servertest.CheckRespawn(t, data,
		registry.MustID("minecraft:dimension_type", world.DimensionType), world.DimensionName,
		-63, 0x01, "the credits' respawn")
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the overworld never arrived behind the credits")
	}
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.PlayerDimension("Ender") == world.DimensionName
	})
	back := srv.PlayerPosition("Ender")
	if back.X != 0.5 || back.Y != 64 || back.Z != 0.5 {
		t.Errorf("the credits' respawn = (%v, %v, %v), want the world spawn (0.5, 64, 0.5)",
			back.X, back.Y, back.Z)
	}
}

// TestTheEndCreditsAreSavedAndSkippedAfterARejoin checks the credits'
// persistence: the file a credits-showing player leaves holds the
// seenCredits byte, and a player who rejoins with it gets no credits and
// the portal's crossing out straight away.
// vanilla: ServerPlayer.addAdditionalSaveData's seenCredits
func TestTheEndCreditsAreSavedAndSkippedAfterARejoin(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, connect := servertest.Start(t, cfg)
	c := connect().Join("Ender")

	arrival := servertest.EnterTheEndByItsPortal(t, srv, c, "Ender")
	endPortal := srv.EndPortalState()

	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.BlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z) == 0
	})
	if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z, endPortal) {
		t.Fatal("placing the end's own end portal failed")
	}
	servertest.ExpectEndCredits(t, c, srv)
	c.PerformRespawn()
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn arrived for the command that ends the credits")
	}
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the overworld never arrived behind the credits")
	}
	c.Disconnect()

	// The file remembers the credits.
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Ender"))
	seen, ok := nbt.Get[nbt.Byte](root, "seenCredits")
	if !ok || seen != 1 {
		t.Errorf("saved seenCredits = %v (present: %v), want 1", seen, ok)
	}

	// The rejoin exits directly: the portal crosses the player out with no
	// credits event in front of the Respawn.
	c = connect()
	c.Join("Ender")
	arrival = servertest.EnterTheEndByItsPortal(t, srv, c, "Ender")
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.BlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z) == 0
	})
	// Held from the crossing tick on: the travel builds the Respawn on its own
	// goroutine, and on a free loop a slow one finds the cooldown run out.
	// The arrival's own cooldown runs out first, so the crossing is the one
	// that arms it.
	servertest.HoldTicks(t, srv)
	servertest.UntilTicks(t, srv, 20, "the arrival's portal cooldown to run out", func() bool {
		return srv.PortalCooldown("Ender") == 0
	})
	if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z, endPortal) {
		t.Fatal("placing the end's own end portal failed")
	}
	servertest.UntilTicks(t, srv, 20, "the end portal's crossing", func() bool {
		return srv.PortalCooldown("Ender") > 0
	})
	var respawnData []byte
	credited := false
	if !c.Pump(servertest.ExpectTimeout, nil, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayGameEvent:
			credited = credited || protocol.NewReader(data).ReadU8() == protocol.GameEventWinGame
		case protocol.IDPlayRespawn:
			respawnData = data
			return true
		}
		return false
	}) {
		t.Fatal("no respawn arrived for the second exit from the end")
	}
	if credited {
		t.Error("the rejoin was shown the credits a second time, want the exit direct")
	}
	// The crossing arms the player's 10-tick cooldown, and with no tick
	// between it and the Respawn the direct way out carries all of it, where
	// a command teleport's Respawn carries an exact zero.
	r := protocol.NewReader(respawnData)
	if got := r.ReadVarInt(); got != registry.MustID("minecraft:dimension_type", world.DimensionType) {
		t.Errorf("the direct way out respawn dimension type = %d, want the overworld's", got)
	}
	if got := r.ReadString(32767); got != world.DimensionName {
		t.Errorf("the direct way out respawn dimension = %q, want %q", got, world.DimensionName)
	}
	r.ReadI64()    // hashed seed
	r.ReadVarInt() // game mode
	r.ReadVarInt() // previous game mode
	r.ReadBool()   // debug
	if !r.ReadBool() {
		t.Error("the direct way out respawn did not flag its superflat world")
	}
	if r.ReadBool() {
		t.Error("the direct way out respawn carried a death location")
	}
	if cooldown := r.ReadVarInt(); cooldown != 10 {
		t.Errorf("the direct way out respawn portal cooldown = %d, want the player's 10-tick cooldown", cooldown)
	}
	if got := r.ReadVarInt(); got != -63 {
		t.Errorf("the direct way out respawn sea level = %d, want -63", got)
	}
	if got := r.ReadU8(); got != 0x03 {
		t.Errorf("the direct way out respawn data kept = %#02x, want 0x03", got)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("the direct way out respawn: %v", err)
	}
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.PlayerDimension("Ender") == world.DimensionName
	})
}

// TestARiderInTheEndPortalGetsNoCredits: a player riding a boat into the
// end's own portal is shown nothing at all — no credits and no crossing —
// because the passenger cannot use the portal and the boat does not leave
// the end with a passenger who has not seen the credits.
// vanilla: EndPortalBlock.entityInside, Entity.canTeleport
func TestARiderInTheEndPortalGetsNoCredits(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, c := servertest.Join(t, cfg, "Rider")

	arrival := servertest.EnterTheEndByItsPortal(t, srv, c, "Rider")
	endPortal := srv.EndPortalState()

	// The boat is stood in the portal's cell and mounted with the loop
	// held, so the portal does not take the rider mid-setup.
	srv.StopTick()
	boatID := srv.SpawnBoatInDimension(world.EndName,
		float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+0.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)
	if _, ok := srv.BoatRider(boatID); !ok {
		t.Fatal("the boat holds no rider")
	}
	if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z, endPortal) {
		t.Fatal("placing the end's own end portal failed")
	}
	srv.StartTick()

	// Nothing happens: no WIN_GAME event, and the rider stays in the end.
	credited := false
	c.ReadTicksSkipping(srv, 60, nil, func(id int32, data []byte) bool {
		if id == protocol.IDPlayGameEvent && protocol.NewReader(data).ReadU8() == protocol.GameEventWinGame {
			credited = true
		}
		return false
	})
	if credited {
		t.Error("a rider on the end's portal was shown the credits, want it left alone")
	}
	if got := srv.PlayerDimension("Rider"); got != world.EndName {
		t.Errorf("the rider left the end for %q, want to stay", got)
	}
}

// TestABoatLeavingTheEndGoesToTheWorldSpawnColumnsSurface checks a boat whose rider has
// seen the credits crosses the end's exit portal to the world spawn column's surface, rider
// aboard, rather than to the rider's bed, and without the missing-block
// event the rider's own respawn search would send.
// vanilla: EndPortalBlock.getPortalDestination, Entity.adjustSpawnLocation
func TestABoatLeavingTheEndGoesToTheWorldSpawnColumnsSurface(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, c := servertest.Join(t, cfg, "Rower")

	servertest.StandABedAndSetTheSpawn(t, srv, c)
	// A stored spawn Y above the surface (63) does not place the body in the air.
	spawn := srv.WorldSpawn()
	spawn.Y = 100
	srv.SetWorldSpawn(spawn)
	arrival := servertest.EnterTheEndByItsPortal(t, srv, c, "Rower")
	srv.SetSeenCredits("Rower", true)
	endPortal := srv.EndPortalState()

	// Set up with the loop held, so the portal does not fire mid-setup.
	srv.StopTick()
	boatID := srv.SpawnBoatInDimension(world.EndName,
		float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+0.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)
	if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z, endPortal) {
		t.Fatal("placing the end's own end portal failed")
	}
	srv.StartTick()

	back, noBlock := c.ExpectRespawnThroughTeleport()
	if noBlock {
		t.Error("a boat leaving the end sent the rider the no-respawn-block game event")
	}
	if back.X != 0.5 || back.Y != 65 || back.Z != 0.5 {
		t.Errorf("the rider arrived at (%v, %v, %v), want the spawn column's surface (0.5, 65, 0.5)", back.X, back.Y, back.Z)
	}
	if got := srv.PlayerDimension("Rower"); got != world.DimensionName {
		t.Errorf("the rider is in %q, want %q", got, world.DimensionName)
	}
	if n := srv.BoatCountInDimension(world.DimensionName); n != 1 {
		t.Errorf("%d boats in the overworld, want the one that crossed", n)
	}
	if x, y, z, _, _, ok := srv.BoatState(boatID); !ok || x != 0.5 || y != 65 || z != 0.5 {
		t.Errorf("the boat stands at (%v, %v, %v) (found %v), want the spawn column's surface (0.5, 65, 0.5)", x, y, z, ok)
	}
	if rider, ok := srv.BoatRider(boatID); !ok || rider != "Rower" {
		t.Errorf("the boat's rider = %q (%v), want Rower still seated", rider, ok)
	}
}

// TestEndArrivalPlatformWakesItsNeighbours checks that the blocks the end's
// arrival platform clears are changes the physics hears about: a torch
// standing on a block the platform clears breaks with it.
func TestEndArrivalPlatformWakesItsNeighbours(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, connect := servertest.Start(t, cfg)

	// A player in the end keeps the platform's chunks loaded for the setup.
	watcher := connect().Join("Watcher")
	arrival := world.EndArrival()
	srv.MovePlayerToDimension("Watcher", world.EndName, float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+0.5)
	if _, ok := servertest.ReadUntilViewSent(t, watcher); !ok {
		t.Fatal("the end never arrived for the watcher")
	}

	endStone, ok := registry.FindState("minecraft:end_stone", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:end_stone")
	}
	torch, ok := registry.FindState("minecraft:torch", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:torch")
	}
	// The top corner of the space the platform clears, and a torch on it.
	sx, sy, sz := arrival.X+2, arrival.Y+2, arrival.Z+2
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.SetBlockInDimension(world.EndName, sx, sy, sz, endStone)
	})
	if !srv.SetBlockInDimension(world.EndName, sx, sy+1, sz, torch) {
		t.Fatal("placing the torch failed")
	}

	ender := connect().Join("Ender")
	if !srv.SetBlock(10, servertest.PortalFloorY-1, 10, srv.EndPortalState()) {
		t.Fatal("placing the end portal block failed")
	}
	ender.MoveTo(10.5, servertest.PortalFloorY-1, 10.5, 0, 0)
	servertest.WaitFor(t, srv, 400, func() bool {
		return srv.PlayerDimension("Ender") == world.EndName
	})

	if got := srv.BlockInDimension(world.EndName, sx, sy, sz); got != 0 {
		t.Fatalf("the cleared cell = %d, want air", got)
	}
	servertest.WaitFor(t, srv, 100, func() bool {
		return srv.BlockInDimension(world.EndName, sx, sy+1, sz) == 0
	})
}

// TestPlayersAndItemsAcrossDimensions checks who sees what across a
// dimensional divide: the players and the items are of the dimension they
// are in, and only of it — another dimension's bodies do not arrive, its
// items lie unseen and its steps are not reported — while the tab list stays
// one for the whole server.
func TestPlayersAndItemsAcrossDimensions(t *testing.T) {
	t.Parallel()
	// Entity visibility needs neither structures nor a large terrain view;
	// bound generation so the other client's keep-alives do not wait on it.
	cfg := servertest.CrossingTestConfig(t)
	srv, connect := servertest.Start(t, cfg)

	// The watcher, who stays in the overworld and sees who goes.
	watcher := connect().Join("Watcher")

	// The mover, whose arrival the watcher is told about.
	mover := connect()
	moverID := mover.JoinAs("Mover")
	// The watcher hears the body first and the announcement after, the
	// order the join sends them in.
	if got := watcher.ExpectPlayerAdded("Mover"); got != moverID {
		t.Fatalf("entity ID disagreement: watcher saw %d, mover is %d", got, moverID)
	}
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Mover")

	// Off to the nether. The watcher sees the body go — and the tab list
	// keep the player, the removal arriving without the player-info removal
	// that a departure from the server would have carried. The mover reads
	// its own stream through, as a client does, so the server is not left
	// holding chunks nobody is reading.
	srv.MovePlayerToDimension("Mover", world.NetherName, 88, 40, 88)
	if _, ok := servertest.ReadUntilViewSent(t, mover); !ok {
		t.Fatal("the nether never arrived for the mover")
	}

	remove, ok := servertest.ReadUntil(t, watcher, servertest.ExpectTimeout, protocol.IDPlayRemoveEntities)
	if !ok {
		t.Fatal("the watcher was not told the mover's body left their dimension")
	}
	rr := protocol.NewReader(remove)
	if got := rr.ReadVarInt(); got != 1 {
		t.Fatalf("removed entities = %d, want 1", got)
	}
	if got := rr.ReadVarInt(); got != moverID {
		t.Errorf("removed entity = %d, want %d", got, moverID)
	}
	watcher.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlayPlayerInfoRemove))

	// The mover moves and drops things, out of sight: no step of theirs is
	// reported to the watcher, and no item of theirs arrives for them.
	mover.MoveTo(88.5, 40, 88.5, 0, 0)
	watcher.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlayEntityPosition))

	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the item registry has no minecraft:stone")
	}
	mover.SetCreativeSlot(36, protocol.ItemStack{ItemID: stone, Count: 1})
	servertest.DropHeld(mover, 88, 40, 88)
	// The mover's own drop arrives for the mover — an item of the nether
	// for the player in it — and is read before its absence elsewhere is
	// asserted, so the check is not looking at what has not arrived yet.
	if _, ok := servertest.ReadUntil(t, mover, servertest.ExpectTimeout, protocol.IDPlaySpawnEntity); !ok {
		t.Fatal("the mover's own drop never arrived for the mover")
	}
	watcher.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlaySpawnEntity))

	// And the other way round: what the watcher drops stays in the
	// overworld, and the mover hears nothing of it.
	watcher.SetCreativeSlot(36, protocol.ItemStack{ItemID: stone, Count: 1})
	servertest.DropHeld(watcher, 2, 64, 2)
	if _, ok := servertest.ReadUntil(t, watcher, servertest.ExpectTimeout, protocol.IDPlaySpawnEntity); !ok {
		t.Fatal("the watcher's own drop never arrived for the watcher")
	}
	mover.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlaySpawnEntity))

	// The mover comes home: the body comes back in front of the watcher,
	// without a word to the tab list, which never lost them. Both sides
	// read their streams through the way clients do.
	srv.MovePlayerToDimension("Mover", world.DimensionName, 4, 64, 4)
	if _, ok := servertest.ReadUntilViewSent(t, mover); !ok {
		t.Fatal("the overworld never arrived for the mover's return")
	}
	data, ok := servertest.ReadUntil(t, watcher, servertest.ExpectTimeout, protocol.IDPlaySpawnEntity)
	if !ok {
		t.Fatal("the watcher was not told the mover's body came back")
	}
	back := protocol.NewReader(data)
	if got := back.ReadVarInt(); got != moverID {
		t.Errorf("the returning body has entity %d, want %d", got, moverID)
	}
	watcher.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlayPlayerInfoUpdate))
}

// TestSetblockBuildsAPortalOneBlockAtATime checks that portal blocks placed
// one by one, as /setblock places them, stay where they are put: the vanilla
// portal block only reacts to a neighbour changing, and a neighbour that is
// portal or obsidian keeps it standing, so a portal built a block at a time
// inside its frame never collapses half-way. This is how the real client was
// first seen to lose its portal here.
func TestSetblockBuildsAPortalOneBlockAtATime(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Builder", servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID: protocol.OfflineUUID("Builder").String(), Name: "Builder", Level: 4,
	}))

	_, portalX, _, _ := servertest.PortalStates(t)
	servertest.BuildFrame(t, srv)

	for _, cell := range servertest.FrameInterior() {
		c.ChatCommand(fmt.Sprintf("setblock %d %d %d minecraft:nether_portal[axis=x]", cell[0], cell[1], cell[2]))
		c.Settle()
	}
	for _, cell := range servertest.FrameInterior() {
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got != portalX {
			t.Errorf("portal block at (%d, %d, %d) = %d, want %d: it collapsed", cell[0], cell[1], cell[2], got, portalX)
		}
	}
}

// crossTo reads a crossing's Respawn and the destination's chunk stream
// through, the way a client does, and waits for the player to be in the
// dimension named.
func crossTo(t *testing.T, srv *server.Server, c *servertest.Client, name, want string) {
	t.Helper()
	if srv.TickEnded() == nil {
		c.AwaitPacket(protocol.IDPlayRespawn)
		servertest.AwaitViewSent(t, c)
	} else {
		c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
		c.ExpectViewWithinTicks(srv, 200)
	}
	if got := srv.PlayerDimension(name); got != want {
		t.Fatalf("crossed to %q, want %q", got, want)
	}
}

// stepOutThenBack crosses, steps one block out of the arrival portal, waits
// the cooldown out, and steps back in: the portal must take them again.
// Standing in the portal restarts the cooldown, so the crossing only happens
// once the out wait has run out. The wait is 30 ticks: three times a player's
// 10-tick cooldown, and a tenth of the 300 a non-player entity carries, which
// is what makes the difference between the two the test pins.
func stepOutThenBack(t *testing.T, srv *server.Server, c *servertest.Client, name string, standTicks int) {
	t.Helper()
	crossTo(t, srv, c, name, world.NetherName)

	arrival := srv.PlayerPosition(name)
	out := arrival
	out.Z++
	c.MoveTo(out.X, out.Y, out.Z, 0, 0)
	c.Settle()
	if got := srv.PlayerPosition(name).Z; got != out.Z {
		t.Fatalf("the step out left the player at z %v, want %v", got, out.Z)
	}
	for range 30 {
		srv.RunTick()
	}

	c.MoveTo(arrival.X, arrival.Y, arrival.Z, 0, 0)
	c.Settle()
	for range standTicks {
		srv.TickPortals()
	}
	crossTo(t, srv, c, name, world.DimensionName)
}

// TestCreativePlayerReturnsAfterTheShortCooldown checks that a creative
// player arriving through a nether portal is held only by the player's
// 10-tick portal cooldown (vanilla: Player.getDimensionChangingDelay), not
// the 300 a non-player entity carries (Entity.getDimensionChangingDelay):
// stepping half a second out of the arrival portal lets it take them back.
func TestCreativePlayerReturnsAfterTheShortCooldown(t *testing.T) {
	t.Parallel()
	cfg := servertest.NetherTestConfig(t, servertest.BlockTestConfig())
	srv, c := servertest.Join(t, cfg, "Returner")

	servertest.PortalFrame(t, srv)
	srv.StopTick()
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ+0.5, 0, 0)

	c.Settle()
	srv.TickPortals()

	stepOutThenBack(t, srv, c, "Returner", 1)
}

// TestSurvivalCrossesOnTheEightyFirstPortalTick pins the survival wait: a
// player who stands in a nether portal crosses on the 81st tick inside, not
// the 80th. vanilla: PortalProcessor.processPortalTeleportation compares the
// count before its add (portalTime++ >= 80), and NetherPortalBlock gives a
// survival player a transition of 80, a creative one 0.
func TestSurvivalCrossesOnTheEightyFirstPortalTick(t *testing.T) {
	t.Parallel()
	cfg := servertest.NetherTestConfig(t, servertest.SurvivalTestConfig())
	srv, c := servertest.Join(t, cfg, "Returner")

	servertest.PortalFrame(t, srv)
	// The loop goes first, so no tick of its own counts a standing tick
	// between the manual ones.
	srv.StopTick()
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ+0.5, 0, 0)
	c.Settle()

	const standTicks = 80
	for range standTicks {
		srv.TickPortals()
	}
	if got := srv.PortalTime("Returner"); got != standTicks {
		t.Fatalf("the count after %d ticks inside = %d: survival crossed on the 80th", standTicks, got)
	}
	srv.TickPortals()
	if got := srv.PortalTime("Returner"); got != standTicks+1 {
		t.Fatalf("the count after the 81st tick = %d, want the kept %d", got, standTicks+1)
	}
	crossTo(t, srv, c, "Returner", world.NetherName)
}

// TestPortalDefaultDelayRuleSetsTheSurvivalWait pins that a survival player
// waits players_nether_portal_default_delay ticks, not a fixed 80.
// vanilla: NetherPortalBlock.getPortalTransitionTime
func TestPortalDefaultDelayRuleSetsTheSurvivalWait(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.NetherTestConfig(t, servertest.SurvivalTestConfig()), "Returner")

	servertest.PortalFrame(t, srv)
	srv.StopTick()
	srv.SetGameRuleValue("players_nether_portal_default_delay", 3)
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ+0.5, 0, 0)
	c.Settle()

	for range 3 {
		srv.TickPortals()
	}
	if got := srv.PlayerDimension("Returner"); got == world.NetherName {
		t.Fatal("crossed before the delay rule's 3 ticks had passed")
	}
	srv.TickPortals()
	crossTo(t, srv, c, "Returner", world.NetherName)
}

// TestSurvivalKeepsMostOfItsPortalProgressWhenItStepsOut pins the decay
// outside the portal: a survival player who stands 60 ticks, steps out 2 and
// back in crosses after the remaining 28 ticks, not from scratch, because
// each tick outside takes 4 off the count rather than clearing it. vanilla:
// PortalProcessor.decayTick.
func TestSurvivalKeepsMostOfItsPortalProgressWhenItStepsOut(t *testing.T) {
	t.Parallel()
	cfg := servertest.NetherTestConfig(t, servertest.SurvivalTestConfig())
	srv, c := servertest.Join(t, cfg, "Returner")

	servertest.PortalFrame(t, srv)
	srv.StopTick()
	arrX, arrY, arrZ := float64(servertest.PortalInnerX0)+0.5, float64(servertest.PortalFloorY), float64(servertest.PortalInnerZ)+0.5
	c.MoveTo(arrX, arrY, arrZ, 0, 0)
	c.Settle()

	const stood = 60
	for range stood {
		srv.TickPortals()
	}
	// Out for two ticks: the count falls from 60 to 52, not to 0.
	c.MoveTo(arrX, arrY, arrZ+1, 0, 0)
	c.Settle()
	srv.TickPortals()
	srv.TickPortals()
	if got := srv.PortalTime("Returner"); got != stood-8 {
		t.Fatalf("the count after two ticks outside = %d, want the decayed %d", got, stood-8)
	}
	if got := srv.PlayerDimension("Returner"); got != world.DimensionName {
		t.Fatal("the player crossed while standing outside the portal")
	}
	// Back in: the 28 ticks left take them, on the 29th add.
	c.MoveTo(arrX, arrY, arrZ, 0, 0)
	c.Settle()
	for range 28 {
		srv.TickPortals()
	}
	if got := srv.PortalTime("Returner"); got != 80 {
		t.Fatalf("the count after the remaining ticks = %d, want 80 (the 80th tick inside)", got)
	}
	srv.TickPortals()
	crossTo(t, srv, c, "Returner", world.NetherName)
}

// TestSurvivalKeepsItsPortalCountAcrossTheCrossing pins the count surviving
// the teleport: a survival player carries the 81 ticks of standing through
// the crossing (vanilla: ServerPlayer.restoreFrom keeps portalProcess), so
// after stepping out to spend the arrival cooldown they cross back on the
// 40th tick inside — 81 decayed to 41 by ten ticks out — not 80 later.
func TestSurvivalKeepsItsPortalCountAcrossTheCrossing(t *testing.T) {
	t.Parallel()
	cfg := servertest.NetherTestConfig(t, servertest.SurvivalTestConfig())
	srv, c := servertest.Join(t, cfg, "Returner")

	servertest.PortalFrame(t, srv)
	srv.StopTick()
	arrX, arrY, arrZ := float64(servertest.PortalInnerX0)+0.5, float64(servertest.PortalFloorY), float64(servertest.PortalInnerZ)+0.5
	c.MoveTo(arrX, arrY, arrZ, 0, 0)
	c.Settle()
	for range 81 {
		srv.TickPortals()
	}
	// Read the crossing's Respawn and chunks through, which accepts its
	// teleport; the loop is stopped, so nothing else advances the clock.
	crossTo(t, srv, c, "Returner", world.NetherName)
	if got := srv.PortalTime("Returner"); got != 81 {
		t.Fatalf("the count after the crossing = %d, want the kept 81", got)
	}

	// Out of the arrival portal for the 10-tick cooldown: 81 decays by four a
	// tick to 41.
	arrival := srv.PlayerPosition("Returner")
	out := arrival
	out.Z++
	c.MoveTo(out.X, out.Y, out.Z, 0, 0)
	c.Settle()
	if got := srv.PlayerPosition("Returner").Z; got != out.Z {
		t.Fatalf("the move left the player at z %v, want %v", got, out.Z)
	}
	for range 10 {
		srv.TickPortals()
	}
	if got := srv.PortalTime("Returner"); got != 41 {
		t.Fatalf("the count after ten ticks out = %d, want the decayed 41", got)
	}

	// Back in with no cooldown left: the 39 ticks to 80 do not take them, the
	// 40th does.
	c.MoveTo(arrival.X, arrival.Y, arrival.Z, 0, 0)
	c.Settle()
	if got := srv.PlayerPosition("Returner").Z; got != arrival.Z {
		t.Fatalf("the move left the player at z %v, want %v", got, arrival.Z)
	}
	for range 39 {
		srv.TickPortals()
	}
	if got := srv.PlayerDimension("Returner"); got != world.NetherName {
		t.Fatal("the player crossed before the retained count reached 80")
	}
	srv.TickPortals()
	crossTo(t, srv, c, "Returner", world.DimensionName)
}

// TestSurvivalPlayerReturnsAfterTheShortCooldown is the survival form: the
// departure needs 80 ticks of standing (the nether portal's default delay),
// but the cooldown it arrives with is still the player's 10, so the same
// portal takes them back once they have stepped out for half a second.
func TestSurvivalPlayerReturnsAfterTheShortCooldown(t *testing.T) {
	t.Parallel()
	cfg := servertest.NetherTestConfig(t, servertest.SurvivalTestConfig())
	srv, c := servertest.Join(t, cfg, "Returner")

	servertest.PortalFrame(t, srv)
	srv.StopTick()
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ+0.5, 0, 0)

	c.Settle()
	for range 81 {
		srv.TickPortals()
	}

	stepOutThenBack(t, srv, c, "Returner", 81)
}

// lookAroundInArrivalPortal keeps the player in the portal they arrived in
// for n ticks, sending the two packets a client sends while it looks around
// without walking: a rotation-only move on one tick and a position+rotation
// move on the next, both staying on the arrival cell. The loop is stopped and
// each tick run by hand once its packet has been read, so the stay costs no
// wall-clock ticks and every tick sees exactly one look.
func lookAroundInArrivalPortal(t *testing.T, srv *server.Server, c *servertest.Client, name string, n int) {
	t.Helper()
	srv.StopTick()
	arrival := srv.PlayerPosition(name)
	for i := range n {
		yaw := float32(i % 360)
		if i%2 == 0 {
			c.SendRotation(yaw, 0, true)
		} else {
			c.MoveToGrounded(arrival.X, arrival.Y, arrival.Z, yaw, 0, true)
		}
		c.Settle()
		srv.RunTick()
		if got := srv.PlayerDimension(name); got != world.NetherName {
			t.Fatalf("the player left the arrival dimension at tick %d (look %v): %q", i, yaw, got)
		}
	}
}

// TestCreativePlayerStaysInTheArrivalPortalWhileLookingAround pins the portal
// cooldown against a rotation-only move packet: a creative player who arrives
// in the nether and looks around for 300 ticks stays in the nether, because
// standing in the arrival portal cannot spend the cooldown the arrival set.
func TestCreativePlayerStaysInTheArrivalPortalWhileLookingAround(t *testing.T) {
	t.Parallel()
	cfg := servertest.NetherTestConfig(t, servertest.BlockTestConfig())
	srv, c := servertest.Join(t, cfg, "Returner")

	servertest.PortalFrame(t, srv)
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ+0.5, 0, 0)

	crossTo(t, srv, c, "Returner", world.NetherName)
	lookAroundInArrivalPortal(t, srv, c, "Returner", 300)
}

// TestSurvivalPlayerStaysInTheArrivalPortalWhileLookingAround is the survival
// form of the same: the arrival's cooldown holds a player looking around in
// the arrival portal for 300 ticks as well.
func TestSurvivalPlayerStaysInTheArrivalPortalWhileLookingAround(t *testing.T) {
	t.Parallel()
	cfg := servertest.NetherTestConfig(t, servertest.SurvivalTestConfig())
	srv, c := servertest.Join(t, cfg, "Returner")

	servertest.PortalFrame(t, srv)
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ+0.5, 0, 0)

	crossTo(t, srv, c, "Returner", world.NetherName)
	lookAroundInArrivalPortal(t, srv, c, "Returner", 300)
}

// TestAPortalAppearsAndCollapsesInOneSectionUpdate checks that a lit frame's
// panes reach the client as one Section Blocks packet and a collapse as
// another, rather than one Block Update per pane.
func TestAPortalAppearsAndCollapsesInOneSectionUpdate(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Batcher")
	servertest.BuildFrame(t, srv)
	servertest.Flint(t, c)
	c.Settle()

	count := func(do func()) (single, section int) {
		do()
		c.SettleAndCollect(func(id int32, _ []byte) {
			switch id {
			case protocol.IDPlayBlockUpdate:
				single++
			case protocol.IDPlaySectionBlocks:
				section++
			}
		})
		return single, section
	}

	single, section := count(func() {
		c.PlaceBlock(servertest.PortalInnerX0, servertest.PortalFloorY-1, servertest.PortalInnerZ, protocol.FaceTop)
	})
	// The two single updates are the strike's own fire and its replacement.
	if section != 1 || single > 2 {
		t.Errorf("lighting sent %d block updates and %d section updates, want at most 2 and 1", single, section)
	}
	single, section = count(func() { c.StartDigging(servertest.PortalInnerX0-1, servertest.PortalFloorY, servertest.PortalInnerZ) })
	// The broken obsidian is the one single update.
	if section != 1 || single > 1 {
		t.Errorf("collapse sent %d block updates and %d section updates, want at most 1 and 1", single, section)
	}
}

// TestPortalCooldownSurvivesARejoinInTheArrivalPortal pins that the player
// file carries PortalCooldown: a creative player who leaves while standing in
// the arrival portal and rejoins stays in the nether instead of being sent
// back on the first tick.
// vanilla: Entity.saveWithoutId, Entity.load
func TestPortalCooldownSurvivesARejoinInTheArrivalPortal(t *testing.T) {
	t.Parallel()
	cfg := servertest.NetherTestConfig(t, servertest.BlockTestConfig())
	srv, connect := servertest.Start(t, cfg)
	c := connect().Join("Rejoiner")

	servertest.PortalFrame(t, srv)
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ+0.5, 0, 0)
	crossTo(t, srv, c, "Rejoiner", world.NetherName)
	lookAroundInArrivalPortal(t, srv, c, "Rejoiner", 5)
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Rejoiner"))
	if cooldown, ok := nbt.Get[nbt.Int](root, "PortalCooldown"); !ok || cooldown <= 0 {
		t.Fatalf("saved PortalCooldown = %v, %v; want the arrival's running cooldown", cooldown, ok)
	}

	c = connect()
	c.Join("Rejoiner")
	if got := srv.PlayerDimension("Rejoiner"); got != world.NetherName {
		t.Fatalf("rejoined in %q, want the nether", got)
	}
	lookAroundInArrivalPortal(t, srv, c, "Rejoiner", 100)
}

// TestABuiltPortalFramesItsCorners checks that the portal the server builds
// at the destination is obsidian on all sixteen edge cells of the 4x5 frame,
// the four corners included.
// vanilla: PortalForcer.createPortal
func TestABuiltPortalFramesItsCorners(t *testing.T) {
	t.Parallel()
	cfg := servertest.NetherTestConfig(t, servertest.BlockTestConfig())
	srv, c := servertest.Join(t, cfg, "Cornerer")

	obsidian, portalX, portalZ, _ := servertest.PortalStates(t)
	servertest.BuildFrame(t, srv)
	servertest.Flint(t, c)
	c.PlaceBlock(servertest.PortalInnerX0, servertest.PortalFloorY-1, servertest.PortalInnerZ, protocol.FaceTop)
	c.Settle()
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ+0.5, 0, 0)
	c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
	c.ExpectViewWithinTicks(srv, 200)

	pos := srv.PlayerPosition("Cornerer")
	fx, fy, fz := blockOfFloor(pos.X), int32(pos.Y), blockOfFloor(pos.Z)
	at := func(x, y, z int32) uint16 { return srv.BlockInDimension(world.NetherName, x, y, z) }
	isPortal := func(s uint16) bool { return s == portalX || s == portalZ }
	// The built portal runs along X: find its lowest interior cell by
	// walking down and west from the feet.
	for isPortal(at(fx, fy-1, fz)) {
		fy--
	}
	for isPortal(at(fx-1, fy, fz)) {
		fx--
	}
	for dx := int32(-1); dx <= 2; dx++ {
		for dy := int32(-1); dy <= 3; dy++ {
			if dx != -1 && dx != 2 && dy != -1 && dy != 3 {
				continue
			}
			if got := at(fx+dx, fy+dy, fz); got != obsidian {
				t.Errorf("frame cell (%d, %d, %d) = %d, want obsidian", fx+dx, fy+dy, fz, got)
			}
		}
	}
}

// zPortal stands a lit portal along Z in the overworld: its interior x 4,
// y 64..66, z 4..5, framed in obsidian.
func zPortal(t *testing.T, srv *server.Server) {
	t.Helper()
	obsidian, _, portalZ, _ := servertest.PortalStates(t)
	x := int32(servertest.PortalInnerX0)
	for z := int32(3); z <= 6; z++ {
		for y := int32(servertest.PortalFloorY - 1); y <= servertest.PortalFloorY+3; y++ {
			state := portalZ
			if z == 3 || z == 6 || y == servertest.PortalFloorY-1 || y == servertest.PortalFloorY+3 {
				state = obsidian
			}
			if !srv.SetBlock(x, y, z, state) {
				t.Fatalf("placing (%d, %d, %d) failed", x, y, z)
			}
		}
	}
}

// netherExit stands two lit portal blocks in the nether at (x, PortalFloorY,
// 0), for a crossing that must find a portal there.
func netherExit(t *testing.T, srv *server.Server, x int32) {
	t.Helper()
	_, portalX, _, _ := servertest.PortalStates(t)
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, x, 0))
	for dy := range int32(2) {
		if !srv.SetBlockInDimension(world.NetherName, x, servertest.PortalFloorY+dy, 0, portalX) {
			t.Fatal("placing the nether's exit portal failed")
		}
	}
}

// TestABuiltPortalTakesTheSourcePortalsAxis checks a crossing from a Z-axis
// portal with no exit in range: the portal built for it runs along Z too.
// vanilla: NetherPortalBlock.getExitPortal, PortalForcer.createPortal
func TestABuiltPortalTakesTheSourcePortalsAxis(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.NetherTestConfig(t, servertest.BlockTestConfig()), "Zedder")
	_, _, portalZ, _ := servertest.PortalStates(t)
	zPortal(t, srv)
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, 4.5, 0, 0)
	c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
	servertest.WaitFor(t, srv, 200, func() bool { return srv.PlayerDimension("Zedder") == world.NetherName })

	pos := srv.PlayerPosition("Zedder")
	x, y, z := blockOfFloor(pos.X), blockOfFloor(pos.Y), blockOfFloor(pos.Z)
	if got := srv.LoadBlockInDimension(world.NetherName, x, y, z); got != portalZ {
		t.Errorf("arrived at (%d, %d, %d) in block %d, want an axis=z portal (%d)", x, y, z, got, portalZ)
	}
}

// TestABuiltPortalStandsBesideBlocksPlacedAtTheArrival walls the nether's
// arrival columns x, z -2..2 in stone from floor to roof: the portal built
// stands within the 16-block square on a floor its frame needs, and leaves
// every placed block where it was.
// vanilla: PortalForcer.createPortal, canHostFrame
func TestABuiltPortalStandsBesideBlocksPlacedAtTheArrival(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.NetherTestConfig(t, servertest.BlockTestConfig()), "Walled")
	obsidian, portalX, portalZ, _ := servertest.PortalStates(t)
	stone := registry.MustState("minecraft:stone", nil)
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 0, 0))
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, -1, 0))
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 0, -1))
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, -1, -1))
	var wall [][3]int32
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			for y := int32(1); y <= 126; y++ {
				if registry.IsSolid(srv.BlockInDimension(world.NetherName, x, y, z)) {
					continue
				}
				if !srv.SetBlockInDimension(world.NetherName, x, y, z, stone) {
					t.Fatalf("placing stone at (%d, %d, %d) failed", x, y, z)
				}
				wall = append(wall, [3]int32{x, y, z})
			}
		}
	}

	servertest.PortalFrame(t, srv)
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ+0.5, 0, 0)
	c.ExpectWithinTicks(srv, 200, protocol.IDPlayRespawn)
	servertest.WaitFor(t, srv, 200, func() bool { return srv.PlayerDimension("Walled") == world.NetherName })

	for _, at := range wall {
		if got := srv.BlockInDimension(world.NetherName, at[0], at[1], at[2]); got != stone {
			t.Fatalf("placed stone at %v is now %d: the portal was built into the wall", at, got)
		}
	}
	pos := srv.PlayerPosition("Walled")
	x, y, z := blockOfFloor(pos.X), blockOfFloor(pos.Y), blockOfFloor(pos.Z)
	if got := srv.LoadBlockInDimension(world.NetherName, x, y, z); got != portalX && got != portalZ {
		t.Fatalf("arrived at (%d, %d, %d) in block %d, want in the portal built", x, y, z, got)
	}
	if x < -16 || x > 17 || z < -16 || z > 17 {
		t.Errorf("arrived at (%d, %d, %d), outside the 16-block square around the arrival column", x, y, z)
	}
	foot := y
	for p := srv.LoadBlockInDimension(world.NetherName, x, foot-1, z); p == portalX || p == portalZ; p = srv.LoadBlockInDimension(world.NetherName, x, foot-1, z) {
		foot--
	}
	if got := srv.LoadBlockInDimension(world.NetherName, x, foot-1, z); got != obsidian {
		t.Errorf("under the portal at (%d, %d, %d) is %d, want the frame's obsidian", x, foot-1, z, got)
	}
}

// spectatorBesidePortal joins an op, makes them a spectator and stops the
// tick, with the portal frame built.
func spectatorBesidePortal(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.NetherTestConfig(t, servertest.BlockTestConfig()), "Ghost", servertest.OpsListing(t, "Ghost"))
	servertest.PortalFrame(t, srv)
	srv.StopTick()
	c.ChatCommand("gamemode spectator")
	servertest.WaitFor(t, srv, 100, func() bool {
		return srv.PlayerGameMode("Ghost") == protocol.GameModeSpectator
	})
	return srv, c
}

// TestASpectatorStandingInANetherPortalStays checks a spectator has no
// physics, so standing in a portal starts nothing.
// vanilla: Player.tick noPhysics, Entity.isAffectedByBlocks
func TestASpectatorStandingInANetherPortalStays(t *testing.T) {
	t.Parallel()
	srv, c := spectatorBesidePortal(t)
	netherExit(t, srv, 0)
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ+0.5, 0, 0)
	c.Settle()
	for range 3 {
		srv.TickPortals()
	}
	if got := srv.PortalCooldown("Ghost"); got != 0 {
		t.Errorf("portal cooldown = %d, want 0", got)
	}
	if got := srv.PortalTime("Ghost"); got != 0 {
		t.Errorf("portal time = %d, want 0", got)
	}
	if got := srv.PlayerDimension("Ghost"); got != world.DimensionName {
		t.Errorf("the spectator is in %s, want to stay in the overworld", got)
	}
}

// TestASpectatorRightClickingANetherPortalCrosses checks the use of a portal
// block takes a spectator through.
// vanilla: ServerPlayerGameMode.useItemOn
func TestASpectatorRightClickingANetherPortalCrosses(t *testing.T) {
	t.Parallel()
	srv, c := spectatorBesidePortal(t)
	netherExit(t, srv, 0)
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ-2.5, 0, 0)
	c.Settle()
	c.UseBlock(servertest.PortalInnerX0, servertest.PortalFloorY, servertest.PortalInnerZ)
	crossTo(t, srv, c, "Ghost", world.NetherName)
}

// TestASpectatorRightClickingAPortalWithNoExitIsTold checks a spectator whose
// use of a portal finds no exit portal in range is told so, and nothing is
// built.
// vanilla: NetherPortalBlock.getExitPortal, ServerPlayerGameMode.useItemOn
func TestASpectatorRightClickingAPortalWithNoExitIsTold(t *testing.T) {
	t.Parallel()
	srv, c := spectatorBesidePortal(t)
	_, portalX, portalZ, _ := servertest.PortalStates(t)
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ-2.5, 0, 0)
	c.Settle()
	c.UseBlock(servertest.PortalInnerX0, servertest.PortalFloorY, servertest.PortalInnerZ)
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: "spectator.cannot_teleport", Color: "red"})
	want.PutBool(true)
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, want.Bytes()) {
		t.Errorf("system chat = %x, want %x", got, want.Bytes())
	}
	if got := srv.PlayerDimension("Ghost"); got != world.DimensionName {
		t.Fatalf("the spectator is in %s, want to stay in the overworld", got)
	}
	// The build's reach is 18 blocks around the arrival column (0, 0).
	for _, cx := range []int32{-32, -16, 0, 16} {
		for _, cz := range []int32{-32, -16, 0, 16} {
			t.Cleanup(srv.HoldChunkInDimension(world.NetherName, cx, cz))
		}
	}
	for x := int32(-18); x <= 18; x++ {
		for z := int32(-18); z <= 18; z++ {
			for y := range int32(128) {
				if s := srv.BlockInDimension(world.NetherName, x, y, z); s == portalX || s == portalZ {
					t.Fatalf("a portal block stands at (%d, %d, %d) in the nether, want none built", x, y, z)
				}
			}
		}
	}
}

// TestASpectatorRightClickingAPortalWithNoExitIsToldWhenTheRuleIsOff checks
// the nether rule does not silence the message when no exit portal is found:
// it refuses only an entry that has a destination.
// vanilla: ServerPlayerGameMode.useItemOn, Entity.teleportToPortalDestination
func TestASpectatorRightClickingAPortalWithNoExitIsToldWhenTheRuleIsOff(t *testing.T) {
	t.Parallel()
	srv, c := spectatorBesidePortal(t)
	srv.SetGameRule(world.RuleAllowEnteringNether.String(), false)
	c.MoveTo(float64(servertest.PortalInnerX0)+0.5, servertest.PortalFloorY, servertest.PortalInnerZ-2.5, 0, 0)
	c.Settle()
	c.UseBlock(servertest.PortalInnerX0, servertest.PortalFloorY, servertest.PortalInnerZ)
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: "spectator.cannot_teleport", Color: "red"})
	want.PutBool(true)
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, want.Bytes()) {
		t.Errorf("system chat = %x, want %x", got, want.Bytes())
	}
}

// TestNetherArrivalIsClampedIntoTheNetherBorder checks a crossing from far
// out in the overworld into a nether with a small border: the exit position
// x 1000/8 = 125 is pulled to the border's edge (x 31) before the search, so
// the crossing finds the portal at x 20, near that edge, which a search
// around x 125 would miss (and then build one of its own).
// vanilla: NetherPortalBlock.getPortalDestination, WorldBorder.clampToBounds
func TestNetherArrivalIsClampedIntoTheNetherBorder(t *testing.T) {
	t.Parallel()
	const name = "Borderer"
	cfg := servertest.NetherTestConfig(t, servertest.BlockTestConfig())
	srv, c := servertest.Join(t, cfg, name, servertest.OpsListing(t, name))
	obsidian, portalX, _, _ := servertest.PortalStates(t)

	// A lit X-axis portal with its interior at x 1000..1001, y 64..66, z 4.
	const x0, z = 1000, int32(servertest.PortalInnerZ)
	t.Cleanup(srv.HoldChunk(x0, z))
	for x := int32(x0 - 1); x <= x0+2; x++ {
		for y := int32(servertest.PortalFloorY - 1); y <= servertest.PortalFloorY+3; y++ {
			state := portalX
			if x == x0-1 || x == x0+2 || y == servertest.PortalFloorY-1 || y == servertest.PortalFloorY+3 {
				state = obsidian
			}
			if !srv.SetBlock(x, y, z, state) {
				t.Fatalf("placing (%d, %d, %d) failed", x, y, z)
			}
		}
	}

	// The nether's border spans x and z -32..32; its exit portal stands at
	// x 20, 11 blocks in from the edge.
	const exitX = 20
	netherExit(t, srv, exitX)
	c.ChatCommand("execute in minecraft:the_nether run worldborder set 64")
	c.ChatCommand(fmt.Sprintf("tp @s %d.5 %d %d.5", x0, servertest.PortalFloorY, z))
	c.ExpectWithinTicks(srv, 400, protocol.IDPlayRespawn)
	servertest.WaitFor(t, srv, 200, func() bool { return srv.PlayerDimension(name) == world.NetherName })

	pos := srv.PlayerPosition(name)
	if x, bz := blockOfFloor(pos.X), blockOfFloor(pos.Z); x != exitX || bz != 0 {
		t.Errorf("nether arrival = (%v, %v), want the exit portal's column (%d, 0)", pos.X, pos.Z, exitX)
	}
}

// TestEndArrivalPlatformDropsWhatItReplaces checks that the arrival platform
// breaks a block it replaces with its drops, and leaves a clean platform alone.
func TestEndArrivalPlatformDropsWhatItReplaces(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")
	arrival := world.EndArrival()
	srv.MovePlayerToDimension("Watcher", world.EndName, float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+0.5)
	if _, ok := servertest.ReadUntilViewSent(t, watcher); !ok {
		t.Fatal("the end never arrived for the watcher")
	}
	endStone, ok := registry.FindState("minecraft:end_stone", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:end_stone")
	}
	sx, sy, sz := arrival.X+1, arrival.Y, arrival.Z+1
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.SetBlockInDimension(world.EndName, sx, sy, sz, endStone)
	})

	cross := func(name string) {
		p := connect().Join(name)
		// A second call finds the portal block already placed.
		srv.SetBlock(10, servertest.PortalFloorY-1, 10, srv.EndPortalState())
		p.MoveTo(10.5, servertest.PortalFloorY-1, 10.5, 0, 0)
		servertest.WaitFor(t, srv, 400, func() bool {
			return srv.PlayerDimension(name) == world.EndName
		})
	}
	cross("Ender")

	if got := srv.BlockInDimension(world.EndName, sx, sy, sz); got != 0 {
		t.Fatalf("the replaced cell = %d, want air", got)
	}
	near := func() int {
		n := 0
		for _, it := range srv.ItemsInDimension(world.EndName) {
			if it.Item == "minecraft:end_stone" && math.Abs(it.X-(float64(sx)+0.5)) <= 1 && math.Abs(it.Y-(float64(sy)+0.5)) <= 1.5 && math.Abs(it.Z-(float64(sz)+0.5)) <= 1 {
				n++
			}
		}
		return n
	}
	servertest.WaitFor(t, srv, 100, func() bool { return near() == 1 })

	before := len(srv.ItemsInDimension(world.EndName))
	cross("Second")
	if after := len(srv.ItemsInDimension(world.EndName)); after != before {
		t.Fatalf("items after a clean arrival = %d, want %d", after, before)
	}
}

// TestEnteringTheEndFacesWestAndKeepsThePitch checks the look of an arrival in
// the end: yaw 90 (west, toward the main island) absolute, the pitch carried
// over and sent as a relative zero.
func TestEnteringTheEndFacesWestAndKeepsThePitch(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Ender")

	if !srv.SetBlock(10, servertest.PortalFloorY-1, 10, srv.EndPortalState()) {
		t.Fatal("placing the end portal block failed")
	}
	c.MoveTo(10.5, servertest.PortalFloorY-1, 10.5, 200, 30)
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn arrived for standing in the end portal")
	}
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlaySyncPosition)
	if !ok {
		t.Fatal("no position sync followed the respawn")
	}
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.PlayerDimension("Ender") == world.EndName
	})

	pos := srv.PlayerPosition("Ender")
	if pos.Yaw != 90 || pos.Pitch != 30 {
		t.Errorf("look = (yaw %v, pitch %v), want (90, 30)", pos.Yaw, pos.Pitch)
	}
	r := protocol.NewReader(data)
	r.ReadVarInt() // teleport ID
	for range 6 {
		r.ReadF64() // position, velocity
	}
	yaw, pitch, flags := r.ReadF32(), r.ReadF32(), r.ReadI32()
	if yaw != 90 || pitch != 0 || flags&0x10 == 0 || flags&0x08 != 0 {
		t.Errorf("sync = yaw %v, pitch %v, flags %#x; want yaw 90, pitch 0, relative pitch only", yaw, pitch, flags)
	}
}
