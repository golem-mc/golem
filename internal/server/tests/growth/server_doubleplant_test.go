package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

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
