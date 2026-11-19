package world

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
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
