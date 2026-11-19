package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestEndPortalIgnoresSpectators checks a spectator standing in an end portal
// starts no travel, while a survival player in the same block does.
// vanilla: Player.tick noPhysics
func TestEndPortalIgnoresSpectators(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		mode int32
		want bool
	}{
		{"survival", protocol.GameModeSurvival, true},
		{"spectator", protocol.GameModeSpectator, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Ghost", servertest.OpsListing(t, "Ghost"))
			if tc.mode != protocol.GameModeSurvival {
				c.SendChangeGameMode(tc.mode)
				servertest.WaitFor(t, srv, 200, func() bool { return srv.PlayerGameMode("Ghost") == tc.mode })
			}
			srv.StopTick()
			if !srv.SetBlock(10, 63, 10, srv.EndPortalState()) {
				t.Fatal("placing the end portal block failed")
			}
			c.MoveTo(10.5, 63, 10.5, 0, 0)
			c.Settle()

			srv.TickPortals()
			if got := srv.PortalCooldown("Ghost") > 0; got != tc.want {
				t.Errorf("travel started = %v, want %v", got, tc.want)
			}
		})
	}
}
