package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
)

// TestAnArrowStaysStuckInThePlayerItHitsThenFallsOut checks the arrow count
// a landed arrow adds to a player's metadata and the 20 * (30 - count) ticks
// after which it goes back to nought.
func TestAnArrowStaysStuckInThePlayerItHitsThenFallsOut(t *testing.T) {
	t.Parallel()
	srv, c, victimID := servertest.JoinAs(t, servertest.CombatTestConfig(), "Victim")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	srv.ShootArrow(4.5, 65, 0.5, -2, 0, 0)
	if got := servertest.StuckArrows(t, c, srv, victimID, 40); len(got) != 1 || got[0] != 1 {
		t.Fatalf("arrow counts told after the hit = %v, want [1]", got)
	}

	srv.StopTick()
	c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
	// The countdown of 580 ticks began at the hit; the clock has run a few
	// dozen of them already, so 400 more leave the arrow in and 200 more
	// let it fall.
	for range 400 {
		srv.TickSurvival()
	}
	if got := servertest.StuckArrows(t, c, srv, victimID, 0); len(got) != 0 {
		t.Fatalf("arrow counts told before the arrow's time = %v, want none", got)
	}
	for range 200 {
		srv.TickSurvival()
	}
	if got := servertest.StuckArrows(t, c, srv, victimID, 0); len(got) != 1 || got[0] != 0 {
		t.Fatalf("arrow counts told when the arrow fell out = %v, want [0]", got)
	}
}
