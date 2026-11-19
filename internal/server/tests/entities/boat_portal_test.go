package entities

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// vanilla: NetherPortalBlock.entityInside, Entity.canUsePortal
func TestABoatRidersFeetCannotActivateANetherPortal(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Rower")
	servertest.HoldTicks(t, srv)
	boat := srv.SpawnBoat(0.5, 64, 0.5, "minecraft:oak_boat")
	c.UseOn(boat)
	servertest.AwaitPassengersFor(c, boat)
	if got := srv.PlayerPosition("Rower").Y; math.Abs(got-63.5875) > 1e-6 {
		t.Fatalf("rider Y = %g, want 63.5875", got)
	}
	// The rider's feet enter this cell; the hull rests flush above it.
	if !srv.SetBlock(0, 63, 0, rules.NetherPortalX) {
		t.Fatal("could not place the portal below the hull")
	}
	srv.TickPortals()
	if got := srv.PortalTime("Rower"); got != 0 {
		t.Fatalf("portal time = %d, want no contact from the passenger", got)
	}
}

// vanilla: NetherPortalBlock.createDimensionTransition, Entity.teleportCrossDimension
func TestARiddenBoatPortalTurnsTheHullAndRider(t *testing.T) {
	t.Parallel()
	for _, rotated := range []bool{false, true} {
		name := "aligned"
		if rotated {
			name = "rotated"
		}
		t.Run(name, func(t *testing.T) {
			cfg := servertest.NetherTestConfig(t, servertest.BlockTestConfig())
			srv, c := servertest.Join(t, cfg, "Rower", servertest.OpsListing(t, "Rower"))
			servertest.HoldTicks(t, srv)
			servertest.PortalFrame(t, srv)
			t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 0, 0))
			for x := int32(2); x <= 7; x++ {
				for y := int32(69); y <= 74; y++ {
					for z := int32(2); z <= 7; z++ {
						state := uint16(0)
						if z == 4 && x >= 3 && x <= 6 && y <= 73 {
							state = rules.ObsidianState
							if x >= 4 && x <= 5 && y >= 70 && y <= 72 {
								state = rules.NetherPortalX
								if rotated {
									state = rules.NetherPortalZ
								}
							}
						}
						bx, bz := x, z
						if rotated {
							bx, bz = z, x
						}
						if !srv.SetBlockInDimension(world.NetherName, bx, y, bz, state) && state != 0 {
							t.Fatal("building the exit failed")
						}
					}
				}
			}
			c.ChatCommand(`summon oak_boat 5.0 64 4.5 {Rotation:[180f,0f]}`)
			c.Settle()
			ids := srv.BoatIDs()
			if len(ids) != 1 {
				t.Fatalf("boats = %v, want one", ids)
			}
			boat := ids[0]
			c.ChatCommand("ride @s mount @e[type=oak_boat,limit=1]")
			servertest.AwaitPassengersFor(c, boat)
			// The rider's look differs from the hull's; neither may supply
			// the other's starting yaw when the portal turns them.
			c.MoveTo(5, 63.5875, 4.5, 143, 24)
			c.Settle()
			// The hull activates the portal on this tick; the trip then runs
			// off the loop, so advancing ticks cannot bound its completion.
			servertest.RunTicks(t, srv, 1)
			c.AwaitPacket(protocol.IDPlayRespawn)
			c.AwaitPacket(protocol.IDPlaySyncPosition)
			turn := float32(0)
			if rotated {
				turn = 90
			}
			r := protocol.NewReader(c.AwaitPacket(protocol.IDPlaySpawnEntity))
			if id := r.ReadVarInt(); id != boat {
				t.Fatalf("arrival entity = %d, want boat %d", id, boat)
			}
			r.ReadUUID()
			if kind := r.ReadVarInt(); kind != protocol.EntityTypeOakBoat {
				t.Fatalf("arrival type = %d, want oak boat", kind)
			}
			r.ReadF64() // position
			r.ReadF64()
			r.ReadF64()
			r.ReadLpVec3() // velocity
			r.ReadAngle()  // pitch
			if yaw := r.ReadAngle(); yaw != 180+turn || r.Err() != nil {
				t.Errorf("arrival hull packet yaw = %v, error %v; want %v", yaw, r.Err(), 180+turn)
			}
			servertest.AwaitPassengersFor(c, boat)
			// The sound follows the completed dimension send-off.
			if event := protocol.NewReader(c.AwaitPacket(protocol.IDPlayWorldEvent)).ReadI32(); event != protocol.WorldEventPortalTravel {
				t.Fatalf("arrival event = %d, want portal travel", event)
			}
			if _, _, _, yaw, _, ok := srv.BoatState(boat); !ok || yaw != float64(180+turn) {
				t.Errorf("arrival hull yaw = %v, present %v; want %v", yaw, ok, 180+turn)
			}
			if got := c.LastTeleport; got.Yaw != 143+turn || got.Pitch != 24 {
				t.Errorf("rider teleport = %+v, want yaw %v/pitch 24", got, 143+turn)
			}
			if got := srv.PlayerDimension("Rower"); got != world.NetherName {
				t.Errorf("rider dimension = %q, want Nether", got)
			}
			if rider, ok := srv.BoatRider(boat); !ok || rider != "Rower" {
				t.Errorf("rider = %q, present %v; want Rower still aboard", rider, ok)
			}
			if srv.BoatCountInDimension(world.NetherName) != 1 || srv.BoatCountInDimension(world.DimensionName) != 0 {
				t.Error("want only the arrival boat in the Nether")
			}
		})
	}
}
