package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The two-tall plants: a placement stands both halves, and however the
// plant comes apart — either half broken, the ground taken from under it —
// both halves go and exactly one item drops.

// plantState looks up a half of a two-tall plant.
func plantState(t *testing.T, name, half string) uint16 {
	t.Helper()
	id, ok := registry.FindState(name, map[string]string{"half": half})
	if !ok {
		t.Fatalf("no %s state with half=%s", name, half)
	}
	return id
}

// grassUnder lays a grass block at (x, y-1, z): the flat world's top is
// stone, which grows no flowers.
func grassUnder(t *testing.T, srv *server.Server, x, y, z int32) {
	t.Helper()
	grass, _ := registry.FindState("minecraft:grass_block", nil)
	if !srv.SetBlock(x, y-1, z, grass) {
		t.Fatal("could not lay the grass")
	}
}

// standPlant stands both halves of a plant at (x, y, z) and (x, y+1, z), on
// grass.
func standPlant(t *testing.T, srv *server.Server, name string, x, y, z int32) {
	t.Helper()
	grassUnder(t, srv, x, y, z)
	if !srv.SetBlock(x, y, z, plantState(t, name, "lower")) ||
		!srv.SetBlock(x, y+1, z, plantState(t, name, "upper")) {
		t.Fatalf("could not stand the %s", name)
	}
}

// expectAir checks that every listed cell is air.
func expectAir(t *testing.T, srv *server.Server, cells ...[3]int32) {
	t.Helper()
	for _, p := range cells {
		if state := srv.BlockAt(p[0], p[1], p[2]); state != 0 {
			name, props := servertest.StateProps(state)
			t.Errorf("(%d,%d,%d) holds %s %v, want air", p[0], p[1], p[2], name, props)
		}
	}
}

// TestPlacingAPeonyStandsBothHalves places a peony on the ground and checks
// the upper half stands above the lower one.
func TestPlacingAPeonyStandsBothHalves(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	grassUnder(t, srv, 5, 64, 0)
	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:peony", 1))
	c.FacePlayer(0.5, 64, 0.5, 0, 0)
	c.PlaceBlock(5, 63, 0, protocol.FaceTop)
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:peony", map[string]string{"half": "lower"})
	servertest.ExpectBlockNamed(t, srv, 5, 65, 0, "minecraft:peony", map[string]string{"half": "upper"})
}

// TestPeonyPlacementIsRefusedUnderACeiling puts stone where the upper half
// would stand and checks nothing is placed.
func TestPeonyPlacementIsRefusedUnderACeiling(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	stone, _ := registry.FindState("minecraft:stone", nil)
	if !srv.SetBlock(5, 65, 0, stone) {
		t.Fatal("could not place the ceiling")
	}
	grassUnder(t, srv, 5, 64, 0)
	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:peony", 1))
	c.FacePlayer(0.5, 64, 0.5, 0, 0)
	c.PlaceBlock(5, 63, 0, protocol.FaceTop)
	c.Settle()

	expectAir(t, srv, [3]int32{5, 64, 0})
}

// TestPlacingADoorFacesTheLookDirection places a door looking south: both
// halves stand, facing the way the player looks.
func TestPlacingADoorFacesTheLookDirection(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:oak_door", 1))
	c.FacePlayer(0.5, 64, 0.5, 0, 0)
	c.PlaceBlock(5, 63, 0, protocol.FaceTop)
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:oak_door", map[string]string{"half": "lower", "facing": "south"})
	servertest.ExpectBlockNamed(t, srv, 5, 65, 0, "minecraft:oak_door", map[string]string{"half": "upper", "facing": "south"})
}

// TestPlacingADoorHingesTowardAWall places a door looking south beside a wall
// two blocks tall: the hinge goes toward the wall, the way the door opens flat
// against it — a wall on the player's left a left hinge, one on the right a
// right hinge.
func TestPlacingADoorHingesTowardAWall(t *testing.T) {
	for _, tc := range []struct {
		name      string
		wallX     int32
		wantHinge string
	}{
		{"wall on the player's left", 6, "left"},
		{"wall on the player's right", 4, "right"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

			stone, _ := registry.FindState("minecraft:stone", nil)
			if !srv.SetBlock(tc.wallX, 64, 0, stone) || !srv.SetBlock(tc.wallX, 65, 0, stone) {
				t.Fatal("could not build the wall")
			}
			c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:oak_door", 1))
			c.FacePlayer(0.5, 64, 0.5, 0, 0)
			c.PlaceBlock(5, 63, 0, protocol.FaceTop)
			c.Settle()

			servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:oak_door", map[string]string{"half": "lower", "facing": "south", "hinge": tc.wantHinge})
			servertest.ExpectBlockNamed(t, srv, 5, 65, 0, "minecraft:oak_door", map[string]string{"half": "upper", "facing": "south", "hinge": tc.wantHinge})
		})
	}
}

// TestPlacingTwoDoorsMakesADoubleDoor places a door and then a second beside
// it on the player's right, the look south both times: the second door's
// hinge mirrors the first's, the free edges meeting in the middle.
func TestPlacingTwoDoorsMakesADoubleDoor(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:oak_door", 1))
	c.FacePlayer(0.5, 64, 0.5, 0, 0)
	c.PlaceBlock(5, 63, 0, protocol.FaceTop)
	c.Settle()
	c.PlaceBlock(4, 63, 0, protocol.FaceTop)
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:oak_door", map[string]string{"half": "lower", "hinge": "left"})
	servertest.ExpectBlockNamed(t, srv, 4, 64, 0, "minecraft:oak_door", map[string]string{"half": "lower", "hinge": "right"})
}

// TestPlacingADoorByTheClickHalfHingesThere places a door looking south with
// the crosshair on one half of the ground's face: the player's right half —
// the west one of a south look — a right hinge, the left half a left one.
func TestPlacingADoorByTheClickHalfHingesThere(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cursorX   float32
		wantHinge string
	}{
		{"crosshair on the right half", 0.2, "right"},
		{"crosshair on the left half", 0.8, "left"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

			c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:oak_door", 1))
			c.FacePlayer(0.5, 64, 0.5, 0, 0)
			c.PlaceBlockAtCursor(5, 63, 0, protocol.FaceTop, tc.cursorX, 0.5, 0.5)
			c.Settle()

			servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:oak_door", map[string]string{"half": "lower", "facing": "south", "hinge": tc.wantHinge})
		})
	}
}

// TestSurvivalBreakingEitherPeonyHalfDropsOnePeony breaks the upper half of
// one peony and the lower half of another: each plant goes whole, and each
// leaves one peony.
func TestSurvivalBreakingEitherPeonyHalfDropsOnePeony(t *testing.T) {
	for _, tc := range []struct {
		name  string
		broke int32
	}{{"upper", 65}, {"lower", 64}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Actor")

			standPlant(t, srv, "minecraft:peony", 5, 64, 0)
			c.BreakBlock(5, tc.broke, 0)
			c.Settle()

			expectAir(t, srv, [3]int32{5, 64, 0}, [3]int32{5, 65, 0})
			if it := servertest.ExpectOneDroppedItem(t, srv, "minecraft:peony"); it.Count != 1 {
				t.Fatalf("the peony dropped %d items, want 1", it.Count)
			}
		})
	}
}

// TestCreativeBreakingPeonyTopLeavesNothing breaks the upper half in
// creative: the lower half goes with it and nothing drops.
func TestCreativeBreakingPeonyTopLeavesNothing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	standPlant(t, srv, "minecraft:peony", 5, 64, 0)
	c.BreakBlock(5, 65, 0)
	c.Settle()

	expectAir(t, srv, [3]int32{5, 64, 0}, [3]int32{5, 65, 0})
	servertest.ExpectNoDroppedItems(t, srv)
}

// TestPeonyPopsWhenItsGroundGoes breaks the grass under a peony: the whole
// plant comes down and drops its one item.
func TestPeonyPopsWhenItsGroundGoes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	standPlant(t, srv, "minecraft:peony", 5, 64, 0)
	c.BreakBlock(5, 63, 0)
	c.Settle()

	expectAir(t, srv, [3]int32{5, 64, 0}, [3]int32{5, 65, 0})
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:peony")
}

// TestShearingTallGrassDropsTwoShortGrass breaks the upper half of tall
// grass with shears: the table's two short grass, once, from the half that
// still had its partner.
func TestShearingTallGrassDropsTwoShortGrass(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Actor")

	srv.GiveSlot("Actor", player.HotbarSlot, servertest.ItemStack(t, "minecraft:shears", 1))
	standPlant(t, srv, "minecraft:tall_grass", 5, 64, 0)
	c.BreakBlock(5, 65, 0)
	c.Settle()

	expectAir(t, srv, [3]int32{5, 64, 0}, [3]int32{5, 65, 0})
	if it := servertest.ExpectOneDroppedItem(t, srv, "minecraft:short_grass"); it.Count != 2 {
		t.Fatalf("the tall grass dropped %d short grass, want 2", it.Count)
	}
}

// TestCreativeBreakingTallSeagrassTopLeavesWater breaks the upper half of a
// tall seagrass: both cells are left to the water the plant stood in.
func TestCreativeBreakingTallSeagrassTopLeavesWater(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	standPlant(t, srv, "minecraft:tall_seagrass", 5, 64, 0)
	c.BreakBlock(5, 65, 0)
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 0, "minecraft:water", map[string]string{"level": "0"})
	servertest.ExpectBlockNamed(t, srv, 5, 65, 0, "minecraft:water", map[string]string{"level": "0"})
	servertest.ExpectNoDroppedItems(t, srv)
}

// TestIronDoorUpperBrokenDropsOne breaks an iron door's upper half in
// survival, by hand and then with a pickaxe: an iron door asks for no tool,
// so either break takes the lower half with it and one door drops.
func TestIronDoorUpperBrokenDropsOne(t *testing.T) {
	for _, tc := range []struct {
		name string
		tool string
	}{{"hand", ""}, {"pickaxe", "minecraft:iron_pickaxe"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// The step clock: a bare hand is five hundred ticks to an
			// iron door, too long a wait for the wall's.
			clock := servertest.NewStepClock()
			srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Actor", func(s *server.Server) {
				s.SetTickClock(clock, nil)
				s.SetKeepAlive(time.Hour, time.Hour)
			})

			if tc.tool != "" {
				srv.GiveSlot("Actor", player.HotbarSlot, servertest.ItemStack(t, tc.tool, 1))
			}
			// On the ground, and clear of the door: the dig's pace counts
			// the standing, and the door's own drop is not to land within
			// the player's pickup of it.
			c.MoveToGround(0.5, 64, 0.5, true)
			for half, y := range map[string]int32{"lower": 64, "upper": 65} {
				door, _ := registry.FindState("minecraft:iron_door", map[string]string{"half": half, "facing": "south"})
				if !srv.SetBlock(5, y, 0, door) {
					t.Fatal("could not stand the door")
				}
			}
			c.BreakBlock(5, 65, 0)
			c.Settle()
			servertest.StepTicks(t, srv, clock, 510)
			c.Settle()

			expectAir(t, srv, [3]int32{5, 64, 0}, [3]int32{5, 65, 0})
			servertest.ExpectOneDroppedItem(t, srv, "minecraft:iron_door")
		})
	}
}
