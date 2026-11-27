package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestLeafTickBookedBeforePistonEventRunsFirst pushes a log from under a
// persistent leaf whose distance tick was booked before the piston's event:
// the tick runs ahead of the event in its game tick, so the leaf still sees the
// log and keeps distance 1 for one more tick.
// vanilla: ServerLevel.tick (block ticks, then runBlockEvents), LeavesBlock.tick
func TestLeafTickBookedBeforePistonEventRunsFirst(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))
	servertest.HoldTicks(t, srv)

	servertest.Setblock(t, actor, 1, 64, 0, "piston[facing=up]")
	servertest.Setblock(t, actor, 1, 65, 0, "oak_log")
	servertest.Setblock(t, actor, 2, 65, 0, "oak_leaves[persistent=true,distance=1]")
	servertest.Setblock(t, actor, 2, 66, 0, "oak_leaves[persistent=true,distance=2]")
	servertest.Setblock(t, actor, 3, 66, 0, "oak_leaves[persistent=true,distance=3]")
	servertest.Setblock(t, actor, 1, 63, 0, "redstone_block")

	distance := func(y int32) string {
		_, props := servertest.StateProps(srv.BlockAt(2, y, 0))
		return props["distance"]
	}
	// Distances after each tick from a vanilla 26.3 server.
	want := [][2]string{{"1", "2"}, {"3", "2"}, {"3", "4"}, {"5", "1"}, {"2", "1"}}
	for i, w := range want {
		servertest.RunTicks(t, srv, 1)
		if got := [2]string{distance(65), distance(66)}; got != w {
			t.Errorf("tick %d: distances %v, want %v", i+1, got, w)
		}
	}
}
