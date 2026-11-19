package redstone

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// firstPoweredTick runs held ticks one at a time and returns the first whose
// end leaves the block at (x, y, z) named name with powered=true, or 0 when
// none of the limit does.
func firstPoweredTick(t *testing.T, srv *server.Server, x, y, z int32, name string, limit int) int {
	t.Helper()
	for tick := 1; tick <= limit; tick++ {
		servertest.RunTicks(t, srv, 1)
		gotName, props := servertest.StateProps(srv.BlockAt(x, y, z))
		if gotName == name && props["powered"] == "true" {
			return tick
		}
	}
	return 0
}

// touchTimingRig joins an operator on a held loop; the scene is laid with
// commands from the server's console.
func touchTimingRig(t *testing.T) *server.Server {
	t.Helper()
	srv, c := servertest.AttachActor(t)
	c.Settle()
	servertest.HoldTicks(t, srv)
	return srv
}

// TestTouchTimingDetectorRailPressesInTheCartsMoveTick: a cart 3.5 blocks before the
// rail powers it on the tenth tick, the tick the cart's move reaches it.
// vanilla: Entity.applyEffectsFromBlocks, DetectorRailBlock.entityInside
func TestTouchTimingDetectorRailPressesInTheCartsMoveTick(t *testing.T) {
	t.Parallel()
	srv := touchTimingRig(t)
	const z = 20
	for x := 10; x < 20; x++ {
		srv.RunLine("setblock " + strconv.Itoa(x) + " 64 " + strconv.Itoa(z) + " rail[shape=east_west]")
	}
	srv.RunLine("setblock 16 64 20 detector_rail[shape=east_west]")
	srv.RunLine("summon minecart 12.5 64 20.5 {Motion:[0.4d,0d,0d]}")
	if got := firstPoweredTick(t, srv, 16, 64, z, "minecraft:detector_rail", 30); got != 10 {
		t.Errorf("detector rail first powered after tick %d, want 10", got)
	}
}

// TestTouchTimingWoodenButtonPressedByArrowInTheArrowsTick: an arrow falling onto a
// button presses it on the first tick.
// vanilla: Entity.applyEffectsFromBlocks, ButtonBlock.entityInside
func TestTouchTimingWoodenButtonPressedByArrowInTheArrowsTick(t *testing.T) {
	t.Parallel()
	srv := touchTimingRig(t)
	srv.RunLine("setblock 14 64 20 oak_button[face=floor]")
	srv.RunLine("summon arrow 14.5 64.5 20.5 {Motion:[0d,-0.5d,0d]}")
	if got := firstPoweredTick(t, srv, 14, 64, 20, "minecraft:oak_button", 10); got != 1 {
		t.Errorf("button first powered after tick %d, want 1", got)
	}
}
