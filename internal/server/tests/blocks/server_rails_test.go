// The rails, black-box: the shape each rail takes when it lands — the
// straight line, the curve or the slope the rails around it dictate — the
// axis its placement first writes when nothing is around, the re-link that
// turns the rails beside it, and the re-derivation a neighbouring change
// sets off; and the powered and activator rail taking redstone and passing
// it along their own lines. Minecarts are another issue's.
package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
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
