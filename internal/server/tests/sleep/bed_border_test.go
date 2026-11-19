package sleep

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: DismountHelper.findSafeDismountLocation
func TestLeavingBedSkipsAnExitWhoseBoxCrossesTheBorder(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.NoTimeSync)
	srv.StopTick()
	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)
	// The west candidates' centres (x=4.5) are inside, but their standing
	// boxes start at x=4.2. The fourth candidate behind the foot fits.
	srv.SetOverworldBorder(9.3, 0, 10)
	servertest.HoldTicks(t, srv)
	c.Settle()
	c.LeaveBed()
	pos := servertest.ReadSyncPosition(t, c.ExpectWithinTicks(srv, 1, protocol.IDPlaySyncPosition))
	if pos.X != 5.5 || pos.Y != 64 || pos.Z != -0.5 || pos.Yaw != 0 {
		t.Errorf("wake = %+v, want (5.5,64,-0.5) yaw 0", pos)
	}
	c.Settle()
	if srv.Sleeping("Actor") {
		t.Error("player remained asleep after leaving the bed")
	}
}
