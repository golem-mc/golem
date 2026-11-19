package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: DismountHelper.findSafeDismountLocation
func TestBedRespawnSkipsAnExitWhoseBoxCrossesTheBorder(t *testing.T) {
	t.Parallel()
	srv, c, entityID := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Sleeper", servertest.OpsListing(t, "Sleeper"))
	servertest.HoldTicks(t, srv)
	servertest.StandABedAndSetTheSpawn(t, srv, c)
	// Reject all three west candidates even though their centres are inside.
	srv.SetOverworldBorder(9.3, 0, 10)
	pos, noBlock := servertest.KillAndRespawn(t, c, entityID, "Sleeper")
	if noBlock {
		t.Error("a bed with an exit inside the border was reported as missing")
	}
	if pos.X != 5.5 || pos.Y != 64 || pos.Z != -0.5 || pos.Yaw != 0 || pos.Pitch != 0 {
		t.Errorf("respawn = %+v, want (5.5,64,-0.5) yaw 0 pitch 0", pos)
	}
}
