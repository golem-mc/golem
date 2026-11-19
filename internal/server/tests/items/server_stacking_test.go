package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// blockProps names the block at a position and reads its properties.
func blockProps(t *testing.T, srv *server.Server, x, y, z int32) (string, map[string]string) {
	t.Helper()
	name, raw, ok := registry.BlockStateName(srv.BlockAt(x, y, z))
	if !ok {
		t.Fatalf("block at (%d,%d,%d) has no registry name", x, y, z)
	}
	return name, registry.ParseStateProps(raw)
}

// stackingClient joins a player standing beside (5, 64, 5), the cell the
// stacking tests build in.
func stackingClient(t *testing.T, survival bool, name string) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	if survival {
		cfg = servertest.SurvivalTestConfig()
	}
	srv, c := servertest.Join(t, cfg, name)
	c.MoveToGround(4.5, 64, 4.5, true)
	c.Settle()
	return srv, c
}

// TestTwoSlabsMakeADoubleSlab checks SlabBlock.canBeReplaced: a slab clicked
// on the top of a bottom slab of its kind fills it to a double slab, one
// clicked on the side goes beside it, and another kind does not merge.
func TestTwoSlabsMakeADoubleSlab(t *testing.T) {
	t.Parallel()
	srv, c := stackingClient(t, false, "Mason")
	servertest.HoldItem(t, c, "minecraft:oak_slab")

	c.PlaceBlock(5, 63, 5, protocol.FaceTop)
	c.ExpectBlockAck()
	if name, props := blockProps(t, srv, 5, 64, 5); name != "minecraft:oak_slab" || props["type"] != "bottom" {
		t.Fatalf("first slab is %s %v, want a bottom oak slab", name, props)
	}

	// The side of a bottom slab: the slab goes in the cell beside it.
	c.PlaceBlockAt(5, 64, 5, protocol.FaceNorth, 0.25)
	c.ExpectBlockAck()
	if _, props := blockProps(t, srv, 5, 64, 5); props["type"] != "bottom" {
		t.Errorf("a side click turned the clicked slab %s, want it left bottom", props["type"])
	}
	if name, props := blockProps(t, srv, 5, 64, 4); name != "minecraft:oak_slab" || props["type"] != "bottom" {
		t.Errorf("the side click placed %s %v beside, want a bottom oak slab", name, props)
	}

	// Another kind on the top half stays out of the oak slab.
	servertest.HoldItem(t, c, "minecraft:stone_slab")
	c.PlaceBlockAt(5, 64, 4, protocol.FaceTop, 0.5)
	c.ExpectBlockAck()
	if name, props := blockProps(t, srv, 5, 64, 4); name != "minecraft:oak_slab" || props["type"] != "bottom" {
		t.Errorf("a stone slab on an oak slab made it %s %v, want it left a bottom oak slab", name, props)
	}

	// The top of the bottom slab: the two make a double slab.
	servertest.HoldItem(t, c, "minecraft:oak_slab")
	c.PlaceBlockAt(5, 64, 5, protocol.FaceTop, 0.5)
	c.ExpectBlockAck()
	if name, props := blockProps(t, srv, 5, 64, 5); name != "minecraft:oak_slab" ||
		props["type"] != "double" || props["waterlogged"] != "false" {
		t.Errorf("slab on slab is %s %v, want a dry double oak slab", name, props)
	}
	if got := srv.BlockAt(5, 65, 5); got != 0 {
		t.Errorf("the cell above the double slab holds %d, want air", got)
	}
}
