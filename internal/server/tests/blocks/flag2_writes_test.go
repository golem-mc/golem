package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestASpongeTurningWetDoesNotWakeItsNeighbours: the wet sponge is a flag-2
// write, so a quasi-powered piston beside it hears no neighborChanged and
// stays retracted until something else wakes it.
// vanilla: SpongeBlock.tryAbsorbWater
func TestASpongeTurningWetDoesNotWakeItsNeighbours(t *testing.T) {
	t.Parallel()
	wet := registry.MustState("minecraft:wet_sponge", nil)
	piston := servertest.BlockStateWith(t, "minecraft:piston", "extended=false,facing=north")
	extended := servertest.BlockStateWith(t, "minecraft:piston", "extended=true,facing=north")

	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	c.Settle()

	// The piston at (2,64,3) is quasi-powered by the redstone block two above
	// it; only the sponge at (3,64,3) touches it.
	srv.SetBlock(2, 64, 3, piston)
	srv.SetBlock(2, 66, 3, registry.MustState("minecraft:redstone_block", nil))
	srv.SetBlock(3, 64, 3, registry.MustState("minecraft:sponge", nil))
	servertest.Setblock(t, c, 4, 64, 3, "water")
	servertest.WaitForBlock(t, srv, 3, 64, 3, func(s uint16) bool { return s == wet })
	servertest.RunTicks(t, srv, 4)
	if s := srv.BlockAt(2, 64, 3); s != piston {
		t.Fatalf("piston beside the wet sponge = %d, want it still retracted %d", s, piston)
	}

	// Waking the piston from its other side shows it was quasi-powered.
	servertest.Setblock(t, c, 1, 64, 3, "stone")
	servertest.WaitForBlock(t, srv, 2, 64, 3, func(s uint16) bool { return s == extended })
}
