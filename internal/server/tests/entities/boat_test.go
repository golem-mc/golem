package entities

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

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

// boatLowStep puts the step-off spot at (2.5, 0.5) under a ceiling only a
// crouching body clears: a slab step beside the hull with stone a block and
// a half over it.
func boatLowStep(t *testing.T, srv *server.Server) {
	t.Helper()
	srv.SetBlock(2, 64, 1, servertest.MustState(t, "minecraft:oak_slab", nil))
	srv.SetBlock(2, 66, 1, servertest.MustState(t, "minecraft:stone", nil))
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
	// on it.
	c = connect().JoinKeeping("Ferry")
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

// TestABoatLeftRidingWaitsForItsChunk: a rejoin whose load timeout runs out
// before its chunks have come keeps the rider's hull in their file until the
// chunk it stands in is theirs. Raised earlier, it would leave with that
// unheld chunk on the next tick and come back empty when the chunk loads.
func TestABoatLeftRidingWaitsForItsChunk(t *testing.T) {
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
	c.Disconnect()
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.BoatCount() == 0
	})

	// The rejoin's chunks are held back past the 60-tick load timeout, and
	// a few ticks beyond it, which would sweep a hull raised in the meantime.
	release := srv.HoldChunkSenders()
	t.Cleanup(release)
	c = connect()
	c.Login("Ferry")
	c.Configure()
	servertest.UntilTicks(t, srv, 100, "the rejoin's load timeout", func() bool {
		return srv.PlayerClientLoaded("Ferry")
	})
	servertest.RunTicks(t, srv, 3)
	if n := srv.BoatCount(); n != 0 {
		t.Errorf("boats standing before the rider's chunk came = %d, want the hull still in their file", n)
	}
	release()

	var joined []servertest.PushedPacket
	c.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlayLogin && c.PlayerEntityID == 0 {
			c.PlayerEntityID = protocol.NewReader(data).ReadI32()
		}
		joined = append(joined, servertest.PushedPacket{Id: id, Data: append([]byte(nil), data...)})
	})
	c.Unread(joined...)
	rejoined := servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)
	if riders := servertest.AwaitPassengersFor(c, rejoined.EntityID); len(riders) != 1 || riders[0] != c.PlayerEntityID {
		t.Errorf("passengers after the rejoin = %v, want the rejoining rider %d", riders, c.PlayerEntityID)
	}
	if rider, ok := srv.BoatRider(rejoined.EntityID); !ok || rider != "Ferry" {
		t.Errorf("the rejoined boat's rider = %q (%v), want Ferry back aboard", rider, ok)
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
