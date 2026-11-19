package movement

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestSandFallsWhenItsSupportIsBroken checks that the physics engine is wired
// into the server: breaking the block under a sand block makes the sand fall
// on the main loop, in the world and on the wire.
func TestSandFallsWhenItsSupportIsBroken(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")

	// A stone pillar block at (5, 64, 0) on the ground, sand on top of it.
	const x, z = int32(5), int32(0)
	stoneItem, _ := registry.ItemID("minecraft:stone")
	sandItem, _ := registry.ItemID("minecraft:sand")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stoneItem, Count: 1})
	c.PlaceBlock(x, 63, z, protocol.FaceTop)
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: sandItem, Count: 1})
	c.PlaceBlock(x, 64, z, protocol.FaceTop)

	sand := waitForBlock(t, srv, x, 65, z, func(s uint16) bool { return s != 0 })

	// Taking the stone away leaves the sand over air, and it falls into the
	// gap the stone left.
	c.BreakBlock(x, 64, z)
	waitForBlock(t, srv, x, 64, z, func(s uint16) bool { return s == sand })
	if got := srv.BlockAt(x, 65, z); got != 0 {
		t.Errorf("block the sand fell from = %d, want 0 (air)", got)
	}
}

// waitForBlock runs ticks until the block at the position satisfies ok,
// failing once a few seconds' worth have passed. Physics runs on the main
// loop, so a change it makes lands some ticks after whatever caused it.
func waitForBlock(t *testing.T, srv *server.Server, x, y, z int32, ok func(uint16) bool) uint16 {
	t.Helper()
	servertest.UntilTicks(t, srv, 100, fmt.Sprintf("block at (%d, %d, %d)", x, y, z),
		func() bool { return ok(srv.BlockAt(x, y, z)) })
	return srv.BlockAt(x, y, z)
}
