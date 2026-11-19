package entities

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The boat tests: the boat an item lays and the carrying it then does,
// driven through a real connection in the flat world the harness grows. A
// boat item used over water stands the boat on the water's surface and
// spends the item; a use whose spot the boat's box does not fit stands
// nothing and keeps the item; a click on a boat takes the clicker aboard
// and a sneak of theirs puts them off; the driver's reports of where the
// boat stands and how its paddles turn are carried to the watchers and
// into the metadata; and a broken boat leaves the boat it was.

// TestABoatLaidOnWaterStandsAtTheWaterSurface: a boat item used on the
// look's water source stands the boat on the fluid's surface — eight
// ninths up the cell — spends the item, and shows the boat to its placer
// as the oak boat it is.
func TestABoatLaidOnWaterStandsAtTheWaterSurface(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "survival", "Sailor", 1)
	// A two-by-two pool, the least the boat's box stands in without
	// crossing the stone of the ground beside it: the box a laid boat
	// carries is wider than one cell.
	srv.SetBlock(0, 63, 0, servertest.MustState(t, "minecraft:water", nil))
	srv.SetBlock(1, 63, 0, servertest.MustState(t, "minecraft:water", nil))
	srv.SetBlock(0, 63, 1, servertest.MustState(t, "minecraft:water", nil))
	srv.SetBlock(1, 63, 1, servertest.MustState(t, "minecraft:water", nil))
	// A hover a block above the pool: the boat's box stops short of the
	// body, which use's noCollision would otherwise refuse.
	c.MoveToGround(1.0, 65, 1.0, true)
	c.FacePlayer(1.0, 65, 1.0, 0, 90) // straight down into the pool
	c.Settle()

	c.UseBoatItem(0, 90)
	spawned := servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)
	if spawned.EntityID == 0 {
		t.Fatal("boat spawn ID = 0, want a real entity ID")
	}
	if servertest.Abs64(spawned.X-1.0) > 0.001 || servertest.Abs64(spawned.Z-1.0) > 0.001 {
		t.Errorf("boat spawned at (%v %v), want the column looked down (1.0, 1.0)", spawned.X, spawned.Z)
	}
	// The surface of a water source at y=63 under air: eight ninths up it.
	if got, want := spawned.Y, 63.0+8.0/9.0; servertest.Abs64(got-want) > 0.001 {
		t.Errorf("boat spawned at y=%v, want the surface %v", got, want)
	}
	if spawned.Vx != 0 || spawned.Vy != 0 || spawned.Vz != 0 {
		t.Errorf("the laid boat moved at (%v %v %v), want the stillness of a laying", spawned.Vx, spawned.Vy, spawned.Vz)
	}
	if got := srv.InventorySlot("Sailor", player.HotbarSlot); got.Count != 0 {
		t.Errorf("boats left = %d, want 0: the laid one was spent", got.Count)
	}
	if got := srv.BoatCount(); got != 1 {
		t.Errorf("boats standing = %d, want 1", got)
	}
}

// TestABoatLaidOnLandStandsOnTheGround: a boat item used on the look's
// block stands the boat on the block's top.
func TestABoatLaidOnLandStandsOnTheGround(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "survival", "Porter", 1)
	c.MoveToGround(2.5, 65, 2.5, true)
	c.FacePlayer(2.5, 65, 2.5, 0, 90) // straight down onto the stone
	c.Settle()

	c.UseBoatItem(0, 90)
	spawned := servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)
	if spawned.X != 2.5 || spawned.Z != 2.5 {
		t.Errorf("boat spawned at (%v %v), want the column looked down (2.5, 2.5)", spawned.X, spawned.Z)
	}
	if spawned.Y != 64 {
		t.Errorf("boat spawned at y=%v, want the stone's top at 64", spawned.Y)
	}
	if got := srv.InventorySlot("Porter", player.HotbarSlot); got.Count != 0 {
		t.Errorf("boats left = %d, want 0: the laid one was spent", got.Count)
	}
}

// TestAnAdventurePlayerLaysABoat: BoatItem.use has no game-mode test beyond
// spectator, so an adventure player lays and spends a boat.
func TestAnAdventurePlayerLaysABoat(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "adventure", "Porter", 1)
	c.MoveToGround(2.5, 65, 2.5, true)
	c.FacePlayer(2.5, 65, 2.5, 0, 90)
	c.Settle()

	c.UseBoatItem(0, 90)
	spawned := servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)
	if spawned.X != 2.5 || spawned.Y != 64 || spawned.Z != 2.5 {
		t.Errorf("boat spawned at (%v %v %v), want (2.5, 64, 2.5)", spawned.X, spawned.Y, spawned.Z)
	}
	if got := srv.InventorySlot("Porter", player.HotbarSlot); got.Count != 0 {
		t.Errorf("boats left = %d, want 0: the laid one was spent", got.Count)
	}
}

// TestABoatIsRefusedWhereItsBoxDoesNotFit: a boat item used on a spot the
// boat's box intersects stands nothing and keeps the item — the water of a
// one-by-one pool would have it, the stone of the pool's side crossing the
// box's reach out of the water it stands on.
func TestABoatIsRefusedWhereItsBoxDoesNotFit(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "survival", "Packer", 1)
	srv.SetBlock(0, 63, 0, servertest.MustState(t, "minecraft:water", nil))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.FacePlayer(0.5, 64, 0.5, 0, 90)
	c.Settle()

	servertest.HoldTicks(t, srv)
	c.UseBoatItem(0, 90)
	refused := c.PumpTicks(srv, 100, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		_, typ := servertest.SpawnEntityHead(data)
		return typ == protocol.EntityTypeOakBoat
	})
	if refused {
		t.Error("a boat was laid where its box crosses the pool's side")
	}
	if got := srv.InventorySlot("Packer", player.HotbarSlot); got.Count != 1 {
		t.Errorf("boats left = %d, want 1: the kept one is not spent", got.Count)
	}
}

// TestABoatMountsItsClickerAndSneakDismounts: a right-click on a boat takes
// the clicker aboard, the passengers packet naming them for every viewer —
// the clicker's own among them, at the seat the server keeps them — and
// the sneak of the Player Input packet puts them off again, standing on
// the hull.
func TestABoatMountsItsClickerAndSneakDismounts(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Rower", 0)
	boatID := srv.SpawnBoat(2.5, 64, 0.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}

	// A sneaking hand boards nothing: vanilla's interact mounts no
	// secondary use.
	c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(boatID)
		b.PutVarInt(protocol.HandMain)
		b.PutLpVec3(0.5, 0.5, 0.5)
		b.PutBool(true) // sneaking
	})
	c.Settle()
	if riders := srv.BoatPassengerIDs(boatID); len(riders) != 0 {
		t.Fatalf("the sneaking click boarded %v, want nobody", riders)
	}

	c.UseOn(boatID)
	if riders := servertest.AwaitPassengersFor(c, boatID); len(riders) != 1 || riders[0] != c.PlayerEntityID {
		t.Errorf("passengers = %v, want the clicker's own %d", riders, c.PlayerEntityID)
	}
	if rider, ok := srv.BoatRider(boatID); !ok || rider != "Rower" {
		t.Errorf("the boat's rider = %q (%v), want Rower", rider, ok)
	}
	// The seat subtracts the player's 0.6 from the hull's attachment. The
	// passengers go out before the rider is moved to the seat, so the click
	// is settled before the seat is read.
	c.Settle()
	if got := srv.PlayerPosition("Rower"); servertest.Abs64(got.Y-(64+rules.BoatSeatY-0.6)) > 0.001 {
		t.Errorf("the rider sits at y=%v, want the seat %v", got.Y, 64+rules.BoatSeatY-0.6)
	}

	// A sneak of the driver's steps them off, onto the hull.
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) {
		b.PutU8(protocol.InputSneak)
	})
	if riders := servertest.AwaitPassengersFor(c, boatID); len(riders) != 0 {
		t.Errorf("passengers after the sneak = %v, want none", riders)
	}
	if _, ok := srv.BoatRider(boatID); ok {
		t.Error("the boat still holds a rider after the sneak")
	}
	// The step leaves them beside the hull on the ground, dry: vanilla's
	// getDismountLocationForPassenger.
	if pos := srv.PlayerPosition("Rower"); servertest.Abs64(pos.Y-64) > 0.001 {
		t.Errorf("the dismounted rider stands at y=%v, want the ground's top 64", pos.Y)
	} else if servertest.Abs64(pos.X-2.5) < 1e-9 && servertest.Abs64(pos.Z-0.5) < 1e-9 {
		t.Error("the dismounted rider stands where the hull was, want the step out beside it")
	}
}

// boatDismountStep is where the escape vector puts a rider stepping off a
// hull standing at (bx, bz) and facing yaw: half the gap the hull's diagonal
// width and the rider's own make, along the look.
func boatDismountStep(bx, bz float64, yaw float32) (float64, float64) {
	half := (rules.BoatHalfWidth*2*math.Sqrt2 + 0.6 + 1e-5) / 2
	rad := float64(yaw) * math.Pi / 180
	dx, dz := -math.Sin(rad), math.Cos(rad)
	if scale := math.Max(math.Abs(dx), math.Abs(dz)); scale != 0 {
		dx, dz = dx/scale, dz/scale
	}
	return bx + dx*half, bz + dz*half
}

// boatRidden stands a boat at the place given with the tick stopped — so the
// blocks around it stay as the case sets them — and boards the rider on it.
func boatRidden(t *testing.T, srv *server.Server, c *servertest.Client, yaw float32, bx, by, bz float64) int32 {
	t.Helper()
	c.MoveTo(bx, by, bz, yaw, 0)
	c.Settle()
	servertest.HoldTicks(t, srv)
	boatID := srv.SpawnBoat(bx, by, bz, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)
	if got := srv.BoatPassengerIDs(boatID); len(got) != 1 {
		t.Fatalf("the boat holds %v, want the rider aboard", got)
	}
	return boatID
}

// boatSneakOff sneaks a rider off their boat and reads what their own client
// is sent: the Synchronize Player Position they are stepped to, and the pose
// the step drew them in, or -1 where the step left the pose alone. The seat
// has to be given up first, since a client ignores a position applied while
// it still rides. The pose is read off every metadata packet on the way, the
// sneak's own included: a step whose pose the body already holds sends none
// of its own.
func boatSneakOff(srv *server.Server, c *servertest.Client, boatID int32) (player.Position, int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	pose := int32(-1)
	readPose := func(data []byte) {
		md := servertest.ParseMetadata(c.T, protocol.NewReader(data))
		if md.EntityID == c.PlayerEntityID {
			pose = md.Pose
		}
	}
	var pos player.Position
	seatGiven, stepped := false, false
	if !c.ReadTicksSkipping(srv, 100, servertest.BoatSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySetPassengers:
			vehicle, riders := servertest.ReadPassengers(c, data)
			if vehicle == boatID {
				seatGiven = true
				if len(riders) != 0 {
					c.T.Errorf("passengers after the sneak = %v, want none", riders)
				}
			}
		case protocol.IDPlayEntityMetadata:
			readPose(data)
		case protocol.IDPlaySyncPosition:
			r := protocol.NewReader(data)
			r.ReadVarInt() // the teleport ID
			pos.X, pos.Y, pos.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
			r.ReadF64() // velocity
			r.ReadF64()
			r.ReadF64()
			pos.Yaw, pos.Pitch = r.ReadF32(), r.ReadF32()
			// The dismount keeps the client's look and velocity, so the
			// rotation and the velocity delta travel relative.
			want := int32(0x08 | 0x10 | 0x20 | 0x40 | 0x80 | 0x100)
			if flags := r.ReadI32(); flags != want {
				c.T.Errorf("the step off carries teleport flags %#x, want relative rotation and delta %#x", flags, want)
			}
			if err := r.Err(); err != nil {
				c.T.Fatalf("synchronize player position: %v", err)
			}
			if r.Remaining() != 0 {
				c.T.Errorf("synchronize player position has %d bytes left over", r.Remaining())
			}
			stepped = true
		}
		return seatGiven && stepped
	}) {
		c.T.Fatal("the sneak sent the client no position")
	}
	c.ReadTicksSkipping(srv, 20, servertest.BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		readPose(data)
		return true
	})
	return pos, pose
}

// assertSteppedOutOfTheHull fails when a step off lands inside the hull's box
// rather than beside or on top of it.
func assertSteppedOutOfTheHull(t *testing.T, pos player.Position, bx, by, bz float64) {
	t.Helper()
	const eps = 1e-4
	insideX := pos.X > bx-rules.BoatHalfWidth+eps && pos.X < bx+rules.BoatHalfWidth-eps
	insideZ := pos.Z > bz-rules.BoatHalfWidth+eps && pos.Z < bz+rules.BoatHalfWidth-eps
	insideY := pos.Y > by+eps && pos.Y < by+rules.BoatHeight-eps
	if insideX && insideZ && insideY {
		t.Errorf("the step off stands inside the hull's box at (%v %v %v)", pos.X, pos.Y, pos.Z)
	}
}

// TestABoatStepsItsRiderOffOnOpenGround: a rider sneaking off a boat on open
// ground is sent beside the hull, on the side they face, crouched on the
// ground.
func TestABoatStepsItsRiderOffOnOpenGround(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Stepper", 0)
	grass := servertest.MustState(t, "minecraft:grass_block", nil)
	for x := int32(0); x <= 3; x++ {
		for z := int32(0); z <= 2; z++ {
			srv.SetBlock(x, 63, z, grass)
		}
	}
	boatID := boatRidden(t, srv, c, 0, 2.5, 64, 0.5)
	pos, pose := boatSneakOff(srv, c, boatID)
	wantX, wantZ := boatDismountStep(2.5, 0.5, 0)
	if servertest.Abs64(pos.X-wantX) > 1e-3 || servertest.Abs64(pos.Z-wantZ) > 1e-3 || servertest.Abs64(pos.Y-64) > 1e-3 {
		t.Errorf("the step off stands at (%v %v %v), want beside the hull (%v %v 64)", pos.X, pos.Y, pos.Z, wantX, wantZ)
	}
	if pose != protocol.PoseSneaking {
		t.Errorf("the step off draws the rider in pose %d, want the crouch %d", pose, protocol.PoseSneaking)
	}
	assertSteppedOutOfTheHull(t, pos, 2.5, 64, 0.5)
}

// TestAReleasedSneakStandsTheSteppedOffRider: the sneak that steps a rider off
// a boat is held through the step, so the body is drawn crouched beside the
// hull; releasing it stands them where they were stepped, the pose this server
// derives on the input change rather than every tick.
func TestAReleasedSneakStandsTheSteppedOffRider(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Released", 0)
	grass := servertest.MustState(t, "minecraft:grass_block", nil)
	for x := int32(0); x <= 3; x++ {
		for z := int32(0); z <= 2; z++ {
			srv.SetBlock(x, 63, z, grass)
		}
	}
	boatID := boatRidden(t, srv, c, 0, 2.5, 64, 0.5)
	if _, pose := boatSneakOff(srv, c, boatID); pose != protocol.PoseSneaking {
		t.Fatalf("the step off draws the sneaking rider in pose %d, want the crouch %d", pose, protocol.PoseSneaking)
	}
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(0) })
	if got := c.ExpectMetadata(); got.Pose != protocol.PoseStanding {
		t.Errorf("releasing the sneak draws the rider in pose %d, want standing %d", got.Pose, protocol.PoseStanding)
	}
}

// TestABoatStepsItsRiderOffOverOpenWater: with no floor beside the hull on
// open water, the rider comes back on top of the boat, in the water.
func TestABoatStepsItsRiderOffOverOpenWater(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Swimmer", 0)
	water := servertest.MustState(t, "minecraft:water", nil)
	for x := int32(0); x <= 3; x++ {
		for z := int32(0); z <= 2; z++ {
			srv.SetBlock(x, 63, z, water)
		}
	}
	by := 63.0 + 8.0/9.0
	boatID := boatRidden(t, srv, c, 0, 2.5, by, 0.5)
	pos, _ := boatSneakOff(srv, c, boatID)
	if servertest.Abs64(pos.X-2.5) > 1e-3 || servertest.Abs64(pos.Z-0.5) > 1e-3 || servertest.Abs64(pos.Y-(by+rules.BoatHeight)) > 1e-3 {
		t.Errorf("the step off over water stands at (%v %v %v), want on top of the hull (2.5 %v 0.5)",
			pos.X, pos.Y, pos.Z, by+rules.BoatHeight)
	}
	if got := srv.BlockNameAt(2, 63, 1); got != "minecraft:water" {
		t.Errorf("the step off's cell below = %s, want the water the hull floats on", got)
	}
	assertSteppedOutOfTheHull(t, pos, 2.5, by, 0.5)
}

// TestABoatStepsItsRiderOffOntoAShore: a hull floating beside land steps its
// rider out onto the shore on the side they face.
func TestABoatStepsItsRiderOffOntoAShore(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Shoreman", 0)
	water := servertest.MustState(t, "minecraft:water", nil)
	for x := int32(0); x <= 1; x++ {
		for z := int32(0); z <= 2; z++ {
			srv.SetBlock(x, 63, z, water)
		}
	}
	by := 63.0 + 8.0/9.0
	boatID := boatRidden(t, srv, c, 270, 1.5, by, 1.5) // facing +X, to the shore
	pos, pose := boatSneakOff(srv, c, boatID)
	wantX, wantZ := boatDismountStep(1.5, 1.5, 270)
	if servertest.Abs64(pos.X-wantX) > 1e-3 || servertest.Abs64(pos.Z-wantZ) > 1e-3 || servertest.Abs64(pos.Y-64) > 1e-3 {
		t.Errorf("the step off stands at (%v %v %v), want on the shore beside the hull (%v %v 64)", pos.X, pos.Y, pos.Z, wantX, wantZ)
	}
	if pose != protocol.PoseSneaking {
		t.Errorf("the step off draws the rider in pose %d, want the crouch %d", pose, protocol.PoseSneaking)
	}
	assertSteppedOutOfTheHull(t, pos, 1.5, by, 1.5)
}

// TestABoatStepsItsRiderOffWithAWallInTheWay: a hull against a wall on the
// side the rider faces cannot step them into it, so they come back on top of
// the boat rather than inside the wall or the hull.
func TestABoatStepsItsRiderOffWithAWallInTheWay(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Boxed", 0)
	wall := servertest.MustState(t, "minecraft:stone", nil)
	for x := int32(0); x <= 3; x++ {
		for z := int32(0); z <= 2; z++ {
			srv.SetBlock(x, 63, z, wall)
		}
	}
	// A wall on the facing (+Z) side of the hull.
	srv.SetBlock(2, 64, 1, wall)
	srv.SetBlock(2, 65, 1, wall)
	boatID := boatRidden(t, srv, c, 0, 2.5, 64, 0.5)
	pos, _ := boatSneakOff(srv, c, boatID)
	if servertest.Abs64(pos.X-2.5) > 1e-3 || servertest.Abs64(pos.Z-0.5) > 1e-3 || servertest.Abs64(pos.Y-(64+rules.BoatHeight)) > 1e-3 {
		t.Errorf("the step off stands at (%v %v %v), want on top of the hull (2.5 %v 0.5)",
			pos.X, pos.Y, pos.Z, 64+rules.BoatHeight)
	}
	if pos.Z > 1 {
		t.Errorf("the step off stands at z=%v, inside the wall on the facing side", pos.Z)
	}
}

// boatLowStep puts the step-off spot at (2.5, 0.5) under a ceiling only a
// crouching body clears: a slab step beside the hull with stone a block and
// a half over it.
func boatLowStep(t *testing.T, srv *server.Server) {
	t.Helper()
	srv.SetBlock(2, 64, 1, servertest.MustState(t, "minecraft:oak_slab", nil))
	srv.SetBlock(2, 66, 1, servertest.MustState(t, "minecraft:stone", nil))
}

// TestABoatStepsItsRiderOffUnderALowCeiling: with only a crouching body's box
// clearing the step spot, the step draws the rider crouched. That is the pose
// the sneak already asked for, so no metadata follows the seat and the crouch
// is read off the sneak's own packet.
func TestABoatStepsItsRiderOffUnderALowCeiling(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Croucher", 0)
	boatLowStep(t, srv)
	boatID := boatRidden(t, srv, c, 0, 2.5, 64, 0.5)
	pos, pose := boatSneakOff(srv, c, boatID)
	if pose != protocol.PoseSneaking {
		t.Errorf("the step off draws the rider in pose %d, want the crouch %d", pose, protocol.PoseSneaking)
	}
	if servertest.Abs64(pos.Y-64.5) > 1e-3 {
		t.Errorf("the crouching step off stands at y=%v, want the slab's top 64.5", pos.Y)
	}
}

// vanilla: LivingEntity.dismountVehicle
func TestABrokenBoatKeepsItsRidersColumn(t *testing.T) {
	t.Parallel()
	for _, slab := range []bool{false, true} {
		name := "ground"
		wantY := 64.0
		if slab {
			name, wantY = "slab", 64.5
		}
		t.Run(name, func(t *testing.T) {
			srv, c, _ := servertest.BoatPlayer(t, "creative", "Wrecked", 0)
			boatID := boatRidden(t, srv, c, 0, 2.5, 64, 0.5)
			if slab {
				// The removed-vehicle search must lift the rider clear of this box.
				srv.SetBlock(2, 64, 0, servertest.MustState(t, "minecraft:oak_slab", nil))
			}
			c.Settle()
			srv.DamageBoat(boatID, 1000)
			servertest.RunTicks(t, srv, 1)
			if _, ok := srv.BoatRider(boatID); ok {
				t.Error("the broken boat still holds the rider")
			}
			if riders := servertest.AwaitPassengersFor(c, boatID); len(riders) != 0 {
				t.Fatalf("broken boat passengers = %v, want none", riders)
			}
			if !c.ReadTicksSkipping(srv, 1, servertest.BoatSkip, func(id int32, data []byte) bool {
				if id != protocol.IDPlaySyncPosition {
					return false
				}
				pos := servertest.ReadSyncPosition(t, data)
				if servertest.Abs64(pos.X-2.5) > 1e-6 || servertest.Abs64(pos.Z-0.5) > 1e-6 || servertest.Abs64(pos.Y-wantY) > 1e-6 {
					t.Errorf("broken boat dismount = (%v %v %v), want (2.5 %v 0.5)", pos.X, pos.Y, pos.Z, wantY)
				}
				return true
			}) {
				t.Fatal("the broken boat sent the rider no position after emptying the seat")
			}
		})
	}
}

// vanilla: LivingEntity.dismountVehicle, AbstractBoat.getDismountLocationForPassenger
func TestABoatDismountsMobPassengersAccordingToItsRemoval(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		dead    bool
		ceiling bool
		rear    bool
	}{
		{name: "destroyed", dead: true},
		{name: "destroyed-in-ceiling", dead: true, ceiling: true},
		{name: "destroyed-rear-seat", dead: true, rear: true},
		{name: "live"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Wrecked", servertest.OpsListing(t, "Wrecked"))
			servertest.HoldTicks(t, srv)
			by := 64.0
			if tc.ceiling {
				by = 64.5
			}
			passengers := `{id:"minecraft:pig",NoAI:1b}`
			if tc.rear {
				passengers = `{id:"minecraft:chicken",NoAI:1b},` + passengers
			}
			c.ChatCommand(fmt.Sprintf(`summon oak_boat 2.5 %g 2.5 {NoGravity:1b,Passengers:[%s]}`, by, passengers))
			c.Settle()
			boats, mobs := srv.BoatIDs(), srv.Mobs()
			if len(boats) != 1 || len(mobs) == 0 {
				t.Fatalf("summoned boats = %v, mobs = %v, want a boat and its pig", boats, mobs)
			}
			boat := boats[0]
			pigIndex := slices.IndexFunc(mobs, func(m server.TestMob) bool { return m.Kind == "minecraft:pig" })
			if pigIndex < 0 {
				t.Fatal("summoned no pig")
			}
			pig := mobs[pigIndex].ID
			servertest.RunTicks(t, srv, 1)
			seat := srv.Mobs()[pigIndex]
			if got := srv.BoatPassengerIDs(boat); !slices.Contains(got, pig) {
				t.Fatalf("boat passengers = %v, want pig %d aboard", got, pig)
			}
			if tc.ceiling {
				// The ceiling crosses the seated body; its top is within half a pig's height.
				srv.SetBlock(2, 64, 2, servertest.MustState(t, "minecraft:stone", nil))
			}
			if tc.dead {
				srv.DamageBoat(boat, 1000)
				servertest.RunTicks(t, srv, 1)
			} else {
				c.ChatCommand("ride @e[type=pig,limit=1] dismount")
				c.Settle()
			}
			if got := srv.BoatPassengerIDs(boat); len(got) != 0 {
				t.Fatalf("dismounted boat passengers = %v, want none", got)
			}
			got := srv.Mobs()[pigIndex]
			wantX, wantY, wantZ := seat.X, seat.Y, seat.Z
			if tc.ceiling {
				wantY = 65
			} else if !tc.dead {
				// vanilla: Entity.getCollisionHorizontalEscapeVector
				wantY = by
				wantZ += (rules.BoatHalfWidth*2*math.Sqrt2 + float64(float32(0.9)) + 1e-5) / 2
			}
			if math.Abs(got.X-wantX) > 1e-6 || math.Abs(got.Y-wantY) > 1e-6 || math.Abs(got.Z-wantZ) > 1e-6 {
				t.Errorf("pig dismount = (%g %g %g), want (%g %g %g)", got.X, got.Y, got.Z, wantX, wantY, wantZ)
			}
		})
	}
}

// TestADyingRiderIsNotSteppedOffTheBoat: a rider whose health runs out on a
// boat is left where the seat held them; the death's dismount neither moves
// the body, re-poses it, nor sends it a position.
func TestADyingRiderIsNotSteppedOffTheBoat(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "survival", "Dier", 0)
	// A step spot only a crouching body clears: a dismount that ran would
	// draw the standing body crouched and send it a position.
	boatLowStep(t, srv)
	boatID := boatRidden(t, srv, c, 0, 2.5, 64, 0.5)
	seat := srv.PlayerPosition("Dier")
	srv.Hurt("Dier", 1000)
	if _, ok := srv.BoatRider(boatID); ok {
		t.Error("the boat still holds the rider after the death")
	}
	if got := srv.PlayerPosition("Dier"); servertest.Abs64(got.X-seat.X) > 1e-6 || servertest.Abs64(got.Y-seat.Y) > 1e-6 || servertest.Abs64(got.Z-seat.Z) > 1e-6 {
		t.Errorf("the dead body was stepped off to %+v, want it left at the seat %+v", got, seat)
	}
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySyncPosition {
			t.Error("the death's dismount sent the dead body's client a position")
		}
		if id == protocol.IDPlayEntityMetadata {
			t.Error("the death's dismount broadcast metadata about the dead body")
		}
	})
}

// TestARidingPlayerCrossingDimensionsLeavesTheSeat: a rider carried to
// another dimension steps out of the seat — the boat stays behind — and the
// crossing's own arrival still lands them. The seat is given back without the
// dismount's step: a position sent before the Respawn would reach a client
// still standing in the old world.
func TestARidingPlayerCrossingDimensionsLeavesTheSeat(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Traveller", 0)
	boatID := boatRidden(t, srv, c, 0, 2.5, 64, 0.5)
	c.Settle()
	srv.MovePlayerToDimension("Traveller", world.NetherName, 8.5, 64, 8.5)
	if !c.ReadTicksSkipping(srv, 200, servertest.BoatSkip, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySyncPosition {
			t.Error("a Synchronize Player Position reached the rider before the crossing's Respawn")
		}
		return id == protocol.IDPlayRespawn
	}) {
		t.Fatal("the crossing sent the rider no Respawn")
	}
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.PlayerDimension("Traveller") == world.NetherName
	})
	if _, ok := srv.BoatRider(boatID); ok {
		t.Error("the boat still holds the rider after the crossing")
	}
	if got := srv.PlayerPosition("Traveller"); got.X != 8.5 || got.Z != 8.5 {
		t.Errorf("the traveller stands at (%v %v), want the nether arrival (8.5, 8.5)", got.X, got.Z)
	}
}

// TestALeavingRiderIsNotSteppedOff: a rider who disconnects is taken off the
// seat for the others, but no pose is drawn for a body about to be removed.
func TestALeavingRiderIsNotSteppedOff(t *testing.T) {
	t.Parallel()
	srv, c, connect := servertest.BoatPlayer(t, "creative", "Leaver", 0)
	// A step spot only a crouching body clears: a dismount that ran would
	// draw the standing body crouched for the watchers.
	boatLowStep(t, srv)
	boatID := boatRidden(t, srv, c, 0, 2.5, 64, 0.5)

	watcher := connect().Join("Watcher")
	watcher.Settle()

	c.Disconnect()
	removed := false
	reposed := false
	watcher.ReadTicksSkipping(srv, 100, servertest.BoatSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityMetadata:
			if pose := servertest.BoatPlayerPose(t, c.PlayerEntityID, data); pose >= 0 {
				reposed = true
			}
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for n := r.ReadVarInt(); n > 0 && r.Err() == nil; n-- {
				if r.ReadVarInt() == c.PlayerEntityID {
					removed = true
				}
			}
		}
		return removed
	})
	if reposed {
		t.Error("the leaving rider's body was re-posed for the watcher")
	}
	if _, ok := srv.BoatRider(boatID); ok {
		t.Error("the leaving rider is still aboard")
	}
}

// TestABoatCarriesItsDriverAtTheirReport: the driver's Move Vehicle report
// stands the boat there and the driver with it — the watchers read the
// boat's new place off the movement packets and the server keeps the rider
// at the seat the boat holds.
func TestABoatCarriesItsDriverAtTheirReport(t *testing.T) {
	t.Parallel()
	srv, c, connect := servertest.BoatPlayer(t, "creative", "Skipper", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)

	// A second client watches, so the boat's movement goes to them and not
	// the driver whose client runs it.
	watcher := connect().Join("Watcher")
	watcher.Settle()

	c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(6.5)
		b.PutF64(64)
		b.PutF64(5.5)
		b.PutF32(90)
		b.PutF32(0)
		b.PutBool(false)
	})

	// The watcher is told the boat's new standing, whatever shape the
	// step's size travels it as.
	moved := false
	watcher.ReadTicksSkipping(srv, 400, servertest.BoatSkip, func(id int32, data []byte) bool {
		var r *protocol.Reader
		switch id {
		case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot, protocol.IDPlayTeleportEntity:
			r = protocol.NewReader(data)
		default:
			return false
		}
		if about := r.ReadVarInt(); about != boatID {
			return false // another body's step
		}
		moved = true
		return true
	})
	if !moved {
		t.Fatal("the watcher was never told of the driven boat's move")
	}
	// The report is applied on the driver's own connection, which carries
	// the boat and then the rider's seat; a tick can broadcast the boat's
	// new place to the watcher before that connection has moved the seat.
	// Read each only once it holds: the boat where it was told, and the
	// rider at the boat's passenger attachment less their own 0.6,
	// a lone rider at its centre.
	servertest.WaitFor(t, srv, 200, func() bool {
		x, y, _, _, _, ok := srv.BoatState(boatID)
		return ok && x == 6.5 && y == 64
	})
	servertest.WaitFor(t, srv, 200, func() bool {
		got := srv.PlayerPosition("Skipper")
		return servertest.Abs64(got.X-6.5) <= 0.001 && servertest.Abs64(got.Y-(64+rules.BoatSeatY-0.6)) <= 0.001 && got.Z == 5.5
	})
}

// TestAPaddleReportShowsInMetadata: the driver's paddle report rides the
// boat's metadata out to the watchers, and a dismount stills them.
func TestAPaddleReportShowsInMetadata(t *testing.T) {
	t.Parallel()
	srv, c, connect := servertest.BoatPlayer(t, "creative", "Paddler", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)

	watcher := connect().Join("Watcher")
	watcher.Settle()

	c.Send(protocol.IDPlayPaddleBoatIn, func(b *protocol.Buffer) {
		b.PutBool(true)  // left paddle
		b.PutBool(false) // right paddle
	})
	// The metadata carries the turn alone: the left paddle's swing is
	// news, the right's stillness is not, being no turn from the default.
	fields := servertest.AwaitBoatMetadata(watcher, boatID)
	if got, ok := fields[int32(protocol.MetadataIndexBoatLeftPaddle)]; !ok || got != true {
		t.Errorf("left paddle field = %v (%v), want true", got, ok)
	}
	if got, ok := fields[int32(protocol.MetadataIndexBoatRightPaddle)]; ok {
		t.Errorf("right paddle field = %v, want none: it never turned", got)
	}

	// The step off stills the paddles: the swing back to stillness is
	// news the same way the swing out was.
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) {
		b.PutU8(protocol.InputSneak)
	})
	fields = servertest.AwaitBoatMetadata(watcher, boatID)
	if got, ok := fields[int32(protocol.MetadataIndexBoatLeftPaddle)]; !ok || got != false {
		t.Errorf("left paddle after the dismount = %v (%v), want the stillness", got, ok)
	}
}

// TestABlowsRockingRidesTheMetadata: a blow on a boat wounds the rocking —
// ten ticks, the direction count one further, the damage laid — which the
// metadata carries to the watchers, and the boat's health runs out into a
// break that leaves the boat it was.
func TestABlowsRockingRidesTheMetadataAndABrokenBoatLeavesItself(t *testing.T) {
	t.Parallel()
	srv, c, connect := servertest.BoatPlayer(t, "survival", "Wrecker", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}

	// A fist blow is the one damage it lays; the rocking is what says it
	// landed, carried to the watcher and the striker both.
	watcher := connect().Join("Watcher")
	watcher.Settle()

	// The striker's recovery starts winding at the join, and the whole
	// join here can run in fewer ticks than the fist's five, so wait on the
	// ticker rather than assume a full charge.
	servertest.AwaitFullSwing(t, srv, "Wrecker")

	c.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) {
		b.PutVarInt(boatID)
	})
	// A fist blow lays its own tenfold of damage, and turns the rocking's
	// direction about from its default of one.
	fields := servertest.AwaitBoatMetadata(watcher, boatID)
	if got, ok := fields[int32(protocol.MetadataIndexVehicleShake)]; !ok || got != int32(10) {
		t.Errorf("shake ticks = %v (%v), want the ten a blow lays", got, ok)
	}
	if got, ok := fields[int32(protocol.MetadataIndexVehicleShakeDirection)]; !ok || got != int32(-1) {
		t.Errorf("shake direction = %v (%v), want the default turned about to minus one", got, ok)
	}
	if got, ok := fields[int32(protocol.MetadataIndexVehicleShakeDamage)]; !ok || got != float32(10.0) {
		t.Errorf("shake damage = %v (%v), want the fist's own tenfold ten", got, ok)
	}

	// The health's end is the break: the body removed, the boat it was
	// laid from left behind it.
	srv.DamageBoat(boatID, 1000)
	removed := watcher.ReadTicksSkipping(srv, 40, servertest.BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayRemoveEntities {
			return false
		}
		r := protocol.NewReader(data)
		if count := r.ReadVarInt(); count != 1 {
			return false
		}
		return r.ReadVarInt() == boatID
	})
	if !removed {
		t.Fatal("the broken boat was never removed")
	}
	spawned := servertest.AwaitBoatSpawn(watcher, protocol.EntityTypeItem)
	// The drop stands where the wreck stood, at its feet
	if servertest.Abs64(spawned.X-4.5) > 0.001 || servertest.Abs64(spawned.Z-4.5) > 0.001 {
		t.Errorf("the drop stands at (%v %v), want the wreck's column (4.5, 4.5)", spawned.X, spawned.Z)
	}
	// The drop stands where the wreck stood — at its feet, a fall of a
	// tick or two below them by the time the watcher reads it — and not
	// the half-boat's width above the old drop stood at.
	if spawned.Y > 64.0625 || spawned.Y < 63.5 {
		t.Errorf("the drop stands at y=%v, want the wreck's feet at 64", spawned.Y)
	}
	if got := srv.ItemCount(); got < 1 {
		t.Errorf("items standing = %d, want the boat the wreck left", got)
	}
	if got := srv.BoatCount(); got != 0 {
		t.Errorf("boats standing = %d, want none", got)
	}
}

// TestABoatBlowLaysTheStrikersChargedShare: the rocking's damage is the
// striker's charged blow and not a fixed ten, so a fist blow spent by a
// swing and wound four ticks of its five lays the 0.9 charge's own share.
func TestABoatBlowLaysTheStrikersChargedShare(t *testing.T) {
	t.Parallel()
	srv, c, connect := servertest.BoatPlayer(t, "survival", "Wrecker", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	watcher := connect().Join("Watcher")
	watcher.Settle()

	// Hold the loop so the recovery winds by hand to a known count: the
	// swing spends it, and four tick passes leave the fist a tick short of
	// its five-tick recovery.
	srv.StopTick()
	c.Send(protocol.IDPlayPunch, func(b *protocol.Buffer) {})
	c.Settle()
	for range 4 {
		srv.TickSurvival()
	}
	c.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(boatID) })

	// The blow the fist's four-tick recovery carries, laid on the boat as
	// its own tenfold.
	strength := rules.AttackStrengthScale(4, rules.AttackSpeed(protocol.ItemStack{}))
	want := float32(rules.AttackDamage(1, false, strength) * 10)
	fields := servertest.AwaitBoatMetadata(watcher, boatID)
	if got, ok := fields[int32(protocol.MetadataIndexVehicleShakeDamage)]; !ok || got != want {
		t.Errorf("shake damage = %v (%v), want the 0.9 charge's own %v", got, ok, want)
	}
}

// TestABoatTakesASecondPassenger: a plain boat seats two — vanilla's
// getMaxPassengers — so a second player's click boards them beside the
// first, and the passengers packet the whole dimension hears names them
// both in mounting order. The first aboard keeps the driving.
func TestABoatTakesASecondPassenger(t *testing.T) {
	t.Parallel()
	srv, c, connect := servertest.BoatPlayer(t, "creative", "First", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	second := connect().Join("Second")
	second.Settle()

	c.UseOn(boatID)
	first := servertest.AwaitPassengersFor(c, boatID)
	second.UseOn(boatID)
	riders := servertest.AwaitPassengersFor(c, boatID)
	if len(riders) != 2 {
		t.Fatalf("passengers = %v, want the two aboard", riders)
	}
	if riders[0] != first[0] || riders[1] != second.PlayerEntityID {
		t.Errorf("passengers = %v, want the first aboard (%v) and the second's %d", riders, first[0], second.PlayerEntityID)
	}
	if d, ok := srv.BoatRider(boatID); !ok || d != "First" {
		t.Errorf("the driver = %q (%v), want the first aboard to keep it", d, ok)
	}
	ids := srv.BoatPassengerIDs(boatID)
	if len(ids) != 2 {
		t.Errorf("the boat holds %d seats, want two", len(ids))
	}
	// The second boarding walks the first from the hull's centre to the
	// bow seat, the second taking the stern, both 0.1875 - 0.6 up the hull.
	servertest.WaitFor(t, srv, 200, func() bool {
		first := srv.PlayerPosition("First")
		second := srv.PlayerPosition("Second")
		return servertest.Abs64(first.Z-4.7) < 0.001 && servertest.Abs64(second.Z-3.9) < 0.001 &&
			servertest.Abs64(first.Y-(64+rules.BoatSeatY-0.6)) < 0.001
	})

	// The driver's report carries the passenger with it: the boat stands
	// where the driving client says, the first rider two tenths toward the
	// bow and the second six tenths the other way, both 0.1875 - 0.6 up the hull.
	c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(6.5)
		b.PutF64(64)
		b.PutF64(5.5)
		b.PutF32(0)
		b.PutF32(0)
		b.PutBool(false)
	})
	servertest.WaitFor(t, srv, 200, func() bool {
		got := srv.PlayerPosition("Second")
		return servertest.Abs64(got.X-6.5) < 0.001 && servertest.Abs64(got.Z-4.9) < 0.001 &&
			servertest.Abs64(got.Y-(64+rules.BoatSeatY-0.6)) < 0.001
	})
	servertest.WaitFor(t, srv, 200, func() bool {
		got := srv.PlayerPosition("First")
		return servertest.Abs64(got.X-6.5) < 0.001 && servertest.Abs64(got.Z-5.7) < 0.001
	})
	// The sneaking hand of a third player boards nothing, one seat free
	// as it might be. The sneaking mount is not a capacity question.
	third := connect().Join("Third")
	third.Settle()
	// A dismount frees the driver's seat first.
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) {
		b.PutU8(protocol.InputSneak)
	})
	servertest.AwaitPassengersFor(c, boatID)
	// The driver's seat given back, the second moves to the hull's centre.
	servertest.WaitFor(t, srv, 200, func() bool {
		got := srv.PlayerPosition("Second")
		return servertest.Abs64(got.X-6.5) < 0.001 && servertest.Abs64(got.Z-5.5) < 0.001 &&
			servertest.Abs64(got.Y-(64+rules.BoatSeatY-0.6)) < 0.001
	})
	third.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(boatID)
		b.PutVarInt(protocol.HandMain)
		b.PutLpVec3(0.5, 0.5, 0.5)
		b.PutBool(true) // sneaking, which mounts nothing
	})
	third.Settle()
	if got := len(srv.BoatPassengerIDs(boatID)); got != 1 {
		t.Errorf("the sneaking hand boarded beside a free seat (%d aboard), want it kept empty", got)
	}
}

// TestABoatIsNotLaidIntoAPickableBody: an eye standing inside a pickable
// body — a zombie about the Angler's own head — lays nothing, the use
// refusing what its eye can already see.
func TestABoatIsNotLaidIntoAPickableBody(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "survival", "Angler", 1)
	c.MoveTo(6.5, 64, 6.5, 0, 90) // straight down onto the stone
	c.Settle()

	// The zombie standing in the Angler's spot: its box runs from their
	// feet to past their eyes.
	zombie := srv.SpawnMob("minecraft:zombie", 6.5, 64, 6.5)
	if zombie == 0 {
		t.Fatal("SpawnMob came to nothing")
	}
	c.ReadTicksSkipping(srv, 200, servertest.BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		return r.ReadVarInt() == zombie
	})

	servertest.HoldTicks(t, srv)
	c.UseBoatItem(0, 90)
	refused := c.PumpTicks(srv, 100, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		return r.ReadVarInt() == protocol.EntityTypeOakBoat
	})
	if refused {
		t.Error("a boat was laid through the zombie about the laying eye")
	}
	if got := srv.InventorySlot("Angler", player.HotbarSlot); got.Count != 1 {
		t.Errorf("boats left = %d, want 1: the refused laying is not spent", got.Count)
	}
}

// TestABoatIsNotLaidThroughAPlayer: the eye inside another player's box —
// a pickable body like any other — lays nothing, the layer's own box
// excepted.
// vanilla: BoatItem.use's EntitySelector
func TestABoatIsNotLaidThroughAPlayer(t *testing.T) {
	t.Parallel()
	srv, c, connect := servertest.BoatPlayer(t, "survival", "Angler", 1)
	c.MoveTo(6.5, 64, 6.5, 0, 90)
	c.Settle()

	other := connect().Join("Standing")
	other.MoveTo(6.5, 64, 6.5, 0, 90)
	other.Settle()

	servertest.HoldTicks(t, srv)
	c.UseBoatItem(0, 90)
	laid := c.PumpTicks(srv, 100, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		return r.ReadVarInt() == protocol.EntityTypeOakBoat
	})
	if laid {
		t.Error("a boat was laid through the player standing at the laying eye")
	}
	if got := srv.InventorySlot("Angler", player.HotbarSlot); got.Count != 1 {
		t.Errorf("boats left = %d, want 1: the refused laying is not spent", got.Count)
	}
}

// awaitBoatCargo reads the 9x3 Open Screen a chest boat's use sends and the
// cargo slot 3 the Container Content then carries, the window a chest block
// opens.
func awaitBoatCargo(t *testing.T, srv *server.Server, c *servertest.Client) protocol.ItemStack {
	t.Helper()
	opened := false
	c.ReadTicksSkipping(srv, 200, servertest.BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayOpenScreen {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt() // the window ID
		if r.ReadVarInt() != protocol.MenuGeneric9x3 {
			return false
		}
		if err := r.Err(); err != nil {
			c.T.Fatalf("open screen: %v", err)
		}
		opened = true
		return true
	})
	if !opened {
		t.Fatal("the chest boat never opened its cargo")
	}
	r := protocol.NewReader(c.AwaitPlay(protocol.IDPlayContainerContent))
	r.ReadVarInt() // window ID
	r.ReadVarInt() // state ID
	slots := r.ReadVarInt()
	var cargo protocol.ItemStack
	for i := range slots {
		stack, ok := r.ReadSlot()
		if !ok {
			c.T.Fatalf("cargo slot %d did not read back", i)
		}
		if i == 3 {
			cargo = stack
		}
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("set container content: %v", err)
	}
	return cargo
}

// TestAChestBoatSeatsOneAndOpensItsCargo: a chest boat seats one — the
// chest takes the second seat — and a plain click on a free seat boards it,
// while a sneaking click, or a click once the seat is taken, opens the
// cargo over its 27 slots, the window a chest block opens.
// vanilla: AbstractBoat.interact, AbstractChestBoat.interact
func TestAChestBoatSeatsOneAndOpensItsCargo(t *testing.T) {
	t.Parallel()
	srv, c, connect := servertest.BoatPlayer(t, "creative", "Captain", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_chest_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the item registry has no diamond")
	}
	srv.StowBoatCargo(boatID, 3, protocol.ItemStack{ItemID: diamond, Count: 2})

	// A sneaking click on the empty chest boat opens the cargo: the
	// chest's own window, with the hull's holding sat in it.
	c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(boatID)
		b.PutVarInt(protocol.HandMain)
		b.PutLpVec3(0.5, 0.5, 0.5)
		b.PutBool(true) // the sneaking hand
	})
	if cargo := awaitBoatCargo(t, srv, c); cargo.ItemID != diamond || cargo.Count != 2 {
		t.Errorf("cargo slot 3 holds %+v, want the two diamonds stowed", cargo)
	}
	c.Send(protocol.IDPlayCloseContainerIn, func(b *protocol.Buffer) {
		b.PutVarInt(1)
	})

	// A plain click on the free seat boards the one seat, opening nothing.
	c.UseOn(boatID)
	if riders := servertest.AwaitPassengersFor(c, boatID); len(riders) != 1 || riders[0] != c.PlayerEntityID {
		t.Fatalf("passengers = %v, want the plain clicker aboard alone", riders)
	}

	// A second player's plain click, the seat now taken, opens the cargo —
	// a chest boat decks no second passenger.
	other := connect().Join("Bosun")
	other.Settle()
	other.UseOn(boatID)
	if cargo := awaitBoatCargo(t, srv, other); cargo.ItemID != diamond || cargo.Count != 2 {
		t.Errorf("cargo slot 3 holds %+v, want the two diamonds stowed", cargo)
	}
}

// TestABambooChestRaftSeatsOneAndOpensItsCargo: the bamboo chest raft is a
// ChestRaft, an AbstractChestBoat like the chest boats — one seat and a
// 27-slot chest — so a plain click on its free seat boards it and a click on
// the taken seat opens the cargo.
// vanilla: ChestRaft, AbstractChestBoat.interact
func TestABambooChestRaftSeatsOneAndOpensItsCargo(t *testing.T) {
	t.Parallel()
	srv, c, connect := servertest.BoatPlayer(t, "creative", "Captain", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:bamboo_chest_raft")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the item registry has no diamond")
	}
	srv.StowBoatCargo(boatID, 3, protocol.ItemStack{ItemID: diamond, Count: 2})

	// A sneaking click on the empty chest raft opens the cargo: the chest's
	// own window, with the hull's holding sat in it.
	c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(boatID)
		b.PutVarInt(protocol.HandMain)
		b.PutLpVec3(0.5, 0.5, 0.5)
		b.PutBool(true) // the sneaking hand
	})
	if cargo := awaitBoatCargo(t, srv, c); cargo.ItemID != diamond || cargo.Count != 2 {
		t.Errorf("cargo slot 3 holds %+v, want the two diamonds stowed", cargo)
	}
	c.Send(protocol.IDPlayCloseContainerIn, func(b *protocol.Buffer) {
		b.PutVarInt(1)
	})

	// A plain click on the free seat boards the one seat the chest leaves.
	c.UseOn(boatID)
	if riders := servertest.AwaitPassengersFor(c, boatID); len(riders) != 1 || riders[0] != c.PlayerEntityID {
		t.Fatalf("passengers = %v, want the plain clicker aboard alone", riders)
	}
	if got := len(srv.BoatPassengerIDs(boatID)); got != 1 {
		t.Errorf("the chest raft holds %d seats, want the one the chest leaves", got)
	}

	// A second player's plain click, the seat now taken, opens the cargo —
	// a chest raft decks no second passenger.
	other := connect().Join("Bosun")
	other.Settle()
	other.UseOn(boatID)
	if cargo := awaitBoatCargo(t, srv, other); cargo.ItemID != diamond || cargo.Count != 2 {
		t.Errorf("cargo slot 3 holds %+v, want the two diamonds stowed", cargo)
	}
}

// TestARaftSeatsItsRiderHigherThanABoat: both rafts use vanilla's
// Raft.rideHeight attachment of 0.5, where boats use 0.1875. The rider
// sits 0.6 below either attachment.
// vanilla: Raft.rideHeight, ChestRaft.rideHeight
func TestARaftSeatsItsRiderHigherThanABoat(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecraft:bamboo_raft", "minecraft:bamboo_chest_raft"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.BoatPlayer(t, "creative", "Rafter", 0)
			servertest.HoldTicks(t, srv)
			boatID := srv.SpawnBoat(4.5, 64, 4.5, kind)
			if boatID == 0 {
				t.Fatalf("SpawnBoat came to nothing for %s", kind)
			}
			c.UseOn(boatID)
			servertest.AwaitPassengersFor(c, boatID)
			c.Settle()
			if got := srv.PlayerPosition("Rafter"); servertest.Abs64(got.Y-63.9) > 0.001 {
				t.Errorf("the rider on %s sits at y=%v, want the raft's seat 63.9", kind, got.Y)
			}
		})
	}
}

// TestKillOnAChestBoatLeavesNoItemButSpillsItsCargo: /kill is Entity.kill,
// remove(KILLED), which never reaches VehicleEntity.destroy where the boat
// item is laid, so the hull goes leaving no item; a chest boat's own remove
// spills its cargo for any reason that destroys, KILLED among them.
// vanilla: Entity.kill, AbstractChestBoat.remove
func TestKillOnAChestBoatLeavesNoItemButSpillsItsCargo(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.BoatPlayer(t, "survival", "Reaper", 0)
	srv.StopTick()
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_chest_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	srv.StowBoatCargo(boatID, 0, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond"), Count: 3})

	srv.RunLine("kill @e[type=minecraft:oak_chest_boat]")
	srv.TickBoats(1)

	if got := srv.BoatCount(); got != 0 {
		t.Errorf("boats standing = %d, want the killed hull gone", got)
	}
	items := srv.ItemsInDimension(world.DimensionName)
	if servertest.BlastLeftItem(items, "minecraft:oak_chest_boat") {
		t.Error("the killed hull left its boat item behind")
	}
	if got := servertest.ItemCountOf(items, "minecraft:diamond"); got != 3 {
		t.Errorf("the killed hull's cargo = %d diamonds, want the 3 it carried", got)
	}
}

// TestACreativeBreakOfAChestBoatSpillsAllItsCargo: a creative blow is
// discard(), a reason that destroys, so the hull goes with no boat item
// while all 27 slots of a chest boat's cargo still spill.
// vanilla: VehicleEntity.hurtServer's discard, AbstractChestBoat.remove
func TestACreativeBreakOfAChestBoatSpillsAllItsCargo(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Smasher", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_chest_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	diamond := servertest.MustItemID(t, "minecraft:diamond")
	for slot := range 27 {
		srv.StowBoatCargo(boatID, slot, protocol.ItemStack{ItemID: diamond, Count: 1})
	}
	servertest.AwaitFullSwing(t, srv, "Smasher")
	srv.StopTick()
	c.Attack(boatID)
	servertest.WaitFor(t, srv, 200, func() bool { return srv.BoatCount() == 0 })
	srv.TickBoats(1)

	items := srv.ItemsInDimension(world.DimensionName)
	if servertest.BlastLeftItem(items, "minecraft:oak_chest_boat") {
		t.Error("the creative break left the boat item behind")
	}
	if got := servertest.ItemCountOf(items, "minecraft:diamond"); got != 27 {
		t.Errorf("the creative break spilled %d diamonds, want all 27 slots", got)
	}
}

// TestABrokenBoatDropsItsItemWithAnItemsToss: the boat's item leaves the
// wreck with the bare ItemEntity constructor's toss, up 0.2 and up to 0.1 to
// either side, not straight up.
// vanilla: Entity.spawnAtLocation, ItemEntity.<init>
func TestABrokenBoatDropsItsItemWithAnItemsToss(t *testing.T) {
	t.Parallel()
	srv, _, connect := servertest.BoatPlayer(t, "survival", "Breaker", 0)
	watcher := connect().Join("Watcher")
	watcher.Settle()
	srv.StopTick()
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	srv.DamageBoat(boatID, 1000)
	srv.TickBoats(1)
	expectItemToss(t, servertest.AwaitBoatSpawn(watcher, protocol.EntityTypeItem))
}

// TestABreakWithEntityDropsOffStillSpillsAChestBoatsCargo: entity_drops
// gates the boat item's drop alone; the cargo spills for the removal that
// destroys the hull, rule or not.
// vanilla: VehicleEntity.destroy, AbstractChestBoat.remove
func TestABreakWithEntityDropsOffStillSpillsAChestBoatsCargo(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.BoatPlayer(t, "survival", "Breaker", 0)
	srv.StopTick()
	srv.SetGameRule("entity_drops", false)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_chest_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	diamond := servertest.MustItemID(t, "minecraft:diamond")
	for slot := range 27 {
		srv.StowBoatCargo(boatID, slot, protocol.ItemStack{ItemID: diamond, Count: 1})
	}
	srv.DamageBoat(boatID, 1000)
	srv.TickBoats(1)

	items := srv.ItemsInDimension(world.DimensionName)
	if servertest.BlastLeftItem(items, "minecraft:oak_chest_boat") {
		t.Error("a break with entity_drops off laid the boat item")
	}
	if got := servertest.ItemCountOf(items, "minecraft:diamond"); got != 27 {
		t.Errorf("a break with entity_drops off spilled %d diamonds, want all 27 slots", got)
	}
}

// TestABreakOfABambooChestRaftSpillsItsCargo: the chest raft is an
// AbstractChestBoat, so the removal that destroys the hull spills its 27
// slots the same as a chest boat's.
// vanilla: VehicleEntity.destroy, AbstractChestBoat.remove
func TestABreakOfABambooChestRaftSpillsItsCargo(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.BoatPlayer(t, "survival", "Breaker", 0)
	srv.StopTick()
	srv.SetGameRule("entity_drops", false)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:bamboo_chest_raft")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	diamond := servertest.MustItemID(t, "minecraft:diamond")
	for slot := range 27 {
		srv.StowBoatCargo(boatID, slot, protocol.ItemStack{ItemID: diamond, Count: 1})
	}
	srv.DamageBoat(boatID, 1000)
	srv.TickBoats(1)

	items := srv.ItemsInDimension(world.DimensionName)
	if servertest.BlastLeftItem(items, "minecraft:bamboo_chest_raft") {
		t.Error("a break with entity_drops off laid the raft item")
	}
	if got := servertest.ItemCountOf(items, "minecraft:diamond"); got != 27 {
		t.Errorf("the broken chest raft spilled %d diamonds, want all 27 slots", got)
	}
}

// TestABrokenChestBoatSpillsItsCargoAsSplitStacks: a chest boat's removal
// spills its cargo the way a broken container's does — each slot laid in the
// block the hull stood in, split into pieces of ten to thirty — and the
// spilled items carry no pickup delay.
// vanilla: AbstractChestBoat.remove, Containers.dropContents, dropItemStack
func TestABrokenChestBoatSpillsItsCargoAsSplitStacks(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.BoatPlayer(t, "survival", "Reaper", 0)
	srv.StopTick()
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_chest_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	srv.StowBoatCargo(boatID, 0, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cobblestone"), Count: 64})
	srv.RunLine("kill @e[type=minecraft:oak_chest_boat]")
	srv.TickBoats(1)

	items := srv.ItemsInDimension(world.DimensionName)
	var spilled []server.TestItem
	var total int32
	for _, it := range items {
		if it.Item != "minecraft:cobblestone" {
			continue
		}
		spilled = append(spilled, it)
		total += it.Count
		if it.Count < 1 || it.Count > 30 {
			t.Errorf("a spilled stack holds %d cobblestone, want a piece of the spill's 10 to 30", it.Count)
		}
		if it.PickupDelay != 0 {
			t.Errorf("a spilled stack carries pickup delay %d, want none", it.PickupDelay)
		}
		if math.Floor(it.X) != 4 || math.Floor(it.Y) != 64 || math.Floor(it.Z) != 4 {
			t.Errorf("a spilled stack lies at (%v %v %v), want the block (4 64 4) the hull stood in", it.X, it.Y, it.Z)
		}
	}
	// Sixty-four split into ten-to-thirty pieces takes at least three and at
	// most seven; the seventh needs the first six to leave four or fewer.
	if len(spilled) < 3 || len(spilled) > 7 {
		t.Errorf("the cargo spilled as %d stacks, want 3 to 7", len(spilled))
	}
	if total != 64 {
		t.Errorf("the spill holds %d cobblestone, want all 64", total)
	}
}

// TestAChestBoatsSpilledCargoIsPickableAtOnce: the spill a broken chest boat
// leaves is not held back the ten ticks a dropped item is; a player standing
// in the block the hull broke in claims it on the tick after it appears.
// vanilla: Containers.dropItemStack (a fresh ItemEntity, pickup delay 0)
func TestAChestBoatsSpilledCargoIsPickableAtOnce(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "survival", "Collector", 0)
	srv.StopTick()
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_chest_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	srv.StowBoatCargo(boatID, 0, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cobblestone"), Count: 1})
	srv.DamageBoat(boatID, 1000)
	srv.TickBoats(1)

	// Step into the block the hull stood in and let one tick run: a spill
	// with the container's own delay is claimed that tick, where the boat
	// item's ten-tick delay would still hold it.
	c.MoveStep(4.5, 64, 4.5, true)
	c.Settle()
	srv.TickEntities(1)

	if got := servertest.ItemCountOf(srv.ItemsInDimension(world.DimensionName), "minecraft:cobblestone"); got != 0 {
		t.Errorf("the spill still holds %d cobblestone a tick on, want the pickup to have taken it", got)
	}
	if got := srv.InventorySlot("Collector", player.HotbarSlot); got.ItemID != servertest.MustItemID(t, "minecraft:cobblestone") || got.Count != 1 {
		t.Errorf("the collector holds %+v in the hotbar, want the spilled cobblestone", got)
	}
}

// TestAMobStandingInABoatsSweepIsPushedAboard: the tick that runs an
// unridden boat sweeps the living bodies its hull touches into the seats,
// unless the body's kind is one the cannot_be_pushed_onto_boats tag keeps
// ashore — the wet eye of a llama, say, is nobody the hull takes.
func TestAMobStandingInABoatsSweepIsPushedAboard(t *testing.T) {
	t.Parallel()
	srv, _, _ := servertest.BoatPlayer(t, "creative", "Ferryman", 0)
	boatID := srv.SpawnBoat(2.5, 64, 2.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	cow := srv.SpawnMob("minecraft:cow", 2.5, 64, 2.5)
	if cow == 0 {
		t.Fatal("SpawnMob came to nothing")
	}
	servertest.WaitFor(t, srv, 200, func() bool {
		return len(srv.BoatPassengerIDs(boatID)) == 1
	})
	if got := srv.BoatPassengerIDs(boatID); len(got) != 1 || got[0] != cow {
		t.Errorf("the boat's passengers = %v, want the cow %d aboard alone", got, cow)
	}
	// A passenger's body rides where the hull holds it: the cow, alone
	// aboard, stands at the hull's centre, not walking anywhere.
	if x, y, z, ok := srv.MobPos(cow); !ok || servertest.Abs64(x-2.5) > 0.01 || servertest.Abs64(z-2.5) > 0.01 ||
		servertest.Abs64(y-(64+rules.BoatSeatY)) > 0.05 {
		t.Errorf("the cow rides at (%v %v %v) (%v), want the boat's seat", x, y, z, ok)
	}
}

// TestARowedBoatDoesNotPickUpAMobItTouches: a boat whose driver is a player
// leaves the pig under its hull alone.
// vanilla: AbstractBoat.tick
func TestARowedBoatDoesNotPickUpAMobItTouches(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Rower", 0)
	boatID := srv.SpawnBoat(2.5, 64, 2.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)
	pig := srv.SpawnMob("minecraft:pig", 2.5, 64, 2.5)
	if pig == 0 {
		t.Fatal("SpawnMob came to nothing")
	}
	servertest.RunTicks(t, srv, 30)
	for _, id := range srv.BoatPassengerIDs(boatID) {
		if id == pig {
			t.Fatalf("the rowed boat seated the pig %d", pig)
		}
	}
}

// TestABoatRefusesAMoveVehicleReportItCannotBelieve: the report of a
// single-tick move past the hundred blocks squared — and the report of a
// spot the hull's box cannot stand in — are refused, the boat put back
// with a Move Vehicle of the server's own and carried nobody along.
// vanilla: ServerGamePacketListenerImpl.handleMoveVehicle
func TestABoatRefusesAMoveVehicleReportItCannotBelieve(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Houdini", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)

	// The five-thousand-block jump.
	c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(5004.5)
		b.PutF64(64)
		b.PutF64(4.5)
		b.PutF32(0)
		b.PutF32(0)
		b.PutBool(false)
	})
	// The refusal puts the boat back with a Move Vehicle of where the
	// server keeps it.
	back := false
	c.ReadTicksSkipping(srv, 200, servertest.BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayMoveVehicle {
			return false
		}
		r := protocol.NewReader(data)
		x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
		yaw, pitch := r.ReadF32(), r.ReadF32()
		if err := r.Err(); err != nil {
			c.T.Fatalf("vehicle move: %v", err)
		}
		if x != 4.5 || y != 64 || z != 4.5 {
			c.T.Errorf("the refused boat was put back at (%v %v %v), want where it stood", x, y, z)
		}
		if yaw != 0 || pitch != 0 {
			c.T.Errorf("the refused boat was put back facing (%v %v), want its own look", yaw, pitch)
		}
		if r.Remaining() != 0 {
			c.T.Errorf("vehicle move has %d bytes left over, want the position and look alone", r.Remaining())
		}
		back = true
		return true
	})
	if !back {
		t.Error("the boat was never put back from its five-thousand-block jump")
	}
	if x, _, z, _, _, ok := srv.BoatState(boatID); !ok || x != 4.5 || z != 4.5 {
		t.Errorf("the boat stands at x=%v z=%v (%v), want it never moved", x, z, ok)
	}
	if rider := srv.PlayerPosition("Houdini"); rider.X != 4.5 || servertest.Abs64(rider.Z-4.5) > 0.001 {
		t.Errorf("the rider was carried to %+v, want them left in the seat at the centre", rider)
	}

	// The spot inside stone.
	srv.SetBlock(4, 80, 4, servertest.MustState(t, "minecraft:stone", nil))
	c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(4.5)
		b.PutF64(80)
		b.PutF64(4.5)
		b.PutF32(0)
		b.PutF32(0)
		b.PutBool(false)
	})
	refused := c.ReadTicksSkipping(srv, 200, servertest.BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayMoveVehicle {
			return false
		}
		r := protocol.NewReader(data)
		x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
		return x == 4.5 && y == 64 && z == 4.5
	})
	if !refused {
		t.Error("the boat was never put back from the stone")
	}
	if _, y, _, _, _, ok := srv.BoatState(boatID); !ok || servertest.Abs64(y-80) < 1 {
		t.Errorf("the boat was stood into the stone at y=%v (%v), want the report refused", y, ok)
	}
}

// TestABoatRefusesALongMoveHiddenByATurn: the report past the hundred blocks
// squared is refused however far the hull also turned. Subtracting the turn
// from the distance would let twenty blocks ride under a twenty-degree turn
// and slip the check.
// vanilla: ServerGamePacketListenerImpl.handleMoveVehicle
func TestABoatRefusesALongMoveHiddenByATurn(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Turner", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)

	// Twenty blocks out and twenty degrees about: the squares are equal, so
	// a check that took one off the other would pass it.
	c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(24.5)
		b.PutF64(64)
		b.PutF64(4.5)
		b.PutF32(20)
		b.PutF32(0)
		b.PutBool(false)
	})
	back := false
	c.ReadTicksSkipping(srv, 200, servertest.BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayMoveVehicle {
			return false
		}
		r := protocol.NewReader(data)
		x := r.ReadF64()
		r.ReadF64()
		r.ReadF64()
		r.ReadF32()
		r.ReadF32()
		if x != 4.5 {
			return false
		}
		back = true
		return true
	})
	if !back {
		t.Error("the boat's long move under a turn was never put back")
	}
	if x, _, _, _, _, ok := srv.BoatState(boatID); !ok || x != 4.5 {
		t.Errorf("the boat stands at x=%v (%v), want the report refused", x, ok)
	}
}

// TestABoatRidesItsChunkOutOfTheWorldAndBack: a boat whose chunk unloads
// leaves with it — saved into the chunk's entity file beside the mobs, not
// left standing in the void of an unloaded chunk — and a restarted server
// stands it back up in the world it left. A chest boat's cargo rides the
// file with it.
func TestABoatRidesItsChunkOutOfTheWorldAndBack(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Keeper")
	c.Settle()

	boatID := srv.SpawnBoat(14.5, 64, 14.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	// The plan waits until the watcher was told.
	spawned := servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)

	chestID := srv.SpawnBoat(-14.5, 64, 14.5, "minecraft:oak_chest_boat")
	if chestID == 0 {
		t.Fatal("SpawnBoat came to nothing for the chest boat")
	}
	servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakChestBoat)
	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the item registry has no diamond")
	}
	srv.StowBoatCargo(chestID, 3, protocol.ItemStack{ItemID: diamond, Count: 2})

	// Away: the player's leaving unloads the world's chunks, and the boats
	// are gone with them — swept into their chunk files, not left to fall
	// behind the world's bottom.
	c.Disconnect()
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.BoatCount() == 0
	})
	srv.FlushMobs()

	// On disk the compounds are the vanilla ones: the kind's name as the
	// id, the place the hull stands, and a chest boat's cargo under Items
	// the container codec reads.
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:oak_boat", func(compound nbt.Compound) {
		assertBoatSave(t, compound, spawned.Uuid, 14.5, 64, 14.5)
	})
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: -1, Z: 0}, "minecraft:oak_chest_boat", func(compound nbt.Compound) {
		items, ok := nbt.Get[nbt.List](compound, "Items")
		if !ok || len(items.Items) != 1 {
			t.Errorf("the saved chest boat holds %v items, want the one stowed", len(items.Items))
			return
		}
		entry, ok := items.Items[0].(nbt.Compound)
		if !ok {
			t.Error("the chest boat's cargo item is no compound")
			return
		}
		if id, _ := nbt.Get[nbt.String](entry, "id"); string(id) != "minecraft:diamond" {
			t.Errorf("the cargo item's id = %v, want minecraft:diamond", id)
		}
		if slot, _ := nbt.Get[nbt.Byte](entry, "Slot"); slot != 3 {
			t.Errorf("the cargo item's Slot = %v, want 3", slot)
		}
	})

	// Back: a restarted server stands both hulls up again, the cargo still
	// in the chest. The first server is stopped first: the restarted one
	// would otherwise be stopped by its session lock on the same folder.
	srv.Shutdown()
	restarted := *cfg
	againSrv, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	back := again.AwaitBoatsThroughJoin(map[int32]string{
		protocol.EntityTypeOakBoat:      "oak boat",
		protocol.EntityTypeOakChestBoat: "chest boat",
	})
	oakBack, chestBack := back["oak boat"], back["chest boat"]
	if oakBack.Uuid.String() != spawned.Uuid {
		t.Errorf("the restored boat's identity = %s, want the saved one %s", oakBack.Uuid, spawned.Uuid)
	}
	if got := againSrv.BoatCount(); got != 2 {
		t.Errorf("the restarted server holds %d boats, want the two that were saved", got)
	}
	if got := againSrv.BoatCargoAt(chestBack.EntityID, 3); got.ItemID != diamond || got.Count != 2 {
		t.Errorf("the restored chest boat's cargo slot 3 holds %+v, want the two diamonds", got)
	}
}

// TestABoatIsLeftStillWhenItsChunkUnloads covers the window between a tick's
// entity sweep and its boat pass: a hull whose chunk unloaded in between, not
// yet swept, has air read for its ground, and stepping it there drops it a
// tick's gravity before the next sweep writes the save. The loop is stopped
// so the sweep cannot race the pass driven by hand.
func TestABoatIsLeftStillWhenItsChunkUnloads(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Keeper")
	c.Settle()

	srv.StopTick()

	boatID := srv.SpawnBoat(14.5, 64, 14.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)

	// Away: the player's leaving releases the chunk, with the sweep held
	// back by the stopped loop, so the hull is still standing in the list.
	c.Disconnect()
	servertest.WaitFor(t, srv, 200, func() bool {
		return !srv.ChunkLoadedInDimension(world.DimensionName, 0, 0)
	})
	if n := srv.BoatCount(); n != 1 {
		t.Fatalf("boats standing = %d, want the one the sweep has not taken", n)
	}

	srv.TickBoats(1)
	if _, y, _, _, _, ok := srv.BoatState(boatID); !ok || y != 64 {
		t.Errorf("the boat's y = %v (%v), want 64: a hull whose chunk unloaded must not be ticked", y, ok)
	}
}

// TestANoGravityBoatSurvivesASaveAndReload plants a boat carrying the tag in
// a chunk's entity file — the state a vanilla world saves it in — and reads
// it back: the reload keeps it in the air under the hull's own ticks, and the
// flag survives the save that writes it out again.
func TestANoGravityBoatSurvivesASaveAndReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, nbt.Compound{
		"id":   nbt.String("minecraft:oak_boat"),
		"UUID": servertest.UuidWords(servertest.FixtureUUID(4, 2, 0, 10)),
		"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
			nbt.Double(5.5), nbt.Double(70), nbt.Double(5.5),
		}},
		"Motion":    nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(0), nbt.Double(0), nbt.Double(0)}},
		"NoGravity": nbt.Byte(1),
	})

	srv, _ := servertest.Join(t, cfg, "Keeper")

	servertest.WaitFor(t, srv, 200, func() bool { return srv.BoatCount() == 1 })
	srv.StopTick()

	ids := srv.BoatIDs()
	if len(ids) != 1 {
		t.Fatalf("boats standing = %d, want the one planted", len(ids))
	}
	before := soleBoatY(t, srv, ids[0])
	srv.TickBoats(40)
	if after := soleBoatY(t, srv, ids[0]); math.Abs(after-before) > 0.01 {
		t.Errorf("the reloaded no-gravity boat fell from y=%v to y=%v over forty ticks, want it hanging", before, after)
	}

	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:oak_boat", func(compound nbt.Compound) {
		flag, ok := nbt.Get[nbt.Byte](compound, "NoGravity")
		if !ok || flag != 1 {
			t.Errorf("the reloaded boat saved NoGravity = %v (present %v), want 1", flag, ok)
		}
	})
}

// soleBoatY is the height the boat with the given entity ID stands at,
// failing the test when no such live boat is in the world.
func soleBoatY(t *testing.T, srv *server.Server, id int32) float64 {
	t.Helper()
	_, y, _, _, _, ok := srv.BoatState(id)
	if !ok {
		t.Fatal("the boat under test is not in the world")
	}
	return y
}

// assertBoatSave checks the common half of a saved boat's compound: the
// identity the file keeps, and where the hull stands.
func assertBoatSave(t *testing.T, compound nbt.Compound, identity string, wantX, wantY, wantZ float64) {
	t.Helper()
	pos, ok := nbt.Get[nbt.List](compound, "Pos")
	if !ok || len(pos.Items) != 3 {
		t.Errorf("the saved boat's Pos = %v (%v), want three doubles", pos, ok)
		return
	}
	for i, want := range []float64{wantX, wantY, wantZ} {
		if got, ok := pos.Items[i].(nbt.Double); !ok || servertest.Abs64(float64(got)-want) > 0.01 {
			t.Errorf("the saved boat's Pos[%d] = %v, want %v", i, pos.Items[i], want)
		}
	}
	saved, ok := nbt.Get[nbt.IntArray](compound, "UUID")
	if !ok || len(saved) != 4 {
		t.Errorf("the saved boat's UUID = %v (%v), want four integers", saved, ok)
	}
}

// TestABoatLeftRidingCarriesTheRiderBackIn: a rider who leaves riding takes
// their hull with them, saved only in their file, so the rejoin stands one
// copy back up and seats them on it, at the seat the boat holds.
func TestABoatLeftRidingCarriesTheRiderBackIn(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	c := connect().Join("Ferry")
	c.Settle()

	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)
	if rider, ok := srv.BoatRider(boatID); !ok || rider != "Ferry" {
		t.Fatalf("the boat's rider = %q (%v), want Ferry aboard to leave", rider, ok)
	}

	// Away: the hull goes with the rider, saved into their file alone.
	c.Disconnect()
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.BoatCount() == 0
	})

	// Back: the rejoin stands the file's copy back up and seats the rider
	// on it. The boat's spawn can go out before the join's last chunk batch,
	// which Join reads past, so the join's packets are kept for the read.
	c = connect()
	c.Login("Ferry")
	c.Configure()
	var joined []servertest.PushedPacket
	c.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlayLogin && c.PlayerEntityID == 0 {
			c.PlayerEntityID = protocol.NewReader(data).ReadI32()
		}
		joined = append(joined, servertest.PushedPacket{Id: id, Data: append([]byte(nil), data...)})
	})
	c.MarkLoaded()
	c.Unread(joined...)
	rejoined := servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)
	if riders := servertest.AwaitPassengersFor(c, rejoined.EntityID); len(riders) != 1 || riders[0] != c.PlayerEntityID {
		t.Errorf("passengers after the rejoin = %v, want the rejoining rider %d", riders, c.PlayerEntityID)
	}
	if rider, ok := srv.BoatRider(rejoined.EntityID); !ok || rider != "Ferry" {
		t.Errorf("the rejoined boat's rider = %q (%v), want Ferry back aboard", rider, ok)
	}
	if got := srv.PlayerPosition("Ferry"); servertest.Abs64(got.Y-(64+rules.BoatSeatY-0.6)) > 0.001 {
		t.Errorf("the rejoining rider sits at y=%v, want the seat %v", got.Y, 64+rules.BoatSeatY-0.6)
	}
}

// TestALeavingRiderTakesTheirHullAlong: the hull a rider leaves as its only
// player passenger goes out of the world with them at once — it lives on in
// their file alone — so a watcher still keeping the chunk loaded does not
// keep the empty boat standing.
// vanilla: PlayerList.remove
func TestALeavingRiderTakesTheirHullAlong(t *testing.T) {
	t.Parallel()
	srv, c, connect := servertest.BoatPlayer(t, "survival", "Solo", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)

	// A watcher in the same chunk keeps it loaded: without the leave taking
	// the hull, it would stay standing here.
	watcher := connect().Join("Watcher")
	watcher.Settle()

	c.Disconnect()
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.BoatCount() == 0
	})
	if got := srv.BoatCount(); got != 0 {
		t.Errorf("boats still standing = %d, want the rider's hull gone with them", got)
	}
}

// TestALeavingRiderTakesAChestBoatsCargoWithThem: the hull a rider leaves
// as its only player passenger carries its cargo off in the rider's file
// alone, so none of it spills beside where the hull stood.
func TestALeavingRiderTakesAChestBoatsCargoWithThem(t *testing.T) {
	t.Parallel()
	srv, c, connect := servertest.BoatPlayer(t, "survival", "Ferry", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_chest_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	srv.StowBoatCargo(boatID, 0, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond"), Count: 3})
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)

	// A watcher keeps the chunk loaded and the items listed, so a spill
	// beside the hull would still show up after the rider is gone.
	watcher := connect().Join("Watcher")
	watcher.Settle()

	c.Disconnect()
	// The leave only marks the hull dead; the tick is what serves its
	// break, so spill or carry-off is settled before the items are read.
	srv.TickBoats(1)
	servertest.WaitFor(t, srv, 200, func() bool { return srv.BoatCount() == 0 })
	if got := servertest.ItemCountOf(srv.ItemsInDimension(world.DimensionName), "minecraft:diamond"); got != 0 {
		t.Errorf("the leaving rider's chest boat spilled %d diamonds, want the cargo carried off", got)
	}
}

// TestALeavingRiderTakesTheirMobPassengerAlong: the hull a rider leaves as
// its only player passenger goes with the whole tree — the mob riding
// beside them is discarded too.
// vanilla: PlayerList.remove
func TestALeavingRiderTakesTheirMobPassengerAlong(t *testing.T) {
	t.Parallel()
	srv, c, connect := servertest.BoatPlayer(t, "creative", "Ferryman", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	// The cow boards first: a boat whose first passenger is a player never
	// seats a mob by touch.
	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 4.5)
	if cow == 0 {
		t.Fatal("SpawnMob came to nothing")
	}
	servertest.WaitFor(t, srv, 200, func() bool {
		return len(srv.BoatPassengerIDs(boatID)) == 1
	})
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)
	servertest.WaitFor(t, srv, 200, func() bool {
		return len(srv.BoatPassengerIDs(boatID)) == 2
	})

	// A watcher in the same chunk keeps it loaded, so the cow's end is the
	// leave's to bring about and not the chunk unload's.
	watcher := connect().Join("Watcher")
	watcher.Settle()

	// Away: the hull goes with the rider, and the cow with it.
	c.Disconnect()
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.BoatCount() == 0
	})
	servertest.WaitFor(t, srv, 200, func() bool {
		_, _, _, ok := srv.MobPos(cow)
		return !ok
	})
}

// TestAFlyingPlayerLaysABoatToo: a flyer's hand is as good as a walker's —
// vanilla's use asks nothing of the player's pose, so a creative flyer laying a boat over the water below them stands it on
// the surface like anyone else.
func TestAFlyingPlayerLaysABoatToo(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Aviator", 1)
	srv.SetBlock(0, 63, 0, servertest.MustState(t, "minecraft:water", nil))
	srv.SetBlock(1, 63, 0, servertest.MustState(t, "minecraft:water", nil))
	srv.SetBlock(0, 63, 1, servertest.MustState(t, "minecraft:water", nil))
	srv.SetBlock(1, 63, 1, servertest.MustState(t, "minecraft:water", nil))
	// A hover a fathom over the pool, still within the laying's reach.
	c.MoveToGrounded(1.0, 67, 1.0, 0, 90, false)
	c.Settle()

	// The abilities the double-jump set, then the boat laid from the air.
	c.Send(protocol.IDPlayPlayerAbilitiesIn, func(b *protocol.Buffer) {
		b.PutI8(protocol.AbilityFlying)
	})
	c.UseBoatItem(0, 90)
	spawned := servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)
	if servertest.Abs64(spawned.X-1.0) > 0.001 || servertest.Abs64(spawned.Z-1.0) > 0.001 {
		t.Errorf("the flyer's boat stands at (%v %v), want the pool's column (1.0, 1.0)", spawned.X, spawned.Z)
	}
	if got := srv.BoatCount(); got != 1 {
		t.Errorf("boats standing = %d, want the one the flyer laid", got)
	}
}

// TestANamedBoatItemLaysANamedBoat: a boat laid from a renamed item takes
// the item's custom name.
// vanilla: BoatItem.use, Entity.applyImplicitComponents
func TestANamedBoatItemLaysANamedBoat(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "survival", "Namer", 0)
	water := servertest.MustState(t, "minecraft:water", nil)
	// The water within the survival reach of the harness's stand, as in
	// TestABoatIsLaidOnWaterWithinTheLayingReach.
	for x := int32(2); x <= 3; x++ {
		for z := int32(0); z <= 1; z++ {
			srv.SetBlock(x, 63, z, water)
		}
	}
	srv.GiveSlot("Namer", player.HotbarSlot, protocol.ItemStack{
		ItemID:     servertest.MustItemID(t, "minecraft:oak_boat"),
		Count:      1,
		CustomName: &protocol.Text{Text: "Ferry"},
	})

	aim := boatAim(0.5, 0.5, 64.0+rules.PlayerEyeHeight(protocol.PoseStanding), 2.9, 63.0+8.0/9, 1.0)
	c.UseBoatItem(aim.yaw, aim.pitch)
	servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)
	tag, ok := srv.BoatSaveTag(srv.BoatIDs()[0])
	if !ok {
		t.Fatal("the boat has no save tag")
	}
	if got := tag["CustomName"]; got != nbt.String("Ferry") {
		t.Errorf("CustomName = %#v, want \"Ferry\"", got)
	}
}

// TestABoatIsLaidOnWaterWithinTheLayingReach: the laying's reach is the
// block interaction range read out of the eye — four and a half blocks in
// survival — so a look whose water stands past that keeps the item.
// vanilla: BoatItem.use
func TestABoatIsLaidOnWaterWithinTheLayingReach(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "survival", "Reachy", 2)
	water := servertest.MustState(t, "minecraft:water", nil)
	// One strip of water down the look's line, x from two to five: the near
	// look crosses its surface about three blocks out, within the survival
	// reach, and the far look reaches its far end past it.
	for x := int32(2); x <= 5; x++ {
		for z := int32(0); z <= 1; z++ {
			srv.SetBlock(x, 63, z, water)
		}
	}
	// The harness stands Reachy at (0.5, 64, 0.5); the eye a body of that
	// stand carries a block and three fifths above its feet.
	eyeY := 64.0 + rules.PlayerEyeHeight(protocol.PoseStanding)

	// The near end: some three blocks of ray inside the reach, lays.
	near := boatAim(0.5, 0.5, eyeY, 2.9, 63.0+8.0/9, 1.0)
	c.UseBoatItem(near.yaw, near.pitch)
	spawned := servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)
	if servertest.Abs64(spawned.X-2.9) > 0.05 {
		t.Errorf("the near pond's boat stands at x=%v, want the looked column 2.9", spawned.X)
	}
	if servertest.Abs64(spawned.Y-(63.0+8.0/9)) > 0.02 {
		t.Errorf("the near pond's boat stands at y=%v, want the surface %v", spawned.Y, 63.0+8.0/9)
	}
	if got := srv.InventorySlot("Reachy", player.HotbarSlot); got.Count != 1 {
		t.Errorf("boats left = %d, want 1: the near pond spent the first", got.Count)
	}

	// The far end: the ray crosses its surface past four and a half blocks,
	// keeps the item.
	far := boatAim(0.5, 0.5, eyeY, 4.9, 63.0+8.0/9, 1.0)
	servertest.HoldTicks(t, srv)
	c.UseBoatItem(far.yaw, far.pitch)
	refused := c.PumpTicks(srv, 100, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		return r.ReadVarInt() == protocol.EntityTypeOakBoat
	})
	if refused {
		t.Error("a boat was laid on water past the survival reach")
	}
	if got := srv.InventorySlot("Reachy", player.HotbarSlot); got.Count != 1 {
		t.Errorf("boats left = %d, want 1: the far look spent nothing", got.Count)
	}
}

// TestALayingInCreativeReachesHalfABlockFurther: the creative hand lays a
// boat on water a half-block past the survival's reach — the five blocks of
// the creative block interaction range read out of the same eye.
func TestALayingInCreativeReachesHalfABlockFurther(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Composer", 1)
	water := servertest.MustState(t, "minecraft:water", nil)
	for x := int32(4); x <= 5; x++ {
		for z := int32(0); z <= 1; z++ {
			srv.SetBlock(x, 63, z, water)
		}
	}
	eyeY := 64.0 + rules.PlayerEyeHeight(protocol.PoseStanding)

	far := boatAim(0.5, 0.5, eyeY, 4.9, 63.0+8.0/9, 1.0)
	c.UseBoatItem(far.yaw, far.pitch)
	spawned := servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)
	if servertest.Abs64(spawned.X-4.9) > 0.05 {
		t.Errorf("the far look's boat stands at x=%v, want the looked column 4.9", spawned.X)
	}
	if got := srv.InventorySlot("Composer", player.HotbarSlot); got.Count != 1 {
		t.Errorf("boats left = %d, want 1: the creative hand spent nothing", got.Count)
	}
}

// TestABoatIsNotLaidThroughTheFlyer: a boat item whose box would cross the
// laying player's own body lays nothing — a flyer hovering so low the hull
// would overlap them keeps the item — the use's noCollision refusing what
// the client's own check already turned down.
// vanilla: BoatItem.use's noCollision
func TestABoatIsNotLaidThroughTheFlyer(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Hoverer", 1)
	srv.SetBlock(0, 63, 0, servertest.MustState(t, "minecraft:water", nil))
	srv.SetBlock(1, 63, 0, servertest.MustState(t, "minecraft:water", nil))
	srv.SetBlock(0, 63, 1, servertest.MustState(t, "minecraft:water", nil))
	srv.SetBlock(1, 63, 1, servertest.MustState(t, "minecraft:water", nil))
	// Feet at the pool's surface, so the hull the look lays would cross the
	// hovering body.
	c.MoveToGrounded(1.0, 64, 1.0, 0, 90, false)
	c.Settle()
	c.Send(protocol.IDPlayPlayerAbilitiesIn, func(b *protocol.Buffer) {
		b.PutI8(protocol.AbilityFlying)
	})

	servertest.HoldTicks(t, srv)
	c.UseBoatItem(0, 90)
	laid := c.PumpTicks(srv, 100, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		_, typ := servertest.SpawnEntityHead(data)
		return typ == protocol.EntityTypeOakBoat
	})
	if laid {
		t.Error("a boat was laid through the flying player's own body")
	}
	if got := srv.InventorySlot("Hoverer", player.HotbarSlot); got.Count != 1 {
		t.Errorf("boats left = %d, want 1: the refused laying is not spent", got.Count)
	}
}

// TestABoatIsLaidThroughAWalkerOnAClimbable: a body on a ladder is not
// pushable, so the walker standing in the ladder's cell does not refuse the
// boat the look lays over them.
// vanilla: AbstractBoat.canVehicleCollide, LivingEntity.isPushable
func TestABoatIsLaidThroughAWalkerOnAClimbable(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Climber", 1)
	// A ladder in the cell the walker's feet take: the hull's spot stops on
	// its top, and the body the box would cross is on the climbable.
	srv.SetBlock(1, 64, 1, servertest.LadderState(t, "south"))
	c.MoveToGrounded(1.0, 64, 1.0, 0, 90, false)
	c.Settle()

	c.UseBoatItem(0, 90)
	laid := c.ReadTicksSkipping(srv, 100, servertest.BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		_, typ := servertest.SpawnEntityHead(data)
		return typ == protocol.EntityTypeOakBoat
	})
	if !laid {
		t.Error("a boat was refused by a body on a climbable")
	}
}

// TestABoatIsNotLaidThroughAFlyerOnAClimbable: a flying player's onClimbable
// is off, so the flyer hovering in the ladder's cell is pushable and refuses
// the boat the walker in the same cell would let through.
// vanilla: Player.onClimbable, AbstractBoat.canVehicleCollide
func TestABoatIsNotLaidThroughAFlyerOnAClimbable(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Hoverer", 1)
	srv.SetBlock(1, 64, 1, servertest.LadderState(t, "south"))
	c.MoveToGrounded(1.0, 64, 1.0, 0, 90, false)
	c.Settle()
	c.Send(protocol.IDPlayPlayerAbilitiesIn, func(b *protocol.Buffer) {
		b.PutI8(protocol.AbilityFlying)
	})

	servertest.HoldTicks(t, srv)
	c.UseBoatItem(0, 90)
	laid := c.PumpTicks(srv, 100, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		_, typ := servertest.SpawnEntityHead(data)
		return typ == protocol.EntityTypeOakBoat
	})
	if laid {
		t.Error("a boat was laid through the flying player's body on a climbable")
	}
	if got := srv.InventorySlot("Hoverer", player.HotbarSlot); got.Count != 1 {
		t.Errorf("boats left = %d, want 1: the refused laying is not spent", got.Count)
	}
}

// TestABoatIsLaidThroughAMobOnAClimbable: a mob standing in a ladder's cell
// is not pushable either, so the boat is laid through it where the same mob
// on open ground refuses.
// vanilla: AbstractBoat.canVehicleCollide, LivingEntity.isPushable
func TestABoatIsLaidThroughAMobOnAClimbable(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Ladderer", 1)
	// A ladder in the cell the cow's feet take, and the layer a block above
	// so its own body stays clear of the hull.
	srv.SetBlock(1, 64, 1, servertest.LadderState(t, "south"))
	cow := srv.SpawnMob("minecraft:cow", 1.0, 64, 1.0)
	if cow == 0 {
		t.Fatal("SpawnMob came to nothing")
	}
	srv.SetMobNoAI(cow)

	c.MoveToGrounded(1.0, 66, 1.0, 0, 90, false)
	c.Settle()
	c.Send(protocol.IDPlayPlayerAbilitiesIn, func(b *protocol.Buffer) {
		b.PutI8(protocol.AbilityFlying)
	})

	c.UseBoatItem(0, 90)
	laid := c.ReadTicksSkipping(srv, 100, servertest.BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		_, typ := servertest.SpawnEntityHead(data)
		return typ == protocol.EntityTypeOakBoat
	})
	if !laid {
		t.Error("a boat was refused by a mob on a climbable")
	}
}

// TestABoatIsNotLaidThroughASpiderOnAClimbable: a spider's onClimbable is
// only its Climbing flag, so one standing on a ladder it is not climbing is
// pushable and refuses the boat, where a cow in the same cell lets it pass.
// vanilla: Spider.isClimbing, AbstractBoat.canVehicleCollide
func TestABoatIsNotLaidThroughASpiderOnAClimbable(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Weaver", 1)
	// A ladder in the cell the spider's feet take; the layer beside it looks
	// down the next column onto the ground, so the hull's box crosses the
	// spider without crossing the layer's own body.
	srv.SetBlock(2, 64, 1, servertest.LadderState(t, "south"))
	spider := srv.SpawnMob("minecraft:spider", 2.0, 64, 1.0)
	if spider == 0 {
		t.Fatal("SpawnMob came to nothing")
	}
	srv.SetMobNoAI(spider)

	c.MoveToGrounded(1.0, 65, 1.0, 0, 90, false)
	c.Settle()
	c.Send(protocol.IDPlayPlayerAbilitiesIn, func(b *protocol.Buffer) {
		b.PutI8(protocol.AbilityFlying)
	})

	servertest.HoldTicks(t, srv)
	c.UseBoatItem(0, 90)
	laid := c.PumpTicks(srv, 100, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		_, typ := servertest.SpawnEntityHead(data)
		return typ == protocol.EntityTypeOakBoat
	})
	if laid {
		t.Error("a boat was laid through a spider on a climbable it is not climbing")
	}
	if got := srv.InventorySlot("Weaver", player.HotbarSlot); got.Count != 1 {
		t.Errorf("boats left = %d, want 1: the refused laying is not spent", got.Count)
	}
}

// TestABoatIsRefusedFlushAgainstABody: the hull's box is tested with the
// entity query's 1e-7 inflation, so a body whose feet sit exactly on the
// hull's top — flush, not overlapping — still refuses the laying.
// vanilla: EntityGetter.getEntityCollisions
func TestABoatIsRefusedFlushAgainstABody(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Flush", 1)
	// The look stops on the ladder's top at y=65, so the hull's top is
	// exactly 65 + BoatHeight; the flyer's feet are set there, flush and a
	// cell above the ladder, so the body is still pushable.
	srv.SetBlock(1, 64, 1, servertest.LadderState(t, "south"))
	c.MoveToGrounded(1.0, 65+rules.BoatHeight, 1.0, 0, 90, false)
	c.Settle()
	c.Send(protocol.IDPlayPlayerAbilitiesIn, func(b *protocol.Buffer) {
		b.PutI8(protocol.AbilityFlying)
	})

	servertest.HoldTicks(t, srv)
	c.UseBoatItem(0, 90)
	laid := c.PumpTicks(srv, 100, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		_, typ := servertest.SpawnEntityHead(data)
		return typ == protocol.EntityTypeOakBoat
	})
	if laid {
		t.Error("a boat was laid flush against a body")
	}
}

// TestABoatIsNotLaidOntoAnotherHull: a boat laid on water another hull
// already floats on — the boxes crossing — lays nothing, the second use's
// noCollision refusing what the client's own check already turned down.
// vanilla: BoatItem.use's noCollision, AbstractBoat.canCollideWith
func TestABoatIsNotLaidOntoAnotherHull(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Crowder", 1)
	srv.SetBlock(0, 63, 0, servertest.MustState(t, "minecraft:water", nil))
	srv.SetBlock(1, 63, 0, servertest.MustState(t, "minecraft:water", nil))
	srv.SetBlock(0, 63, 1, servertest.MustState(t, "minecraft:water", nil))
	srv.SetBlock(1, 63, 1, servertest.MustState(t, "minecraft:water", nil))
	// A hull already afloat where the look falls, and the eye a block above
	// so the layer's own body stays clear of the box.
	if srv.SpawnBoat(1.0, 63.0+8.0/9.0, 1.0, "minecraft:oak_boat") == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.MoveToGrounded(1.0, 65, 1.0, 0, 90, false)
	c.Settle()
	c.Send(protocol.IDPlayPlayerAbilitiesIn, func(b *protocol.Buffer) {
		b.PutI8(protocol.AbilityFlying)
	})

	servertest.HoldTicks(t, srv)
	c.UseBoatItem(0, 90)
	laid := c.PumpTicks(srv, 100, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		return r.ReadVarInt() == protocol.EntityTypeOakBoat
	})
	if laid {
		t.Error("a boat was laid onto another hull's box")
	}
	if got := srv.InventorySlot("Crowder", player.HotbarSlot); got.Count != 1 {
		t.Errorf("boats left = %d, want 1: the refused laying is not spent", got.Count)
	}
}

// TestABoatIsLaidOnARunningFlowAtItsOwnSurface: the water a look falls on
// can be a running flow, and the laying stands the hull on the surface the
// flow's own level fills to — six ninths of the cell for the second level —
// not the eight a source keeps.
// vanilla: FluidState.getShape, BoatItem.use
func TestABoatIsLaidOnARunningFlowAtItsOwnSurface(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "survival", "Wader", 1)
	flowing := servertest.MustState(t, "minecraft:water", map[string]string{"level": "2"})
	for x := int32(1); x <= 2; x++ {
		for z := int32(0); z <= 1; z++ {
			srv.SetBlock(x, 63, z, flowing)
		}
	}
	eyeY := 64.0 + rules.PlayerEyeHeight(protocol.PoseStanding)

	surface := 63.0 + 6.0/9
	aim := boatAim(0.5, 0.5, eyeY, 1.9, surface, 1.0)
	c.UseBoatItem(aim.yaw, aim.pitch)
	spawned := servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)
	if servertest.Abs64(spawned.X-1.9) > 0.05 {
		t.Errorf("the flowing pond's boat stands at x=%v, want the looked column 1.9", spawned.X)
	}
	if servertest.Abs64(spawned.Y-surface) > 0.02 {
		t.Errorf("the flowing pond's boat stands at y=%v, want the level's surface %v", spawned.Y, surface)
	}
}

// TestANonfiniteMoveVehicleReportEndsTheConnection: a driving report that
// is not real numbers is the one the vehicle's handler does not drop
// quietly — vanilla's isInvalidVehiclePosition ends the connection, with
// the disconnect reason that names it.
// vanilla: ServerGamePacketListenerImpl.handleMoveVehicle
func TestANonfiniteMoveVehicleReportEndsTheConnection(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Hijacker", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)

	c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(math.NaN())
		b.PutF64(64)
		b.PutF64(4.5)
		b.PutF32(0)
		b.PutF32(0)
		b.PutBool(false)
	})
	// The kick closes the connection, so no tick barrier can follow it.
	servertest.ExpectDisconnect(t, c, protocol.Text{Translate: "multiplayer.disconnect.invalid_vehicle_movement"})
}

// TestAVehicleMovePastTheWorldEdgeIsNotKicked: a finite Move Vehicle at a
// position the world cannot hold is refused by the boat's own check and put
// back, not ended by the connection: only a NaN position or a non-finite turn
// kicks. vanilla: ServerGamePacketListenerImpl.handleMoveVehicle
func TestAVehicleMovePastTheWorldEdgeIsNotKicked(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Voyager", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)

	// Far past the world edge, but every number is real.
	c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(4e7)
		b.PutF64(64)
		b.PutF64(4.5)
		b.PutF32(0)
		b.PutF32(0)
		b.PutBool(false)
	})
	back := false
	c.ReadTicksSkipping(srv, 200, servertest.BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayMoveVehicle {
			return false
		}
		back = protocol.NewReader(data).ReadF64() == 4.5
		return true
	})
	if !back {
		t.Error("the boat was not put back from the world-edge report")
	}
	// The connection is still up: a ping is answered.
	c.Settle()
}

// boatAim is the look that aims the eye at (fromX, eyeY, fromZ) at the
// point given: the yaw and pitch the Use Item packet carries, the degrees
// the look vector reads them in.
func boatAim(fromX, fromZ, eyeY, toX, toY, toZ float64) (look struct {
	yaw, pitch float32
}) {
	d := math.Hypot(toX-fromX, toZ-fromZ)
	look.yaw = float32(math.Atan2(-(toX-fromX), toZ-fromZ) * 180 / math.Pi)
	look.pitch = float32(math.Atan2(eyeY-toY, d) * 180 / math.Pi)
	return look
}

// TestASneakingUseOfABoatDoesNotRaceTheStep: a boat's step moves it on the
// tick goroutine under the boat lock, and a use's reach check reads its box.
// The two overlap for as long as the click takes, so the loop hammers one
// sneaking click — which reads the box and seats nobody, drawing no reply the
// read would have to keep up with — while the boat ticks; the race detector is
// the assertion, the empty seats the plain one.
func TestASneakingUseOfABoatDoesNotRaceTheStep(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Clicker", 0)
	c.MoveToGround(8.5, 64, 8.5, true)
	c.Settle()
	boatID := srv.SpawnBoat(8.5, 64, 8.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.Settle()
	for range 50000 {
		c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
			b.PutVarInt(boatID)
			b.PutVarInt(protocol.HandMain)
			b.PutLpVec3(0.1, 0.9, 0.2)
			b.PutBool(true) // the sneak key, which seats nobody
		})
	}
	c.Settle()
	if riders := srv.BoatPassengerIDs(boatID); len(riders) != 0 {
		t.Errorf("the sneaking use boarded %v, want nobody", riders)
	}
}

// TestAnOutOfReachAttackOnABoatDoesNotRaceTheStep: a boat's step moves it on
// the tick goroutine under the boat lock, and a blow reads its box to decide
// the reach before it does anything else. A swing from beyond the reach reads
// the box and returns without touching the boat, so the loop hammers one while
// the boat ticks; the race detector is the assertion, the boat still afloat
// behind it.
func TestAnOutOfReachAttackOnABoatDoesNotRaceTheStep(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Swinger", 0)
	boatID := srv.SpawnBoat(8.5, 64, 8.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.Settle()
	for range 50000 {
		c.Attack(boatID)
	}
	c.Settle()
	if got := srv.BoatCount(); got != 1 {
		t.Errorf("the out-of-reach blows left %d boats, want the one standing", got)
	}
}

// TestADraggingColumnShakesAMobOffAnUnriddenBoat: the downward throw ejects
// the passengers, a mob among them.
// vanilla: AbstractBoat.tickBubbleColumn
func TestADraggingColumnShakesAMobOffAnUnriddenBoat(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Diver", 0)
	servertest.HoldTicks(t, srv)
	magma := servertest.MustState(t, "minecraft:magma_block", nil)
	drag := servertest.MustState(t, "minecraft:bubble_column", map[string]string{"drag": "true"})
	for x := int32(0); x <= 5; x++ {
		for z := int32(0); z <= 5; z++ {
			srv.SetBlock(x, 62, z, magma)
			srv.SetBlock(x, 63, z, drag)
			srv.SetBlock(x, 64, z, drag)
		}
	}
	// Keep the hull's eye above water: a fully submerged boat stays on the
	// bottom and refuses passengers. vanilla: AbstractBoat.canAddPassenger
	boatID := srv.SpawnBoat(2.5, 64.5, 2.5, "minecraft:oak_boat")
	cow := srv.SpawnMob("minecraft:cow", 2.5, 65, 2.5)
	if boatID == 0 || cow == 0 {
		t.Fatal("spawns came to nothing")
	}
	servertest.RunTicks(t, srv, 1)
	if got := srv.BoatPassengerIDs(boatID); !slices.Equal(got, []int32{cow}) {
		t.Fatalf("passengers after boarding tick = %v, want cow %d", got, cow)
	}
	// The surface countdown lasts 60 ticks; the cow must still be seated
	// just before the throw, rather than lost to drowning or submersion.
	servertest.RunTicks(t, srv, 58)
	if got := srv.BoatPassengerIDs(boatID); !slices.Equal(got, []int32{cow}) {
		t.Fatalf("passengers before throw = %v, want cow %d", got, cow)
	}
	if !c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityEvent {
			return false
		}
		r := protocol.NewReader(data)
		return r.ReadI32() == boatID && r.ReadU8() == protocol.EntityStatusBoatSink
	}) {
		t.Fatal("the downward column did not send the sink event on tick 60")
	}
	if got := srv.BoatPassengerIDs(boatID); len(got) != 0 {
		t.Fatalf("passengers after downward throw = %v, want none", got)
	}
}

// TestAClickOnAnotherBoatMovesTheRider: a plain click on a free boat while
// seated in one takes the rider out of the old hull and into the new, and a
// click on the hull they already ride changes nothing.
func TestAClickOnAnotherBoatMovesTheRider(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Hopper", 0)
	boatA := srv.SpawnBoat(2.5, 64, 0.5, "minecraft:oak_boat")
	boatB := srv.SpawnBoat(2.5, 64, 2.5, "minecraft:oak_boat")
	if boatA == 0 || boatB == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.UseOn(boatA)
	servertest.AwaitPassengersFor(c, boatA)
	c.UseOn(boatA)
	c.Settle()
	if got := srv.BoatPassengerIDs(boatA); len(got) != 1 {
		t.Fatalf("a click on the ridden hull left it holding %v, want the rider", got)
	}

	c.UseOn(boatB)
	c.Settle()
	if got := srv.BoatPassengerIDs(boatA); len(got) != 0 {
		t.Errorf("the old boat still holds %v after the switch", got)
	}
	if got := srv.BoatPassengerIDs(boatB); len(got) != 1 || got[0] != c.PlayerEntityID {
		t.Errorf("the new boat holds %v, want the rider %d", got, c.PlayerEntityID)
	}
}

// TestAMobThatLeftABoatIsNotTakenAboardForSixtyTicks: a pig dismounted by
// /ride next to the boat that held it stays ashore for the 60-tick boarding
// cooldown, then the hull's sweep takes it again.
// vanilla: Entity.removePassenger, Entity.canRide
func TestAMobThatLeftABoatIsNotTakenAboardForSixtyTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Ferryman", servertest.OpsListing(t, "Ferryman"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	boatID := srv.SpawnBoat(2.5, 64, 2.5, "minecraft:oak_boat")
	pig := srv.SpawnMob("minecraft:pig", 2.5, 64, 2.5)
	if boatID == 0 || pig == 0 {
		t.Fatal("SpawnBoat or SpawnMob came to nothing")
	}
	aboard := func() bool { return slices.Contains(srv.BoatPassengerIDs(boatID), pig) }
	servertest.UntilTicks(t, srv, 20, "the pig taken aboard", aboard)

	c.ChatCommand("ride @e[type=pig,limit=1] dismount")
	servertest.RunTicks(t, srv, 2)
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	if aboard() {
		t.Fatal("the pig is still aboard after /ride dismount")
	}
	// The dismount set the pig beside the hull; keep putting it back under
	// it, so a wander away cannot make the refusal vacuous.
	for range 5 {
		c.ChatCommand("tp @e[type=pig,limit=1] 2.5 64 2.5")
		c.Settle()
		servertest.RunTicks(t, srv, 10)
		if aboard() {
			t.Fatal("the pig was taken aboard again inside the boarding cooldown")
		}
	}
	// About 50 ticks have run since the dismount; the cooldown ends within ten more.
	c.ChatCommand("tp @e[type=pig,limit=1] 2.5 64 2.5")
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the pig taken aboard after the cooldown", aboard)
}

// vanilla: ServerGamePacketListenerImpl.isEntityCollidingWithAnythingNew
func TestABoatOnlyRefusesNewCollisions(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Sailor", 0)
	servertest.HoldTicks(t, srv)
	boat := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	c.UseOn(boat)
	servertest.AwaitPassengersFor(c, boat)
	stone := servertest.MustState(t, "minecraft:stone", nil)
	srv.SetBlock(8, 64, 4, stone)
	report := func(x float64, refused bool) {
		t.Helper()
		c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
			b.PutF64(x)
			b.PutF64(64)
			b.PutF64(4.5)
			b.PutF32(0)
			b.PutF32(0)
			b.PutBool(true)
		})
		corrected := false
		c.SettleAndCollect(func(id int32, _ []byte) { corrected = corrected || id == protocol.IDPlayMoveVehicle })
		if corrected != refused {
			t.Errorf("report x=%v corrected = %v, want %v", x, corrected, refused)
		}
		if got, _, _, _, _, _ := srv.BoatState(boat); !refused && got != x {
			t.Errorf("boat x=%v, want %v", got, x)
		}
	}
	report(8-1.375/2, false)
	// The old hull already intersects this block, so a move within it is legal.
	srv.SetBlock(7, 64, 4, stone)
	report(7.2, false)
	report(7.4, true)
	if x, _, _, _, _, _ := srv.BoatState(boat); x != 7.2 {
		t.Errorf("refused report moved boat to %v, want 7.2", x)
	}
}

// paddleSounds rows a boat placed at the given spot with the paddles as
// reported and returns the paddle sounds the rower hears in 40 ticks.
func paddleSounds(t *testing.T, water, left, right bool) (wet, dry []servertest.HeardSound) {
	t.Helper()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Rower", 0)
	if water {
		for x := int32(-2); x <= 2; x++ {
			for z := int32(-2); z <= 2; z++ {
				srv.SetBlock(x+4, 63, z+4, servertest.MustState(t, "minecraft:water", nil))
			}
		}
	}
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	c.UseOn(boatID)
	servertest.AwaitPassengersFor(c, boatID)
	servertest.HoldTicks(t, srv)
	// The driver's client floats the hull in the pool or grounds it.
	y := 64.0
	if water {
		y = 63.8
	}
	c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(4.5)
		b.PutF64(y)
		b.PutF64(4.5)
		b.PutF32(0)
		b.PutF32(0)
		b.PutBool(!water)
	})
	c.Send(protocol.IDPlayPaddleBoatIn, func(b *protocol.Buffer) {
		b.PutBool(left)
		b.PutBool(right)
	})
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputForward) })
	heard, _, _ := servertest.CollectSoundsTicks(c, srv, 40)
	for _, h := range heard {
		switch h.Name {
		case "minecraft:entity.boat.paddle_water":
			wet = append(wet, h)
		case "minecraft:entity.boat.paddle_land":
			dry = append(dry, h)
		}
	}
	return wet, dry
}

// TestRowingABoatPlaysPaddleSounds: turned paddles sound in the neutral
// category as they pass the start of their arc, water or land by what the
// hull rests in, and nothing sounds with the paddles off.
func TestRowingABoatPlaysPaddleSounds(t *testing.T) {
	t.Parallel()
	check := func(name string, got []servertest.HeardSound) {
		if len(got) < 2 {
			t.Fatalf("%s sounds = %d, want at least 2", name, len(got))
		}
		for _, h := range got {
			if h.Category != protocol.SoundCategoryNeutral || h.Volume != 1 || h.Pitch < 0.8 || h.Pitch > 1.2 {
				t.Errorf("%s = %+v, want neutral, volume 1, pitch in [0.8, 1.2]", name, h)
			}
		}
	}
	t.Run("water", func(t *testing.T) {
		t.Parallel()
		wet, dry := paddleSounds(t, true, true, true)
		check("paddle_water", wet)
		if len(dry) != 0 {
			t.Errorf("paddle_land on water: %d", len(dry))
		}
	})
	t.Run("land", func(t *testing.T) {
		t.Parallel()
		wet, dry := paddleSounds(t, false, true, true)
		check("paddle_land", dry)
		if len(wet) != 0 {
			t.Errorf("paddle_water on land: %d", len(wet))
		}
	})
	t.Run("off", func(t *testing.T) {
		t.Parallel()
		wet, dry := paddleSounds(t, true, false, false)
		if len(wet)+len(dry) != 0 {
			t.Errorf("paddle sounds with the paddles off: %d", len(wet)+len(dry))
		}
	})
}
