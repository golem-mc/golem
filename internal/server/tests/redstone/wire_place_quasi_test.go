package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestWirePlacedBelowQuasiPowerRetractsPiston replaces a power source with dust
// above a conductor: the piston beside the conductor is a neighbour of the cell
// below the dust and must hear the placement.
// vanilla: RedStoneWireBlock.onPlace
func TestWirePlacedBelowQuasiPowerRetractsPiston(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))
	servertest.HoldTicks(t, srv)

	servertest.Setblock(t, actor, 1, 64, 0, "stone")
	servertest.Setblock(t, actor, 1, 65, 0, "redstone_block")
	servertest.Setblock(t, actor, 0, 64, 0, "piston[facing=north]")
	servertest.AwaitTicks(t, srv, 3)
	actor.Settle()
	if _, props := servertest.StateProps(srv.BlockAt(0, 64, 0)); props["extended"] != "true" {
		t.Fatalf("piston %v, want extended before the dust", props)
	}

	servertest.Setblock(t, actor, 1, 65, 0, "redstone_wire")
	servertest.AwaitTicks(t, srv, 4)
	actor.Settle()
	name, props := servertest.StateProps(srv.BlockAt(0, 64, 0))
	if name != "minecraft:piston" || props["extended"] == "true" {
		t.Errorf("piston is %s %v, want retracted", name, props)
	}
}
