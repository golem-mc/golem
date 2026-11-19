// The rails, black-box: the shape each rail takes when it lands — the
// straight line, the curve or the slope the rails around it dictate — the
// axis its placement first writes when nothing is around, the re-link that
// turns the rails beside it, and the re-derivation a neighbouring change
// sets off; and the powered and activator rail taking redstone and passing
// it along their own lines. Minecarts are another issue's.
package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// railPlayer joins a builder into the flat world holding n of the named
// rail in its hotbar, standing in reach of the floor east and south of
// spawn.
func railPlayer(t *testing.T, srv *server.Server, c *servertest.Client, rail string, n int32) {
	t.Helper()
	c.MoveTo(0.5, 64, 3.5, 0, 60)
	c.Settle()
	srv.GiveSlot("Builder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, rail), Count: n})
}

// TestPlacingARailBesideARailLinesUpWithIt puts each of the four rails down
// beside one of its own kind and checks the pair line up east-west: the
// shape derivation reads the rails around it whatever the kind, and the
// rail already standing re-links to the newcomer. The player faces south,
// so the north-south placement state is the one the pair has to beat.
func TestPlacingARailBesideARailLinesUpWithIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		rail string
	}{
		{"a plain rail", "minecraft:rail"},
		{"a powered rail", "minecraft:powered_rail"},
		{"a detector rail", "minecraft:detector_rail"},
		{"an activator rail", "minecraft:activator_rail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")
			railPlayer(t, srv, c, tc.rail, 2)
			// A rail of the kind standing one east, carrying the shape it
			// has when nothing around it had a say.
			if !srv.SetBlock(3, 64, 3, servertest.MustState(t, tc.rail, nil)) {
				t.Fatal("the standing rail's chunk is not loaded")
			}
			c.PlaceBlock(2, 63, 3, protocol.FaceTop)
			c.ExpectBlockAck()

			servertest.ExpectBlockNamed(t, srv, 2, 64, 3, tc.rail, map[string]string{"shape": "east_west"})
			servertest.ExpectBlockNamed(t, srv, 3, 64, 3, tc.rail, map[string]string{"shape": "east_west"})
		})
	}
}

// TestPlacingARailBetweenTwoRailsTakesTheCurve places a plain rail with
// rails standing east and south of it — the third row of the issue's
// table — and checks the curve the three are left in: the newcomer curves
// south-east, the rail at its east is re-linked into the line, and the one
// at its south already pointed at the newcomer so it stays as it was.
func TestPlacingARailBetweenTwoRailsTakesTheCurve(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")
	railPlayer(t, srv, c, "minecraft:rail", 1)
	if !srv.SetBlock(3, 64, 3, servertest.MustState(t, "minecraft:rail", nil)) ||
		!srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:rail", nil)) {
		t.Fatal("the standing rails' chunks are not loaded")
	}
	c.PlaceBlock(2, 63, 3, protocol.FaceTop)
	c.ExpectBlockAck()

	servertest.ExpectBlockNamed(t, srv, 2, 64, 3, "minecraft:rail",
		map[string]string{"shape": "south_east"})
	servertest.ExpectBlockNamed(t, srv, 3, 64, 3, "minecraft:rail",
		map[string]string{"shape": "east_west"})
	servertest.ExpectBlockNamed(t, srv, 2, 64, 4, "minecraft:rail",
		map[string]string{"shape": "north_south"})
}

// TestPlacingARailUnderARaiseDRailAscends places a rail with one standing
// a block up and one east of it — the last row of the issue's table — and
// checks the newcomer runs up to it as ascending_east, and the raised rail
// itself is re-linked to the line the pair makes.
func TestPlacingARailUnderARaisedRailAscends(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")
	railPlayer(t, srv, c, "minecraft:rail", 1)
	// A stone block to stand the raised rail on, the rail on top of it.
	if !srv.SetBlock(3, 64, 3, servertest.MustState(t, "minecraft:stone", nil)) ||
		!srv.SetBlock(3, 65, 3, servertest.MustState(t, "minecraft:rail", nil)) {
		t.Fatal("the raised rail's chunk is not loaded")
	}
	c.PlaceBlock(2, 63, 3, protocol.FaceTop)
	c.ExpectBlockAck()

	servertest.ExpectBlockNamed(t, srv, 2, 64, 3, "minecraft:rail",
		map[string]string{"shape": "ascending_east"})
	servertest.ExpectBlockNamed(t, srv, 3, 65, 3, "minecraft:rail",
		map[string]string{"shape": "east_west"})
}

// TestARailPlacedOnEmptyGroundKeepsTheLookAxis checks the placement state's
// own axis: a rail placed with nothing around it stands east-west for a
// builder looking west and north-south for one looking north.
// vanilla: BaseRailBlock.getStateForPlacement
func TestARailPlacedOnEmptyGroundKeepsTheLookAxis(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")
	railPlayer(t, srv, c, "minecraft:rail", 2)

	c.FacePlayer(0.5, 64, 3.5, 90, 60)
	c.PlaceBlock(2, 63, 3, protocol.FaceTop)
	c.ExpectBlockAck()
	servertest.ExpectBlockNamed(t, srv, 2, 64, 3, "minecraft:rail",
		map[string]string{"shape": "east_west"})

	// Far enough east that neither rail reaches the other's columns.
	c.MoveTo(3.5, 64, 7.5, 180, 60)
	c.PlaceBlock(3, 63, 8, protocol.FaceTop)
	c.ExpectBlockAck()
	servertest.ExpectBlockNamed(t, srv, 3, 64, 8, "minecraft:rail",
		map[string]string{"shape": "north_south"})
}

// TestBreakingASignalSourceBesideARailJunctionReshapesIt checks the
// neighbour change: a redstone block broken beside a rail holding three
// other rails makes that rail re-derive its shape from the three that are
// left, turning its north-south into the north-east curve and re-linking
// the rail it turned toward. Rails do not re-derive when the neighbour
// that went was no signal source — vanilla's plain rail keeps its shape
// through a lost rail — so it takes the broken signal source to prove the
// path.
func TestBreakingASignalSourceBesideARailJunctionReshapesIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")
	railPlayer(t, srv, c, "minecraft:rail", 1)
	// A three-way junction: rails north, east and south, a redstone block
	// west. Seeded directly, so no shape has been derived anywhere yet.
	for _, pos := range [][3]int32{{2, 64, 2}, {2, 64, 3}, {2, 64, 4}, {3, 64, 3}} {
		if !srv.SetBlock(pos[0], pos[1], pos[2], servertest.MustState(t, "minecraft:rail", nil)) {
			t.Fatalf("the rail at (%d, %d, %d) did not stand", pos[0], pos[1], pos[2])
		}
	}
	if !srv.SetBlock(1, 64, 3, servertest.MustState(t, "minecraft:redstone_block", nil)) {
		t.Fatal("the redstone block's chunk is not loaded")
	}
	servertest.ExpectBlockNamed(t, srv, 2, 64, 3, "minecraft:rail",
		map[string]string{"shape": "north_south"})

	c.BreakBlock(1, 64, 3)
	c.ExpectBlockAck()

	// The junction rail re-derives from the rails north, south and east of
	// it — no signal answers now, and the no-signal curve order ends on its
	// last match, south-east — and the rail at its east is pulled into the
	// line.
	servertest.ExpectBlockNamed(t, srv, 2, 64, 3, "minecraft:rail",
		map[string]string{"shape": "south_east"})
	servertest.ExpectBlockNamed(t, srv, 3, 64, 3, "minecraft:rail",
		map[string]string{"shape": "east_west"})
}

// TestRailsReshapeBeyondTheOverworld checks the same placement derivation
// in another dimension: the end's arrival platform has the engine of every
// other world, and the rail placed beside one standing on the obsidian
// lines up with it there too.
func TestRailsReshapeBeyondTheOverworld(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")

	arrival := world.EndArrival()
	srv.MovePlayerToDimension("Builder", world.EndName,
		float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+0.5)
	// The arrival's chunks have to be read: a connection that stops reading
	// holds the whole server's tick loop up on its socket.
	c.Settle()
	srv.GiveSlot("Builder", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:rail"), Count: 1})
	rail := servertest.MustState(t, "minecraft:rail", nil)
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.SetBlockInDimension(world.EndName, arrival.X+2, arrival.Y, arrival.Z, rail)
	})

	// Facing south down the platform's x axis, so the placement state is
	// the north-south one the pair has to beat.
	c.MoveTo(float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+0.5, 0, 60)
	c.PlaceBlock(arrival.X+1, arrival.Y-1, arrival.Z, protocol.FaceTop)
	c.ExpectBlockAck()

	for _, pos := range [][3]int32{{arrival.X + 1, arrival.Y, arrival.Z}, {arrival.X + 2, arrival.Y, arrival.Z}} {
		name, props := servertest.StateProps(srv.BlockInDimension(world.EndName, pos[0], pos[1], pos[2]))
		if name != "minecraft:rail" {
			t.Fatalf("block at (%d, %d, %d) of the end is %q, want a rail", pos[0], pos[1], pos[2], name)
		}
		if props["shape"] != "east_west" {
			t.Errorf("the end's rail at (%d, %d, %d) is %s, want east_west",
				pos[0], pos[1], pos[2], props["shape"])
		}
	}
}

// TestAThreeWayJunctionCurvesWithTheSignal pins the signal's place in the
// curve preference: a junction of rails south, east and west, a redstone
// block north in the empty column, lands on south-west while the signal is
// live and on south-east once the block is broken. The signal order is the
// no-signal order reversed, so with both curve pairs answering the two
// orders end on the two curves.
func TestAThreeWayJunctionCurvesWithTheSignal(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")
	railPlayer(t, srv, c, "minecraft:rail", 1)
	for _, pos := range [][3]int32{{2, 64, 4}, {3, 64, 3}, {1, 64, 3}} {
		if !srv.SetBlock(pos[0], pos[1], pos[2], servertest.MustState(t, "minecraft:rail", nil)) {
			t.Fatalf("the rail at (%d, %d, %d) did not stand", pos[0], pos[1], pos[2])
		}
	}
	if !srv.SetBlock(2, 64, 2, servertest.MustState(t, "minecraft:redstone_block", nil)) {
		t.Fatal("the redstone block's chunk is not loaded")
	}
	c.PlaceBlock(2, 63, 3, protocol.FaceTop)
	c.ExpectBlockAck()
	servertest.ExpectBlockNamed(t, srv, 2, 64, 3, "minecraft:rail",
		map[string]string{"shape": "south_west"})

	c.BreakBlock(2, 64, 2)
	c.ExpectBlockAck()
	servertest.ExpectBlockNamed(t, srv, 2, 64, 3, "minecraft:rail",
		map[string]string{"shape": "south_east"})
}

// TestACornerRailCurvesOnlyForThePlainRail pins the straight flag: with
// rails standing south and east of the spot, the plain rail lands on the
// south-east curve, while the three straight rails keep the north-south
// axis their placement chose. The player faces south.
func TestACornerRailCurvesOnlyForThePlainRail(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		rail  string
		shape string
	}{
		{"the plain rail curves", "minecraft:rail", "south_east"},
		{"a powered rail keeps its axis", "minecraft:powered_rail", "north_south"},
		{"a detector rail keeps its axis", "minecraft:detector_rail", "north_south"},
		{"an activator rail keeps its axis", "minecraft:activator_rail", "north_south"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")
			railPlayer(t, srv, c, tc.rail, 1)
			if !srv.SetBlock(3, 64, 3, servertest.MustState(t, "minecraft:rail", nil)) ||
				!srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:rail", nil)) {
				t.Fatal("the standing rails' chunks are not loaded")
			}
			c.PlaceBlock(2, 63, 3, protocol.FaceTop)
			c.ExpectBlockAck()
			servertest.ExpectBlockNamed(t, srv, 2, 64, 3, tc.rail,
				map[string]string{"shape": tc.shape})
		})
	}
}

// TestLeverPowersNinePoweredRailsOfTen lays a line of ten bare powered
// rails beside a floor lever: the bare placements connect east-west on the
// way in, and the pulled lever lights its own rail plus exactly eight more —
// the ninth along is past the walk's limit of eight. The lever off takes
// the whole lit line back down.
func TestLeverPowersNinePoweredRailsOfTen(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	for x := int32(2); x <= 11; x++ {
		servertest.Setblock(t, c, x, 64, 0, "powered_rail")
	}
	for x := int32(2); x <= 11; x++ {
		servertest.ExpectBlockNamed(t, srv, x, 64, 0, "minecraft:powered_rail",
			map[string]string{"shape": "east_west"})
	}

	servertest.Setblock(t, c, 1, 64, 0, "lever[face=floor,facing=north]")
	lit, _ := registry.FindState("minecraft:powered_rail", map[string]string{"shape": "east_west", "powered": "true"})
	unpowered, _ := registry.FindState("minecraft:powered_rail", map[string]string{"shape": "east_west", "powered": "false"})
	c.UseBlock(1, 64, 0)
	// The ninth lit rail is the last of the one wake, so waiting on it covers
	// the whole line.
	servertest.WaitForTicks(t, srv, 60, func() bool {
		return srv.BlockAt(10, 64, 0) == lit
	}, "the line's eighth rail past the lever's never lit")
	for x := int32(2); x <= 10; x++ {
		servertest.ExpectBlockNamed(t, srv, x, 64, 0, "minecraft:powered_rail",
			map[string]string{"powered": "true"})
	}
	servertest.ExpectBlockNamed(t, srv, 11, 64, 0, "minecraft:powered_rail",
		map[string]string{"powered": "false"})

	c.UseBlock(1, 64, 0)
	servertest.WaitForTicks(t, srv, 60, func() bool {
		return srv.BlockAt(10, 64, 0) == unpowered
	}, "the lit line never went back down")
	for x := int32(2); x <= 11; x++ {
		servertest.ExpectBlockNamed(t, srv, x, 64, 0, "minecraft:powered_rail",
			map[string]string{"powered": "false"})
	}
}

// TestObserverFacingAPoweredRailPulsesOnItsStateChanges points an observer
// at the first rail of a powered line and pulls the lever beside it: the
// rail's POWERED property moves on both edges, and the observer pulses on
// each. vanilla: ObserverBlock.updateShape, PoweredRailBlock.updateState.
func TestObserverFacingAPoweredRailPulsesOnItsStateChanges(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	for x := int32(2); x <= 4; x++ {
		servertest.Setblock(t, c, x, 64, 0, "powered_rail")
	}
	servertest.Setblock(t, c, 1, 64, 0, "lever[face=floor,facing=north]")
	servertest.Setblock(t, c, 2, 64, 1, "observer[facing=north]")

	// A pulse is the observer's powered=true update on the wire; each edge of
	// the rail's state sends one.
	pulse := func() bool {
		return c.PumpTicks(srv, 100, func(id int32, data []byte) bool {
			if id != protocol.IDPlayBlockUpdate {
				return false
			}
			r := protocol.NewReader(data)
			x, y, z := r.ReadPosition()
			if x != 2 || y != 64 || z != 1 {
				return false
			}
			got, props := servertest.StateProps(uint16(r.ReadVarInt()))
			return got == "minecraft:observer" && props["powered"] == "true"
		})
	}

	c.UseBlock(1, 64, 0)
	if !pulse() {
		t.Error("the observer never pulsed on the rail lighting")
	}
	servertest.AwaitTicks(t, srv, 4) // the pulse's own trailing edge first

	c.UseBlock(1, 64, 0)
	if !pulse() {
		t.Error("the observer never pulsed on the rail darkening")
	}
}

// TestActivatorRailsPowerTheirOwnLinesOnly gives the activator rail the
// same lever line the powered rail takes — its own nine of ten — and checks
// the seam: the powered family passes its power along rails of its own kind,
// so activators beyond a lit powered rail stay dark as its own lit rails
// stay past a lit activator.
func TestActivatorRailsPowerTheirOwnLinesOnly(t *testing.T) {
	t.Parallel()
	t.Run("activator line", func(t *testing.T) {
		t.Parallel()
		srv, c := servertest.AttachActor(t)

		for x := int32(1); x <= 10; x++ {
			servertest.Setblock(t, c, x, 64, 3, "activator_rail")
		}
		servertest.Setblock(t, c, 0, 64, 3, "redstone_block")
		for x := int32(1); x <= 9; x++ {
			servertest.ExpectBlockNamed(t, srv, x, 64, 3, "minecraft:activator_rail",
				map[string]string{"powered": "true"})
		}
		servertest.ExpectBlockNamed(t, srv, 10, 64, 3, "minecraft:activator_rail",
			map[string]string{"powered": "false"})
	})
	t.Run("powered seam", func(t *testing.T) {
		t.Parallel()
		srv, c := servertest.AttachActor(t)

		for x := int32(12); x <= 16; x++ {
			servertest.Setblock(t, c, x, 64, 4, "powered_rail")
		}
		servertest.Setblock(t, c, 11, 64, 4, "redstone_block")
		servertest.Setblock(t, c, 17, 64, 4, "activator_rail")
		servertest.Setblock(t, c, 18, 64, 4, "activator_rail")
		for x := int32(12); x <= 16; x++ {
			servertest.ExpectBlockNamed(t, srv, x, 64, 4, "minecraft:powered_rail",
				map[string]string{"powered": "true"})
		}
		for x := int32(17); x <= 18; x++ {
			servertest.ExpectBlockNamed(t, srv, x, 64, 4, "minecraft:activator_rail",
				map[string]string{"powered": "false"})
		}
	})
}

// TestAPoweredRailSlopeDoesNotLightTheRailOneAboveItsFlatEnd places a dip
// of two powered rails between a flat high rail and a redstone block: the
// fed rail's flat walk steps onto the dark slope one below its end and
// stops, and the slope's backward walk reaches the stone under the fed
// rail, so only the rail beside the block lights.
// vanilla: PoweredRailBlock.findPoweredRailSignal.
func TestAPoweredRailSlopeDoesNotLightTheRailOneAboveItsFlatEnd(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 10, 99, 33, "stone")
	servertest.Setblock(t, c, 11, 100, 33, "stone")
	servertest.Setblock(t, c, 9, 100, 33, "stone")
	servertest.Setblock(t, c, 11, 101, 33, "powered_rail")
	servertest.Setblock(t, c, 10, 100, 33, "powered_rail")
	servertest.Setblock(t, c, 9, 101, 33, "powered_rail")
	servertest.Setblock(t, c, 12, 101, 33, "redstone_block")

	servertest.ExpectBlockNamed(t, srv, 11, 101, 33, "minecraft:powered_rail",
		map[string]string{"shape": "east_west", "powered": "true"})
	servertest.ExpectBlockNamed(t, srv, 10, 100, 33, "minecraft:powered_rail",
		map[string]string{"shape": "ascending_west", "powered": "false"})
	servertest.ExpectBlockNamed(t, srv, 9, 101, 33, "minecraft:powered_rail",
		map[string]string{"shape": "east_west", "powered": "false"})
}

// TestBreakingTheRailASlopeRunsToTakesThePowerDown feeds a powered rail on
// a step from a redstone block and lets its power run down the slope to the
// rail that climbs to it, then breaks the top rail: the removal wakes the
// support block below the rail, whose neighbours include the lower rail, so
// it goes dark. vanilla: BaseRailBlock.affectNeighborsAfterRemoval.
func TestBreakingTheRailASlopeRunsToTakesThePowerDown(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 9, 99, 25, "stone")
	servertest.Setblock(t, c, 10, 100, 25, "stone")
	servertest.Setblock(t, c, 9, 100, 25, "powered_rail[shape=ascending_east]")
	servertest.Setblock(t, c, 10, 101, 25, "powered_rail")
	servertest.Setblock(t, c, 11, 101, 25, "redstone_block")
	servertest.ExpectBlockNamed(t, srv, 9, 100, 25, "minecraft:powered_rail", map[string]string{"shape": "ascending_east", "powered": "true"})
	servertest.ExpectBlockNamed(t, srv, 10, 101, 25, "minecraft:powered_rail", map[string]string{"powered": "true"})

	servertest.Setblock(t, c, 10, 101, 25, "air")
	servertest.ExpectBlockNamed(t, srv, 9, 100, 25, "minecraft:powered_rail", map[string]string{"shape": "ascending_east", "powered": "false"})
}

// TestAPoweredRailSlopeKeepsToItsOwnAxis checks the axis check the walk makes
// on a slope: the slope reads as its flat axis, so a rail one cell above its
// low end on the other axis does not continue the walk and the slope stays
// dark.
// vanilla: PoweredRailBlock.findPoweredRailSignal.
func TestAPoweredRailSlopeKeepsToItsOwnAxis(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 9, 64, 39, "rail")
	servertest.Setblock(t, c, 9, 64, 41, "rail")
	servertest.Setblock(t, c, 9, 64, 40, "powered_rail")
	servertest.ExpectBlockNamed(t, srv, 9, 64, 40, "minecraft:powered_rail",
		map[string]string{"shape": "north_south"})

	servertest.Setblock(t, c, 11, 64, 40, "stone")
	servertest.Setblock(t, c, 11, 65, 40, "rail")
	servertest.Setblock(t, c, 10, 64, 40, "powered_rail")
	servertest.ExpectBlockNamed(t, srv, 10, 64, 40, "minecraft:powered_rail",
		map[string]string{"shape": "ascending_east"})

	servertest.Setblock(t, c, 8, 64, 40, "redstone_block")

	servertest.ExpectBlockNamed(t, srv, 9, 64, 40, "minecraft:powered_rail",
		map[string]string{"shape": "north_south", "powered": "true"})
	servertest.ExpectBlockNamed(t, srv, 10, 64, 40, "minecraft:powered_rail",
		map[string]string{"shape": "ascending_east", "powered": "false"})
}

// TestAPoweredRailSlopeCarriesPowerToBothEnds feeds the flat low rail of a
// powered slope from the side and checks the slope and the raised high rail
// light, once for a south slope and once for a north one: the slope's own
// walk reaches the fed rail from the shape's fixed direction.
// vanilla: PoweredRailBlock.findPoweredRailSignal.
func TestAPoweredRailSlopeCarriesPowerToBothEnds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		low   [3]int32
		slope [3]int32
		high  [3]int32
		feed  [3]int32
		shape string
	}{
		{
			name:  "south slope",
			low:   [3]int32{20, 64, 40},
			slope: [3]int32{20, 64, 41},
			high:  [3]int32{20, 65, 42},
			feed:  [3]int32{19, 64, 40},
			shape: "ascending_south",
		},
		{
			name:  "north slope",
			low:   [3]int32{20, 64, 42},
			slope: [3]int32{20, 64, 41},
			high:  [3]int32{20, 65, 40},
			feed:  [3]int32{19, 64, 42},
			shape: "ascending_north",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)

			servertest.Setblock(t, c, tc.high[0], tc.high[1]-1, tc.high[2], "stone")
			servertest.Setblock(t, c, tc.low[0], tc.low[1], tc.low[2], "powered_rail")
			servertest.Setblock(t, c, tc.high[0], tc.high[1], tc.high[2], "powered_rail")
			servertest.Setblock(t, c, tc.slope[0], tc.slope[1], tc.slope[2], "powered_rail")
			servertest.ExpectBlockNamed(t, srv, tc.slope[0], tc.slope[1], tc.slope[2],
				"minecraft:powered_rail", map[string]string{"shape": tc.shape})

			servertest.Setblock(t, c, tc.feed[0], tc.feed[1], tc.feed[2], "redstone_block")

			for _, p := range [][3]int32{tc.low, tc.slope, tc.high} {
				servertest.ExpectBlockNamed(t, srv, p[0], p[1], p[2], "minecraft:powered_rail",
					map[string]string{"powered": "true"})
			}
		})
	}
}

// TestAnActivatorRailGoesWithItsSupport checks the activator rail's survival:
// it stands on a rigid block and pops when that support goes.
func TestAnActivatorRailGoesWithItsSupport(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 4, 64, 3, "stone")
	servertest.Setblock(t, c, 4, 65, 3, "activator_rail")
	servertest.ExpectBlockNamed(t, srv, 4, 65, 3, "minecraft:activator_rail", nil)

	servertest.Setblock(t, c, 4, 64, 3, "air")
	if got := srv.BlockAt(4, 65, 3); got != 0 {
		name, props, _ := registry.BlockStateName(got)
		t.Errorf("the activator rail above the removed support = %s%v, want air", name, props)
	}
}

// TestSetblockOfAnUnsupportedStraightRailLeavesAir sets each straight rail
// in open air: its landing runs neighborChanged on itself, which removes it.
// The plain rail has no such pass and stays until a neighbour changes.
func TestSetblockOfAnUnsupportedStraightRailLeavesAir(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	for i, rail := range []string{"powered_rail", "detector_rail", "activator_rail"} {
		x := int32(40 + i)
		servertest.Setblock(t, c, x, 70, 30, rail)
		if got := srv.BlockAt(x, 70, 30); got != 0 {
			name, _, _ := registry.BlockStateName(got)
			t.Errorf("%s set over nothing = %s, want air", rail, name)
		}
	}
	servertest.Setblock(t, c, 50, 70, 30, "rail")
	servertest.ExpectBlockNamed(t, srv, 50, 70, 30, "minecraft:rail", nil)
}

// TestSetblockAndFillHandNeighboursTheNewBlock pins which block a command's
// neighbour wake names: replace mode of /setblock and /fill hands the block
// that was set, destroy mode of /setblock hands the one its break removed.
// A plain rail curved by two neighbours reroutes only when a redstone block
// is the block that changed beside it.
// vanilla: ServerLevel.updateNeighboursOnBlockSet, RailBlock.updateState
func TestSetblockAndFillHandNeighboursTheNewBlock(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		prior   bool // a redstone block stands south of the junction before its rail
		command string
		want    string
	}{
		{"setblock replace sets a signal source", false, "setblock 9 64 11 redstone_block", "north_west"},
		{"fill replace sets a signal source", false, "fill 9 64 11 9 64 11 redstone_block", "north_west"},
		{"setblock replace removes a signal source", true, "setblock 9 64 11 stone", "north_west"},
		{"fill replace removes a signal source", true, "fill 9 64 11 9 64 11 stone", "north_west"},
		{"setblock destroy removes a signal source", true, "setblock 9 64 11 stone destroy", "north_east"},
		{"clone move clears the source with the old block", true, "clone 9 64 11 9 64 11 30 64 11 replace move", "north_east"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.AttachActor(t)
			if tc.prior {
				servertest.Setblock(t, c, 9, 64, 11, "redstone_block")
			}
			for _, pos := range [][3]int32{{8, 64, 10}, {10, 64, 10}, {9, 64, 9}, {9, 64, 10}} {
				servertest.Setblock(t, c, pos[0], pos[1], pos[2], "rail")
			}
			start := "north_east"
			if tc.prior {
				start = "north_west"
			}
			servertest.ExpectBlockNamed(t, srv, 9, 64, 10, "minecraft:rail",
				map[string]string{"shape": start})
			c.ChatCommand(tc.command)
			one := int64(1)
			switch {
			case strings.HasPrefix(tc.command, "clone"):
				servertest.AwaitCommandFeedbackText(t, c, "commands.clone.success", protocol.Text{Number: &one})
			case strings.HasPrefix(tc.command, "fill"):
				servertest.AwaitCommandFeedbackText(t, c, "commands.fill.success", protocol.Text{Number: &one})
			default:
				servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "9", "64", "11")
			}
			servertest.ExpectBlockNamed(t, srv, 9, 64, 10, "minecraft:rail",
				map[string]string{"shape": tc.want})
		})
	}
}
