package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The strong power a lit dust line gives the block it lies on: the dust
// strongly powers the block below it, and that block passes the power to
// every neighbour. vanilla: RedStoneWireBlock.getDirectSignal,
// SignalGetter.getSignal/getDirectSignalTo, TntBlock.neighborChanged.

// TestLitDustPrimesTNTBesideTheBlockBelow checks the positive: a straight lit
// line of dust on a row of stone, and TNT beside that stone one level below
// the dust, primes because the stone is strongly powered.
func TestLitDustPrimesTNTBesideTheBlockBelow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	// The stone row under the line and the redstone block powering its head.
	servertest.Setblock(t, c, 5, 64, 5, "stone")
	servertest.Setblock(t, c, 6, 64, 5, "stone")
	servertest.Setblock(t, c, 7, 64, 5, "stone")
	servertest.Setblock(t, c, 4, 65, 5, "redstone_block")
	servertest.Setblock(t, c, 5, 65, 5, "redstone_wire[east=side]")
	servertest.Setblock(t, c, 6, 65, 5, "redstone_wire[east=side,west=side]")
	servertest.Setblock(t, c, 7, 65, 5, "redstone_wire[west=side]")

	// TNT beside the stone under the line's middle cell, one level below the
	// dust: the strongly powered stone primes it. The loop stops first so
	// the body is read where the prime put it, not a tick into its fall.
	srv.StopTick()
	servertest.Setblock(t, c, 6, 64, 6, "tnt")
	c.Settle()

	if got := srv.BlockAt(6, 64, 6); got == registry.BlockStates["minecraft:tnt"] {
		t.Fatal("the TNT beside the strongly powered stone never primed")
	}
	var found bool
	for _, it := range srv.TNTsInDimension("minecraft:overworld") {
		if it.X == 6.5 && it.Y == 64 && it.Z == 6.5 {
			found = true
		}
	}
	if !found {
		t.Errorf("no primed TNT stood beside the stone: %v", srv.TNTsInDimension("minecraft:overworld"))
	}
}

// TestDustLightingPrimesTNTAlreadyBesideTheBlockBelow checks the wake side:
// the TNT is already beside the stone before the dust lights, so the wire's
// own two-step wake has to reach it rather than the TNT's placement.
func TestDustLightingPrimesTNTAlreadyBesideTheBlockBelow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	// Stone with the TNT beside it and the unlit dust on top.
	servertest.Setblock(t, c, 6, 64, 5, "stone")
	servertest.Setblock(t, c, 6, 64, 6, "tnt")
	servertest.Setblock(t, c, 6, 65, 5, "redstone_wire")

	// Light the dust from the side. The loop stops first so the body is
	// read where the prime put it, not a tick into its fall.
	srv.StopTick()
	servertest.Setblock(t, c, 5, 65, 5, "redstone_block")
	c.Settle()

	if got := srv.BlockAt(6, 64, 6); got == registry.BlockStates["minecraft:tnt"] {
		t.Fatal("the TNT beside the strongly powered stone never primed")
	}
	var found bool
	for _, it := range srv.TNTsInDimension("minecraft:overworld") {
		if it.X == 6.5 && it.Y == 64 && it.Z == 6.5 {
			found = true
		}
	}
	if !found {
		t.Errorf("no primed TNT stood beside the stone: %v", srv.TNTsInDimension("minecraft:overworld"))
	}
}

// TestDustLightingLightsLampAlreadyBesideTheBlockBelow is the same reach for a
// lamp: it is already beside the stone before the dust lights.
func TestDustLightingLightsLampAlreadyBesideTheBlockBelow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 5, "stone")
	servertest.Setblock(t, c, 6, 64, 6, "redstone_lamp")
	servertest.Setblock(t, c, 6, 65, 5, "redstone_wire")
	servertest.Setblock(t, c, 5, 65, 5, "redstone_block")
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 6, 64, 6, "minecraft:redstone_lamp", map[string]string{"lit": "true"})
}

// TestBreakingLitDustTurnsOffLampBesideTheBlockBelow checks the removal wake:
// the lamp beside the stone goes dark when the lit dust above the stone is
// broken, the removal's wake having to reach it two steps away.
func TestBreakingLitDustTurnsOffLampBesideTheBlockBelow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	// The dust on the stone lit by the redstone block beside it.
	servertest.Setblock(t, c, 6, 64, 5, "stone")
	servertest.Setblock(t, c, 5, 65, 5, "redstone_block")
	servertest.Setblock(t, c, 6, 65, 5, "redstone_wire")
	c.Settle()

	// A neighbour change makes the lamp read the dust's strong power
	// through the stone.
	servertest.Setblock(t, c, 6, 64, 6, "redstone_lamp")
	servertest.Setblock(t, c, 6, 65, 6, "stone")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 6, 64, 6, "minecraft:redstone_lamp", map[string]string{"lit": "true"})

	// Break the lit dust: the lamp waits out its four-tick off delay.
	servertest.Setblock(t, c, 6, 65, 5, "air")
	c.PumpTicks(srv, 6, func(int32, []byte) bool { return false })
	servertest.ExpectBlockNamed(t, srv, 6, 64, 6, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
}

// TestDustDoesNotPowerTNTBesideAnUnconnectedSide is the negative: a dust cell
// in a straight line points at neither side, so TNT beside the dust itself,
// at the dust's own level, stays unlit. vanilla: RedStoneWireBlock.getSignal's
// connection check.
func TestDustDoesNotPowerTNTBesideAnUnconnectedSide(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	// A straight north-south line on the ground, its head fed by a redstone
	// block, and TNT beside the middle cell facing a side the line does not
	// connect.
	servertest.Setblock(t, c, 6, 64, 4, "redstone_wire[north=side,south=side]")
	servertest.Setblock(t, c, 6, 64, 5, "redstone_wire[north=side,south=side]")
	servertest.Setblock(t, c, 6, 64, 6, "redstone_wire[north=side,south=side]")
	servertest.Setblock(t, c, 5, 64, 4, "redstone_block")
	servertest.Setblock(t, c, 7, 64, 5, "tnt")
	c.Settle()

	if got := srv.BlockAt(7, 64, 5); got != registry.BlockStates["minecraft:tnt"] {
		t.Error("the TNT beside the unconnected side of a straight dust line primed, want it left standing")
	}
}

// TestLeverThroughTheFloorLightsTheLampBelow pulls a lever on a stone floor
// with a lamp under the floor: the lever strongly powers the stone and the
// lamp, whose only neighbour is the stone, reads that strong power through
// the conductor and lights. vanilla: SignalGetter.getSignal/getDirectSignalTo.
func TestLeverThroughTheFloorLightsTheLampBelow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 3, 64, 0, "stone")
	servertest.Setblock(t, c, 3, 63, 0, "redstone_lamp")
	servertest.Setblock(t, c, 3, 65, 0, "lever[face=floor,facing=north]")
	c.Settle()

	c.UseBlock(3, 65, 0)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 3, 63, 0, "minecraft:redstone_lamp", map[string]string{"lit": "true"})
}

// TestSetblockRedstoneWireTakesItsConnections gives a bare /setblock wire a
// trapped chest beside it and checks the wire leaves its placement with the
// neighbour-shape update's isolated-pair connections rather than none. A
// trapped chest is a signal source, a plain chest is not, so only the trapped
// one gives the wire a side to reach out from.
// vanilla: BlockInput.place, Block.updateFromNeighbourShapes.
func TestSetblockRedstoneWireTakesItsConnections(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 5, 64, 5, "trapped_chest")
	servertest.Setblock(t, c, 6, 64, 5, "redstone_wire")
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 6, 64, 5, "minecraft:redstone_wire",
		map[string]string{"east": "side", "west": "side"})
}

// TestSetblockRedstoneWireStaysADotWithoutANeighbour is the negative: a bare
// /setblock wire with nothing that connects to it stays the dot it was placed
// as, rather than taking the isolated-pair line a fresh recompute would give.
// vanilla: RedStoneWireBlock.getConnectionState's wasDot && isDot return.
func TestSetblockRedstoneWireStaysADotWithoutANeighbour(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 5, "redstone_wire")
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 6, 64, 5, "minecraft:redstone_wire", map[string]string{
		"east": "none", "north": "none", "south": "none", "west": "none",
	})
}

// TestSetblockRedstoneWireDropsAStaleCommandSide places a wire whose named
// side no neighbour supports: the shape pass recomputes from the neighbours,
// dropping the side the command named, rather than keeping it.
// vanilla: RedStoneWireBlock.getConnectionState restarting from
// defaultBlockState.
func TestSetblockRedstoneWireDropsAStaleCommandSide(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 5, "redstone_wire[north=side]")
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 6, 64, 5, "minecraft:redstone_wire", map[string]string{
		"east": "side", "west": "side", "north": "side", "south": "side",
	})
}

// TestPlayerPlacedLoneDustIsACross places a single dust with nothing that
// connects to it: the placement's getConnectionState runs the isolated-pair
// rule over an all-empty neighbourhood and gives every side a side, so the
// dust is the four-armed cross, not an east-west line.
// vanilla: RedStoneWireBlock.getStateForPlacement/getConnectionState.
func TestPlayerPlacedLoneDustIsACross(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.PlaceFrom(c, "minecraft:redstone", 4, 5, 0, 4, 63, 6, protocol.FaceTop)

	servertest.ExpectBlockNamed(t, srv, 4, 64, 6, "minecraft:redstone_wire", map[string]string{
		"east": "side", "north": "side", "south": "side", "west": "side",
	})
}

// TestRemovingASourceDropsDustFedThroughABlock covers a lever on a block
// strongly powering it, and dust on the ground beside that block reading the
// power through it — a straight run, a branch and a loop. Removing the lever
// has to take every wire to 0, including the head wire that only ever read
// the block. vanilla: RedStoneWireBlock.getBlockSignal, Level.getSignal.
func TestRemovingASourceDropsDustFedThroughABlock(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 4, 64, 5, "stone")
	servertest.Setblock(t, c, 4, 65, 5, "lever[face=floor,facing=north,powered=true]")
	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire")
	servertest.Setblock(t, c, 6, 64, 5, "redstone_wire")
	servertest.Setblock(t, c, 7, 64, 5, "redstone_wire")
	servertest.Setblock(t, c, 6, 64, 4, "redstone_wire")
	servertest.Setblock(t, c, 8, 64, 5, "redstone_wire")
	servertest.Setblock(t, c, 8, 64, 6, "redstone_wire")
	servertest.Setblock(t, c, 7, 64, 6, "redstone_wire")
	c.Settle()

	cells := [][3]int32{
		{5, 64, 5}, {6, 64, 5}, {7, 64, 5}, {6, 64, 4},
		{8, 64, 5}, {8, 64, 6}, {7, 64, 6},
	}
	for _, p := range cells {
		if _, props := servertest.StateProps(srv.BlockAt(p[0], p[1], p[2])); props["power"] == "0" {
			t.Fatalf("setup: dust at %v never lit", p)
		}
	}

	servertest.Setblock(t, c, 4, 65, 5, "air")
	c.PumpTicks(srv, 25, func(int32, []byte) bool { return false })
	for _, p := range cells {
		name, props := servertest.StateProps(srv.BlockAt(p[0], p[1], p[2]))
		if name == "minecraft:redstone_wire" && props["power"] != "0" {
			t.Errorf("dust at %v has power %s after the lever went, want 0", p, props["power"])
		}
	}
}

// TestRemovingAPlateDropsDustFedThroughTheLamp is the same circuit with a
// lamp in place of the stone: the plate on the lamp strongly powers it, dust
// beside the lamp reads that power, and breaking the plate has to take the
// dust to 0.
func TestRemovingAPlateDropsDustFedThroughTheLamp(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 4, 64, 5, "redstone_lamp")
	servertest.Setblock(t, c, 4, 65, 5, "oak_pressure_plate[powered=true]")
	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire")
	servertest.Setblock(t, c, 6, 64, 5, "redstone_wire")
	servertest.Setblock(t, c, 7, 64, 5, "redstone_wire")
	c.Settle()

	cells := [][3]int32{{5, 64, 5}, {6, 64, 5}, {7, 64, 5}}
	for _, p := range cells {
		if _, props := servertest.StateProps(srv.BlockAt(p[0], p[1], p[2])); props["power"] == "0" {
			t.Fatalf("setup: dust at %v never lit", p)
		}
	}

	servertest.Setblock(t, c, 4, 65, 5, "air")
	c.PumpTicks(srv, 25, func(int32, []byte) bool { return false })
	for _, p := range cells {
		name, props := servertest.StateProps(srv.BlockAt(p[0], p[1], p[2]))
		if name == "minecraft:redstone_wire" && props["power"] != "0" {
			t.Errorf("dust at %v has power %s after the plate went, want 0", p, props["power"])
		}
	}
}

// TestBreakingASourceLeavesABudPistonByTheStoneAlone puts a retracted piston
// beside the stone a floor source stands on and breaks the source. The piston
// is quasi-powered through a redstone block beside the air above it, so it
// extends the moment its own cell hears a block update. A source that carried
// no signal must wake nothing, leaving the piston retracted; one that carried
// a signal wakes the block it powered and that block's neighbours - the ring
// the piston sits in - so the piston extends.
func TestBreakingASourceLeavesABudPistonByTheStoneAlone(t *testing.T) {
	t.Parallel()
	sources := []struct {
		name    string
		off, on string
	}{
		{"lever", "minecraft:lever[face=floor,facing=north]",
			"minecraft:lever[face=floor,facing=north,powered=true]"},
		{"button", "minecraft:oak_button[face=floor,facing=north,powered=false]",
			"minecraft:oak_button[face=floor,facing=north,powered=true]"},
		{"plate", "minecraft:oak_pressure_plate[powered=false]",
			"minecraft:oak_pressure_plate[powered=true]"},
	}
	for _, tc := range sources {
		for _, powered := range []bool{false, true} {
			name := tc.name + "Unpowered"
			source := tc.off
			extended := "false"
			if powered {
				name = tc.name + "Powered"
				source = tc.on
				extended = "true"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				srv, c := servertest.AttachActor(t)

				servertest.Setblock(t, c, 4, 64, 5, "stone")
				// The piston goes down before the source: its placement
				// reads no power, so it starts retracted.
				servertest.Setblock(t, c, 5, 64, 5, "piston[facing=east]")
				// The piston's quasi-connected power: a redstone block
				// beside the air above it, no direct line to the piston.
				servertest.Setblock(t, c, 6, 65, 5, "redstone_block")
				servertest.Setblock(t, c, 4, 65, 5, source)
				c.Settle()
				if got, props := servertest.StateProps(srv.BlockAt(5, 64, 5)); got != "minecraft:piston" || props["extended"] != "false" {
					t.Fatalf("setup: the piston started %s %v, want it retracted", got, props)
				}

				servertest.Setblock(t, c, 4, 65, 5, "air")
				c.PumpTicks(srv, 8, func(int32, []byte) bool { return false })

				if got, props := servertest.StateProps(srv.BlockAt(5, 64, 5)); got != "minecraft:piston" || props["extended"] != extended {
					t.Fatalf("the piston hearing the %s removal: %s %v, want extended=%s", name, got, props, extended)
				}
			})
		}
	}
}

// TestBreakingAPressedButtonReleasesTheBlockItHangsOn presses a wall and a
// ceiling button and breaks each while it is down. The dust beside the block
// a button hangs on has to go dark, which needs the removal to wake that
// block's neighbours rather than the block under the button.
func TestBreakingAPressedButtonReleasesTheBlockItHangsOn(t *testing.T) {
	t.Parallel()
	t.Run("wall", func(t *testing.T) {
		t.Parallel()
		srv, c := servertest.AttachActor(t)

		servertest.Setblock(t, c, 5, 64, 0, "stone")
		servertest.Setblock(t, c, 6, 64, 0, "redstone_wire")
		servertest.Setblock(t, c, 4, 64, 0, "oak_button[face=wall,facing=west,powered=false]")
		c.Settle()

		c.UseBlock(4, 64, 0)
		c.Settle()
		servertest.ExpectBlockNamed(t, srv, 6, 64, 0, "minecraft:redstone_wire", map[string]string{"power": "15"})

		servertest.Setblock(t, c, 4, 64, 0, "air")
		c.Settle()
		servertest.ExpectBlockNamed(t, srv, 6, 64, 0, "minecraft:redstone_wire", map[string]string{"power": "0"})
	})
	t.Run("ceiling", func(t *testing.T) {
		t.Parallel()
		srv, c := servertest.AttachActor(t)

		servertest.Setblock(t, c, 6, 64, 0, "stone")
		servertest.Setblock(t, c, 6, 65, 0, "redstone_wire")
		servertest.Setblock(t, c, 5, 65, 0, "stone")
		servertest.Setblock(t, c, 5, 64, 0, "oak_button[face=ceiling,facing=north,powered=false]")
		c.Settle()

		c.UseBlock(5, 64, 0)
		c.Settle()
		servertest.ExpectBlockNamed(t, srv, 6, 65, 0, "minecraft:redstone_wire", map[string]string{"power": "15"})

		servertest.Setblock(t, c, 5, 64, 0, "air")
		c.Settle()
		servertest.ExpectBlockNamed(t, srv, 6, 65, 0, "minecraft:redstone_wire", map[string]string{"power": "0"})
	})
}

// TestAPistonBreakingASourceWakesItsAttachedBlock puts a powered floor source
// on a stone and breaks it with a piston. The move destroys the cell with
// movedByPiston false, so the removal wake runs: the block the source stood on
// wakes and a retracted piston beside it extends.
// vanilla: PistonBaseBlock.moveBlocks, BasePressurePlateBlock.affectNeighborsAfterRemoval
func TestAPistonBreakingASourceWakesItsAttachedBlock(t *testing.T) {
	t.Parallel()
	sources := []struct {
		name  string
		state string
	}{
		{"lever", "minecraft:lever[face=floor,facing=north,powered=true]"},
		{"plate", "minecraft:oak_pressure_plate[powered=true]"},
	}
	for _, tc := range sources {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)

			servertest.Setblock(t, c, 4, 64, 5, "stone")
			// The BUD piston goes down before the source, so it starts
			// retracted, quasi-powered by the redstone block.
			servertest.Setblock(t, c, 5, 64, 5, "piston[facing=east]")
			servertest.Setblock(t, c, 6, 65, 5, "redstone_block")
			servertest.Setblock(t, c, 4, 65, 5, tc.state)
			// The destroyer, unpowered until the redstone block lands.
			servertest.Setblock(t, c, 3, 65, 5, "piston[facing=east]")
			c.Settle()
			if got, props := servertest.StateProps(srv.BlockAt(5, 64, 5)); got != "minecraft:piston" || props["extended"] != "false" {
				t.Fatalf("setup: the piston started %s %v", got, props)
			}

			servertest.Setblock(t, c, 3, 64, 5, "redstone_block")
			c.PumpTicks(srv, 8, func(int32, []byte) bool { return false })

			if got, props := servertest.StateProps(srv.BlockAt(5, 64, 5)); got != "minecraft:piston" || props["extended"] != "true" {
				t.Fatalf("the piston beside the broken source's block: %s %v, want it extended", got, props)
			}
		})
	}
}

// TestPoweringATorchsBaseTurnsOffTheLampAbove lights a lamp on a block a
// torch stands under, then powers the torch's base with a wall lever: the
// torch goes dark two ticks later and the lamp must go with it, which needs
// the torch's state change to wake the neighbours of its neighbours.
// vanilla: RedstoneTorchBlock.tick/notifyNeighbors.
func TestPoweringATorchsBaseTurnsOffTheLampAbove(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 3, 64, 0, "stone")
	servertest.Setblock(t, c, 3, 65, 0, "redstone_torch")
	servertest.Setblock(t, c, 3, 66, 0, "stone")
	servertest.Setblock(t, c, 3, 67, 0, "redstone_lamp")
	servertest.Setblock(t, c, 3, 68, 0, "stone")
	servertest.Setblock(t, c, 2, 64, 0, "lever[face=wall,facing=west]")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 3, 67, 0, "minecraft:redstone_lamp", map[string]string{"lit": "true"})

	lampOff, _ := registry.FindState("minecraft:redstone_lamp", map[string]string{"lit": "false"})
	c.UseBlock(2, 64, 0)
	c.Settle()
	servertest.WaitForTicks(t, srv, 20, func() bool { return srv.BlockAt(3, 67, 0) == lampOff })

	servertest.ExpectBlockNamed(t, srv, 3, 65, 0, "minecraft:redstone_torch", map[string]string{"lit": "false"})
	servertest.ExpectBlockNamed(t, srv, 3, 67, 0, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
}

// TestRepeaterStaysOffWhenLockedAfterItsTickWasBooked is the issue's repro: a
// repeater R facing west has its input rise while a side diode powering up
// books its own visit in the same tick. The locker's visit runs first, so R
// is already locked when its booked visit runs; R must skip it and stay off.
// vanilla: DiodeBlock.tick -> isLocked.
func TestRepeaterStaysOffWhenLockedAfterItsTickWasBooked(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	// R at (11, 64, 9) faces west, its input at (10, 64, 9). The locker at
	// (11, 64, 10) faces south, its output north into R's side and its
	// input at (11, 64, 11). Both inputs rise in one tick, the locker's
	// first, so its visit is the earlier of the two due together.
	servertest.Setblock(t, c, 11, 64, 9, "repeater[facing=west,delay=1]")
	servertest.Setblock(t, c, 11, 64, 10, "repeater[facing=south,delay=1]")
	servertest.Setblock(t, c, 11, 64, 11, "redstone_block")
	servertest.Setblock(t, c, 10, 64, 9, "redstone_block")

	srv.TickPhysics(3)

	servertest.ExpectBlockNamed(t, srv, 11, 64, 10, "minecraft:repeater", map[string]string{"powered": "true"})
	servertest.ExpectBlockNamed(t, srv, 11, 64, 9, "minecraft:repeater", map[string]string{"powered": "false"})
}

// TestObserverWatchingARepeaterSeesTheLockFlip points an observer at a
// repeater and locks it from the side: the shape pass that writes LOCKED must
// run the repeater's own neighbour shape pass, which books the observer's
// pulse. vanilla: RepeaterBlock.updateShape -> LevelChunk.setBlockState.
func TestObserverWatchingARepeaterSeesTheLockFlip(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	// R at (11, 64, 9) faces west. The locker at (11, 64, 10) faces
	// south, its output north into R's side, fed by the redstone block at
	// (11, 64, 11). The observer at (11, 64, 8) faces south, watching R.
	servertest.Setblock(t, c, 11, 64, 9, "repeater[facing=west,delay=1]")
	servertest.Setblock(t, c, 11, 64, 10, "repeater[facing=south,delay=1]")
	// Isolate the lock edge from the observer's command-placement pulse.
	servertest.Setblock(t, c, 11, 64, 8, "observer[facing=south] strict")
	servertest.Setblock(t, c, 11, 64, 11, "redstone_block")

	fired := c.PumpTicks(srv, 40, func(id int32, data []byte) bool {
		if id != protocol.IDPlayBlockUpdate {
			return false
		}
		r := protocol.NewReader(data)
		x, y, z := r.ReadPosition()
		if x != 11 || y != 64 || z != 8 {
			return false
		}
		_, props := servertest.StateProps(uint16(r.ReadVarInt()))
		return props["powered"] == "true"
	})
	if !fired {
		t.Error("the observer watching the repeater never pulsed on the lock")
	}
}

// TestRepeaterPlacedBesideAPoweredDiodeArrivesLocked places a repeater beside
// a powered diode aimed at its side. The placement reads the lock and tells
// the clients the LOCKED state; unpowering the side writes it back off.
// vanilla: RepeaterBlock.getStateForPlacement, updateShape.
func TestRepeaterPlacedBesideAPoweredDiodeArrivesLocked(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	// A powered repeater at (5, 64, 5) faces south: its output points north,
	// into the side of the repeater placed at (5, 64, 4), on the stone there.
	servertest.Setblock(t, c, 5, 64, 5, "repeater[facing=south,powered=true]")

	locked, _ := registry.FindState("minecraft:repeater", map[string]string{"facing": "west", "locked": "true"})
	unlocked, _ := registry.FindState("minecraft:repeater", map[string]string{"facing": "west"})

	// The actor stands north of the support and looks east (yaw 270), so the
	// repeater it sets down faces west and its axis runs across the diode.
	id, _ := registry.ItemID("minecraft:repeater")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: id, Count: 1})
	c.MoveToGround(5.5, 64, 3.5, true)
	c.FacePlayer(5.5, 64, 3.5, 270, 0)
	c.Settle()
	c.PlaceBlock(5, 63, 4, protocol.FaceTop)
	servertest.AwaitBlockUpdate(t, c, 5, 64, 4, locked)
	servertest.ExpectBlockNamed(t, srv, 5, 64, 4, "minecraft:repeater", map[string]string{"facing": "west", "locked": "true"})

	// The placement's tail echoes the state a second time; drain it so the
	// update the unlock writes is the one the next read finds.
	c.Settle()

	// Take the side diode's power away: the shape update writes LOCKED off.
	servertest.Setblock(t, c, 5, 64, 5, "repeater[facing=south,powered=false]")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 4, unlocked)
	servertest.ExpectBlockNamed(t, srv, 5, 64, 4, "minecraft:repeater", map[string]string{"facing": "west", "locked": "false"})
}

// pistonFacingDelta is the cell step each facing property names.
func pistonFacingDelta(facing string) (int32, int32, int32) {
	switch facing {
	case "down":
		return 0, -1, 0
	case "up":
		return 0, 1, 0
	case "north":
		return 0, 0, -1
	case "south":
		return 0, 0, 1
	case "west":
		return -1, 0, 0
	default:
		return 1, 0, 0
	}
}

// TestDownFacingPistonIgnoresTheRedstoneBlockInFront is the reported setup: a
// piston facing down with a redstone block directly below its face stays
// retracted, plain and sticky, because a piston reads no power from the side
// it faces. vanilla: PistonBaseBlock.getNeighborSignal.
func TestDownFacingPistonIgnoresTheRedstoneBlockInFront(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, piston string }{
		{"plain", "piston"},
		{"sticky", "sticky_piston"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)

			// The piston goes down first, retracted; the redstone block
			// below is then the block update that would wake it.
			servertest.Setblock(t, c, 6, 66, 5, tc.piston+"[facing=down]")
			c.Settle()
			servertest.ExpectBlockNamed(t, srv, 6, 66, 5, "minecraft:"+tc.piston, map[string]string{"extended": "false"})

			servertest.Setblock(t, c, 6, 65, 5, "redstone_block")
			c.Settle()
			c.PumpTicks(srv, 40, func(int32, []byte) bool { return false })

			servertest.ExpectBlockNamed(t, srv, 6, 66, 5, "minecraft:"+tc.piston, map[string]string{"extended": "false", "facing": "down"})
			servertest.ExpectBlockNamed(t, srv, 6, 65, 5, "minecraft:redstone_block", nil)
		})
	}
}

// TestPistonIgnoresARedstoneBlockInFrontOnTheOtherFacings is the same rule for
// the five facings but down: the redstone block in front powers nothing, so
// the piston holds its ground and the block does not move.
// vanilla: PistonBaseBlock.getNeighborSignal.
func TestPistonIgnoresARedstoneBlockInFrontOnTheOtherFacings(t *testing.T) {
	t.Parallel()
	for i, facing := range []string{"up", "north", "south", "west", "east"} {
		t.Run(facing, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)

			dx, dy, dz := pistonFacingDelta(facing)
			for j, tc := range []struct{ name, piston string }{
				{"plain", "piston"},
				{"sticky", "sticky_piston"},
			} {
				bx, by, bz := int32(8+i), int32(70), int32(8+j*4)
				fx, fy, fz := bx+dx, by+dy, bz+dz
				servertest.Setblock(t, c, bx, by, bz, tc.piston+"[facing="+facing+"]")
				c.Settle()
				servertest.ExpectBlockNamed(t, srv, bx, by, bz, "minecraft:"+tc.piston, map[string]string{"extended": "false"})

				// The redstone block in front is the wake; the cells beyond
				// it are air, so a buggy extend has somewhere to push it.
				servertest.Setblock(t, c, fx, fy, fz, "redstone_block")
				c.Settle()
				c.PumpTicks(srv, 40, func(int32, []byte) bool { return false })

				servertest.ExpectBlockNamed(t, srv, bx, by, bz, "minecraft:"+tc.piston, map[string]string{"extended": "false", "facing": facing})
				servertest.ExpectBlockNamed(t, srv, fx, fy, fz, "minecraft:redstone_block", nil)
			}
		})
	}
}

// TestPistonReadsQuasiConnectivityFromTwoAbove is the piston's quasi reading:
// a redstone block two cells above a retracted piston powers it once a block
// update wakes the piston. vanilla: PistonBaseBlock.getNeighborSignal.
func TestPistonReadsQuasiConnectivityFromTwoAbove(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 5, 64, 5, "piston[facing=east]")
	servertest.Setblock(t, c, 5, 66, 5, "redstone_block")
	c.Settle()

	// The power is not beside the piston, so nothing has woken it.
	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:piston", map[string]string{"extended": "false"})

	// A stone beside the piston wakes it and the quasi reading runs.
	servertest.Setblock(t, c, 4, 64, 5, "stone")
	c.PumpTicks(srv, 8, func(int32, []byte) bool { return false })

	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:piston", map[string]string{"extended": "true"})
}

// TestRepeaterBooksExtremelyHighBeforeALockerBookedLater: R's front is empty,
// so R books HIGH and the pulse placed first books it before the locker; the
// locker faces R, a diode not facing back, so its EXTREMELY_HIGH visit runs
// first and locks R.
// vanilla: DiodeBlock.checkTickOnNeighbor, DiodeBlock.shouldPrioritize.
func TestRepeaterBooksExtremelyHighBeforeALockerBookedLater(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 11, 64, 9, "repeater[facing=west,delay=1]")
	servertest.Setblock(t, c, 11, 64, 10, "repeater[facing=south,delay=1]")
	servertest.Setblock(t, c, 10, 64, 9, "redstone_block")
	servertest.Setblock(t, c, 11, 64, 11, "redstone_block")

	srv.TickPhysics(3)

	servertest.ExpectBlockNamed(t, srv, 11, 64, 10, "minecraft:repeater", map[string]string{"powered": "true"})
	servertest.ExpectBlockNamed(t, srv, 11, 64, 9, "minecraft:repeater", map[string]string{"powered": "false"})
}

// TestRepeaterFacingIntoADiodeThatFacesBackKeepsHighPriority puts a diode in
// front of R that outputs back into it: R is not prioritised, so the locker
// still runs first.
func TestRepeaterFacingIntoADiodeThatFacesBackKeepsHighPriority(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 11, 64, 9, "repeater[facing=west,delay=1]")
	servertest.Setblock(t, c, 12, 64, 9, "repeater[facing=east,delay=1]")
	servertest.Setblock(t, c, 11, 64, 10, "repeater[facing=south,delay=1]")
	servertest.Setblock(t, c, 10, 64, 9, "redstone_block")
	servertest.Setblock(t, c, 11, 64, 11, "redstone_block")

	srv.TickPhysics(3)

	servertest.ExpectBlockNamed(t, srv, 11, 64, 9, "minecraft:repeater", map[string]string{"powered": "false"})
}

// TestRepeaterFacingIntoADiodeThatDoesNotFaceBackIsPrioritised puts a diode in
// front of R that does not face back: R books EXTREMELY_HIGH like the locker
// and, booked first, runs first and turns on.
func TestRepeaterFacingIntoADiodeThatDoesNotFaceBackIsPrioritised(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	servertest.Setblock(t, c, 11, 64, 9, "repeater[facing=west,delay=1]")
	servertest.Setblock(t, c, 12, 64, 9, "repeater[facing=west,delay=1]")
	servertest.Setblock(t, c, 11, 64, 10, "repeater[facing=south,delay=1]")
	servertest.Setblock(t, c, 10, 64, 9, "redstone_block")
	servertest.Setblock(t, c, 11, 64, 11, "redstone_block")

	srv.TickPhysics(3)

	servertest.ExpectBlockNamed(t, srv, 11, 64, 9, "minecraft:repeater", map[string]string{"powered": "true"})
}
