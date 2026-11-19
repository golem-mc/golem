package blocks

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func gatewayBody(t *testing.T, srv *server.Server, kind string) command.Entity {
	t.Helper()
	bodies := srv.SelectedEntities(kind)
	if len(bodies) != 1 {
		t.Fatalf("%s bodies = %d, want one", kind, len(bodies))
	}
	return bodies[0]
}

func gatewayBodyTag(t *testing.T, body command.Entity) nbt.Compound {
	t.Helper()
	tag, ok := body.(command.DataTarget).DataTag()
	if !ok {
		t.Fatal("body has no saved data")
	}
	return tag.Entries()
}

func gatewayBodyWorld(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gate")
	servertest.HoldTicks(t, srv)
	srv.SetLoadedBlock(3, gatewayY-1, 3, servertest.BlockState(t, "minecraft:stone"))
	srv.SetLoadedBlock(3, gatewayY, 3, servertest.BlockState(t, "minecraft:end_gateway"))
	srv.SetEndGatewayExit(3, gatewayY, 3, 20, 120, 20, true)
	srv.SetLoadedBlock(20, 119, 20, servertest.BlockState(t, "minecraft:stone"))
	return srv, c
}

func TestEndGatewayCarriesEmptyVehicles(t *testing.T) {
	for _, kind := range []string{"minecraft:oak_boat", "minecraft:minecart"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, _ := gatewayBodyWorld(t)
			if kind == "minecraft:oak_boat" {
				srv.SpawnBoat(3.5, gatewayY, 3.5, kind)
			} else {
				srv.SpawnMinecart(3.5, gatewayY, 3.5)
			}
			servertest.UntilTicks(t, srv, 100, "empty vehicle at gateway exit", func() bool {
				srv.WaitGatewayMoves() // the landing search runs off the tick
				p := gatewayBody(t, srv, kind).Position()
				return p.X == 20.5 && p.Y == 120 && p.Z == 20.5
			})
			if cooldown, _ := nbt.IntField(gatewayBodyTag(t, gatewayBody(t, srv, kind)), "PortalCooldown"); cooldown <= 0 || cooldown > 10 {
				// vanilla: VehicleEntity.getDimensionChangingDelay
				t.Fatalf("portal cooldown = %v, want 1..10", cooldown)
			}
			// A different gateway at the arrival must not bounce the body back.
			srv.SetLoadedBlock(20, 120, 20, servertest.BlockState(t, "minecraft:end_gateway"))
			srv.SetEndGatewayExit(20, 120, 20, 3, gatewayY, 3, true)
			servertest.RunTicks(t, srv, 5)
			srv.WaitGatewayMoves()
			if p := gatewayBody(t, srv, kind).Position(); p.X != 20.5 || p.Z != 20.5 {
				t.Fatalf("cooling vehicle bounced to %+v", p)
			}
		})
	}
}

func TestEndGatewayCarriesFirework(t *testing.T) {
	t.Parallel()
	srv, _ := gatewayBodyWorld(t)
	// Its centre skips the cell in one tick; the swept box still touches it.
	srv.SpawnFireworkAtAngle(2.5, gatewayY+0.5, 3.5, 2, 0, 0, 10000)
	servertest.UntilTicks(t, srv, 100, "rocket at gateway exit", func() bool {
		srv.WaitGatewayMoves() // the landing search runs off the tick
		return gatewayBody(t, srv, "minecraft:firework_rocket").Position().Z == 20.5
	})
	tag := gatewayBodyTag(t, gatewayBody(t, srv, "minecraft:firework_rocket"))
	motion := tag["Motion"].(nbt.List)
	if motion.Items[0] != nbt.Double(2) {
		t.Fatalf("rocket motion = %v, want its original x velocity 2", motion)
	}
	if cooldown, _ := nbt.IntField(tag, "PortalCooldown"); cooldown <= 0 || cooldown > 2 {
		// vanilla: Projectile.getDimensionChangingDelay
		t.Fatalf("rocket portal cooldown = %d, want 1..2", cooldown)
	}
}

func TestEndGatewaySkipsAttachedFirework(t *testing.T) {
	t.Parallel()
	srv, c := gatewayBodyWorld(t)
	srv.GiveSlot("Gate", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:elytra"), Count: 1})
	servertest.UseHeld(c, 0, 0)
	c.AwaitPlay(protocol.IDPlayContainerSlot)
	c.AwaitPlay(protocol.IDPlayContainerSlot)
	srv.GiveSlot("Gate", 36, servertest.FireworkStack(3, 0))
	// The glider stays outside the gateway; only the rocket beside its hand overlaps.
	c.MoveToGrounded(4.5, gatewayY, 3.5, 0, 0, false)
	servertest.StartFallFlying(c)
	servertest.UseHeld(c, 0, 0)
	c.Settle()
	if pose := srv.PlayerPose("Gate"); pose != protocol.PoseFallFlying {
		t.Fatalf("player pose = %d, want fall flying", pose)
	}
	c.ExpectNoneWithinTicks(srv, 5, servertest.PacketIs(protocol.IDPlayBlockAction))
	body := gatewayBody(t, srv, "minecraft:firework_rocket")
	before := body.Position()
	if before.X < 3.8 || before.X > 4.125 || before.Y != gatewayY || before.Z < 3 || before.Z > 4 {
		t.Fatalf("attached rocket left the glider's hand: %+v", before)
	}
	servertest.RunTicks(t, srv, 5)
	if after := body.Position(); after != before {
		t.Fatalf("attached rocket moved: before=%+v after=%+v", before, after)
	}
	if cooldown, _ := nbt.IntField(gatewayBodyTag(t, body), "PortalCooldown"); cooldown != 0 {
		t.Fatalf("attached rocket portal cooldown = %d, want zero", cooldown)
	}
}

func TestEndGatewayCarriesArmorStand(t *testing.T) {
	t.Parallel()
	srv, _ := gatewayBodyWorld(t)
	// Only its head overlaps the gateway.
	srv.SetLoadedBlock(3, gatewayY-1, 3, 0)
	srv.SpawnArmorStand(3.5, gatewayY-1, 3.5)
	servertest.UntilTicks(t, srv, 100, "stand at gateway exit", func() bool {
		return gatewayBody(t, srv, "minecraft:armor_stand").Position().Z == 20.5
	})
	_, vy, _, ok := srv.ArmorStandVelocity(gatewayBody(t, srv, "minecraft:armor_stand").EntityID())
	if !ok || vy >= 0 {
		t.Fatalf("stand lost its fall: vy=%v, present=%v", vy, ok)
	}
}

func TestEndGatewaySkipsArmorStandsWithoutPhysics(t *testing.T) {
	for _, flag := range []string{"Marker", "NoGravity"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			srv, c := gatewayBodyWorld(t)
			srv.SpawnArmorStand(3.5, gatewayY, 3.5)
			body := gatewayBody(t, srv, "minecraft:armor_stand")
			tag := gatewayBodyTag(t, body)
			tag[flag] = nbt.Byte(1)
			if !body.(command.DataWriter).SetDataTag(tag) {
				t.Fatal("stand data write failed")
			}
			c.ExpectNoneWithinTicks(srv, 5, servertest.PacketIs(protocol.IDPlayBlockAction))
			if p := gatewayBody(t, srv, "minecraft:armor_stand").Position(); p.X != 3.5 || p.Z != 3.5 {
				t.Fatalf("stand without physics moved through gateway: %+v", p)
			}
		})
	}
}

func TestEndGatewayCarriesVehiclePlayers(t *testing.T) {
	for _, kind := range []string{"minecraft:oak_boat", "minecraft:minecart"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c, connect := servertest.BoatPlayer(t, "creative", "Driver", 0)
			other := connect().Join("Rider")
			servertest.HoldTicks(t, srv)
			c.MoveToGround(3.5, 64, 1.5, true)
			other.MoveToGround(3.5, 64, 1.5, true)
			c.Settle()
			other.Settle()
			var id int32
			if kind == "minecraft:oak_boat" {
				id = srv.SpawnBoat(3.5, 64, 3.5, kind)
			} else {
				id = srv.SpawnMinecart(3.5, 64, 3.5)
			}
			c.UseOn(id)
			servertest.AwaitPassengersFor(c, id)
			riders := []*servertest.Client{c}
			if kind == "minecraft:oak_boat" {
				other.UseOn(id)
				servertest.AwaitPassengersFor(other, id)
				riders = append(riders, other)
			}
			c.Settle()
			other.Settle()
			servertest.RunTicks(t, srv, 1)
			before := srv.PlayerPosition("Driver")
			vehicleBefore := gatewayBody(t, srv, kind).Position()
			srv.SetLoadedBlock(20, 119, 20, servertest.BlockState(t, "minecraft:stone"))
			srv.SetLoadedBlock(3, 64, 3, servertest.BlockState(t, "minecraft:end_gateway"))
			srv.SetEndGatewayExit(3, 64, 3, 20, 120, 20, true)
			for _, rider := range riders {
				data := rider.ExpectWithinTicks(srv, 100, protocol.IDPlayTeleportEntityRelative)
				r := protocol.NewReader(data)
				gotID := r.ReadVarInt()
				x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
				vx, vy, vz := r.ReadF64(), r.ReadF64(), r.ReadF64()
				yaw, pitch := r.ReadF32(), r.ReadF32()
				flags := r.ReadI32()
				r.ReadBool()
				wantFlags := int32(0)
				if kind == "minecraft:oak_boat" && rider == c {
					wantFlags = 0x1f8
				}
				if gotID != id || x != 20.5 || y != 120 || z != 20.5 || vx != 0 || vz != 0 || yaw != 0 || pitch != 0 || flags != wantFlags || r.Err() != nil || r.Remaining() != 0 {
					t.Fatalf("vehicle teleport: id=%d pos=%v/%v/%v velocity=%v/%v/%v rotation=%v/%v flags=%x err=%v remaining=%d", gotID, x, y, z, vx, vy, vz, yaw, pitch, flags, r.Err(), r.Remaining())
				}
			}
			after := srv.PlayerPosition("Driver")
			if math.Abs((after.Y-120)-(before.Y-vehicleBefore.Y)) > 0.001 || math.Abs((after.X-20.5)-(before.X-vehicleBefore.X)) > 0.001 || math.Abs((after.Z-20.5)-(before.Z-vehicleBefore.Z)) > 0.001 {
				t.Fatalf("driver lost its seat offset: before=%+v vehicle=%+v after=%+v", before, vehicleBefore, after)
			}
			var ids []int32
			if kind == "minecraft:oak_boat" {
				ids = srv.BoatPassengerIDs(id)
			} else {
				ids = srv.MinecartPassengerIDs(id)
			}
			if len(ids) != len(riders) {
				t.Fatalf("%d passengers after travel, want %d", len(ids), len(riders))
			}
			if cooldown, _ := nbt.IntField(gatewayBodyTag(t, gatewayBody(t, srv, kind)), "PortalCooldown"); cooldown < 1 || cooldown > 10 {
				t.Fatalf("ridden vehicle cooldown = %d, want 1..10", cooldown)
			}
		})
	}
}

func TestEndGatewayCarriesBoatMob(t *testing.T) {
	t.Parallel()
	srv, _ := gatewayBodyWorld(t)
	boatID := srv.SpawnBoat(3.5, gatewayY, 3.5, "minecraft:oak_boat")
	mobID := srv.SpawnMob("minecraft:cow", 3.5, gatewayY, 3.5)
	servertest.UntilTicks(t, srv, 100, "cow and boat at exit", func() bool {
		x, _, z, ok := srv.MobPos(mobID)
		return ok && x > 19 && z > 19
	})
	if ids := srv.BoatPassengerIDs(boatID); len(ids) != 1 || ids[0] != mobID {
		t.Fatalf("boat passengers = %v, want cow %d", ids, mobID)
	}
	srv.SetLoadedBlock(20, 120, 20, servertest.BlockState(t, "minecraft:end_gateway"))
	srv.SetEndGatewayExit(20, 120, 20, 3, gatewayY, 3, true)
	servertest.RunTicks(t, srv, 5)
	// Mob passengers do not acquire a separate portal cooldown.
	if cooldown, _ := nbt.IntField(gatewayBodyTag(t, gatewayBody(t, srv, "minecraft:cow")), "PortalCooldown"); cooldown != 0 {
		t.Fatalf("riding cow used a gateway itself: cooldown=%d", cooldown)
	}
}

func TestEndGatewaySweepsDrivenBoat(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Driver", 0)
	servertest.HoldTicks(t, srv)
	c.MoveToGround(3.5, 64, 1.5, true)
	c.Settle()
	id := srv.SpawnBoat(3.5, 64, 3.5, "minecraft:oak_boat")
	c.UseOn(id)
	servertest.AwaitPassengersFor(c, id)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	srv.SetLoadedBlock(5, 64, 3, servertest.BlockState(t, "minecraft:end_gateway"))
	srv.SetEndGatewayExit(5, 64, 3, 20, 120, 20, true)
	srv.SetLoadedBlock(20, 119, 20, servertest.BlockState(t, "minecraft:stone"))
	c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(8.5)
		b.PutF64(64)
		b.PutF64(3.5)
		b.PutF32(0)
		b.PutF32(0)
		b.PutBool(true)
	})
	c.Settle()
	c.ExpectWithinTicks(srv, 100, protocol.IDPlayTeleportEntityRelative)
	if x, _, z, _, _, ok := srv.BoatState(id); !ok || x != 20.5 || z != 20.5 {
		t.Fatalf("driven boat skipped gateway: pos=%v/%v, present=%v", x, z, ok)
	}
}
