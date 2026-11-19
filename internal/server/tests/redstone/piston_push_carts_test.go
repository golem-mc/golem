package redstone

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// pistonPushRig joins an operator with the loop held, stone through the destination at Y=63
// and a piston at (2, 64, 3) facing east, the cell in front of it clear.
func pistonPushRig(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Pusher", servertest.OpsListing(t, "Pusher"))
	servertest.HoldTicks(t, srv)
	for x := int32(3); x <= 9; x++ {
		srv.SetBlock(x, 63, 3, registry.BlockStone.DefaultState())
	}
	servertest.Setblock(t, c, 2, 64, 3, "piston[facing=east]")
	return srv, c
}

// TestAPistonPushesAMinecart extends a piston into a cart standing in front
// of it: the cart moves one block along the push without turning.
// vanilla: PistonMovingBlockEntity.moveCollidedEntities
func TestAPistonPushesAMinecart(t *testing.T) {
	t.Parallel()
	srv, c := pistonPushRig(t)
	id := srv.SpawnMinecart(3.5, 64, 3.5)
	srv.RunLine("data merge entity @e[type=minecraft:minecart,limit=1] {Rotation:[90f,0f]}")
	if _, _, _, yaw, ok := srv.MinecartState(id); !ok || yaw != 90 {
		t.Fatalf("initial cart yaw=%v (alive %v), want 90", yaw, ok)
	}
	servertest.Setblock(t, c, 2, 65, 3, "redstone_block")
	servertest.RunTicks(t, srv, 3)
	x, _, _, yaw, ok := srv.MinecartState(id)
	if !ok || x != 4.5 {
		t.Errorf("the cart stands at x=%v (alive %v), want 4.5", x, ok)
	}
	if yaw != 90 {
		t.Errorf("the cart's yaw=%v after the push, want 90", yaw)
	}
}

// TestAPistonPushesAnEndCrystal is the same push against an end crystal,
// whose 2-wide box is cleared by the head rather than carried a block.
// vanilla: PistonMovingBlockEntity.moveCollidedEntities
func TestAPistonPushesAnEndCrystal(t *testing.T) {
	t.Parallel()
	srv, c := pistonPushRig(t)
	id := srv.SpawnEndCrystal(3.5, 64, 3.5)
	c.AwaitSpawn(protocol.EntityTypeEndCrystal)
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
	servertest.Setblock(t, c, 2, 65, 3, "redstone_block")
	// The client moves the crystal with the piston itself. A position sync
	// during the carry can make it apply the same displacement twice.
	// vanilla: EntityTypes.END_CRYSTAL, ServerEntity.sendChanges
	movement := servertest.PacketAbout(id, protocol.IDPlayTeleportEntity,
		protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot)
	c.ExpectNoneWithinTicks(srv, 3, movement)
	x, _, _, ok := srv.EndCrystalPosition(id)
	if !ok || x < 4.5 || x > 4.6 {
		t.Errorf("the crystal stands at x=%v (alive %v), want 4.5", x, ok)
	}
	srv.RunLine("tick freeze")
	c.ExpectNoneWithinTicks(srv, 3, movement)

	// Commanded teleports still need an absolute sync, even while frozen.
	srv.RunLine("tp @e[type=minecraft:end_crystal,limit=1] 8.5 64 3.5")
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 1, protocol.IDPlayTeleportEntity))
	if got := r.ReadVarInt(); got != id {
		t.Fatalf("teleport entity=%d, want crystal %d", got, id)
	}
	r.ReadVarInt() // position path
	if got := r.ReadF64(); got != 8.5 {
		t.Errorf("commanded crystal teleport x=%v, want 8.5", got)
	}
}

// vanilla: ChunkMap.tick, ServerEntity.sendChanges, PistonMovingBlockEntity.moveEntityByPiston
func TestAPistonTracksAMinecartBeforePhysicsWhileFrozen(t *testing.T) {
	t.Parallel()
	for frozenTicks := range 3 {
		t.Run(fmt.Sprint(frozenTicks), func(t *testing.T) {
			srv, c := pistonPushRig(t)
			id := srv.SpawnMinecart(3.5, 64, 3.5)
			spawn := c.AwaitSpawn(protocol.EntityTypeMinecart)
			if spawn.EntityID != id {
				t.Fatalf("spawn=%d, want cart %d", spawn.EntityID, id)
			}
			wire := [3]float64{spawn.X, spawn.Y, spawn.Z}
			var yaw float32
			var packets, absolutes int
			decode := func(packet int32, data []byte) bool {
				switch packet {
				case protocol.IDPlayTeleportEntity, protocol.IDPlayEntityPosition,
					protocol.IDPlayEntityPositionRot, protocol.IDPlayEntityRotation:
				default:
					return false
				}
				r := protocol.NewReader(data)
				if r.ReadVarInt() != id {
					return false
				}
				packets++
				switch packet {
				case protocol.IDPlayTeleportEntity:
					absolutes++
					if path := r.ReadVarInt(); path != 0 {
						t.Fatalf("position path=%d, want linear", path)
					}
					wire = [3]float64{r.ReadF64(), r.ReadF64(), r.ReadF64()}
					yaw = r.ReadF32()
					r.ReadF32() // pitch
					r.ReadBool()
				case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot:
					if properties := r.ReadVarInt(); properties>>1 != 0 {
						t.Fatalf("relative move properties=%d, want three deltas", properties)
					}
					for axis := range wire {
						wire[axis] = (math.Floor(wire[axis]*4096) + float64(r.ReadI16())) / 4096
					}
					if packet == protocol.IDPlayEntityPositionRot {
						yaw = r.ReadAngle()
						r.ReadAngle()
					}
				case protocol.IDPlayEntityRotation:
					r.ReadBool()
					yaw = r.ReadAngle()
					r.ReadAngle()
				}
				if r.Err() != nil || r.Remaining() != 0 {
					t.Fatalf("movement decode: %v (%d bytes left)", r.Err(), r.Remaining())
				}
				return false // consume every packet, including later moves in this tick
			}
			srv.RunLine("data merge entity @e[type=minecraft:minecart,limit=1] {Rotation:[90f,0f]}")
			// Settle the initial ground correction and /data sync request.
			// The first loop tick after spawn is tracking pass zero.
			c.ReadTicks(srv, 7, decode)
			srv.RunLine("tick freeze")
			loop := 7
			readTick := func(teleport bool) {
				t.Helper()
				x, y, z, _, alive := srv.MinecartState(id)
				if !alive {
					t.Fatal("cart disappeared")
				}
				before := [3]float64{x, y, z}
				encoded := before
				for axis := range encoded {
					encoded[axis] = math.Floor(encoded[axis]*4096) / 4096
				}
				want := 0
				if teleport || loop%3 == 0 && wire != encoded {
					want = 1
				}
				packets, absolutes = 0, 0
				c.ReadTicks(srv, 1, func(packet int32, data []byte) bool {
					previous := packets
					decode(packet, data)
					if packets != previous {
						if wire != encoded || yaw != 90 {
							t.Errorf("loop %d: wire=%v yaw=%v, want pre-physics %v yaw=90", loop, wire, yaw, encoded)
						}
					}
					return false
				})
				if packets != want || (!teleport && absolutes != 0) || (teleport && absolutes != 1) {
					t.Errorf("loop %d: movements=%d absolute=%d, want movements=%d teleport=%v", loop, packets, absolutes, want, teleport)
				}
				loop++
			}
			for range frozenTicks {
				readTick(false)
			}
			servertest.Setblock(t, c, 2, 65, 3, "redstone_block")
			srv.RunLine("tick step 3")
			for range 3 {
				readTick(false)
			}
			for range 3 {
				readTick(false)
				x, y, z, turn, alive := srv.MinecartState(id)
				if !alive || x != 4.5 || y != 64 || z != 3.5 || turn != 90 {
					t.Fatalf("frozen cart=(%v,%v,%v) yaw=%v alive=%v, want (4.5,64,3.5) yaw=90", x, y, z, turn, alive)
				}
			}
			if wire != [3]float64{4.5, 64, 3.5} || yaw != 90 {
				t.Errorf("final wire=%v yaw=%v, want (4.5,64,3.5) yaw=90", wire, yaw)
			}
			srv.RunLine("tp @e[type=minecraft:minecart,limit=1] 8.5 64 3.5")
			readTick(true)
		})
	}
}
