package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestSandAtWorldBottomFallsIntoVoid checks that sand on the overworld's
// bottom row falls: the cell below the build floor reads as void air.
func TestSandAtWorldBottomFallsIntoVoid(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)

	servertest.Setblock(t, c, 0, -64, 0, "sand")
	if !servertest.ASpawnOfWithinTicks(c, srv, 5, protocol.EntityTypeFallingBlock) {
		t.Fatal("no falling_block spawned from sand at y=-64")
	}
	if got := srv.BlockAt(0, -64, 0); got != 0 {
		t.Errorf("cell at y=-64 = %d, want air", got)
	}
}
