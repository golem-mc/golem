package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The team friendlyFire option: a teammate's blow lands only while it is on,
// and a sword sweep passes over a teammate whatever it says.

// friendlyFireServer is a combat server with the minimum view, since every
// body in these tests stands in one chunk.
func friendlyFireServer(t *testing.T) (*server.Server, func() *servertest.Client) {
	t.Helper()
	cfg := servertest.CombatTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, connect := servertest.Start(t, cfg, func(*server.Server) {})
	return srv, func() *servertest.Client {
		c := connect()
		c.ViewAsk = config.MinDistance
		return c
	}
}

// TestTeamFriendlyFireGatesAPunch punches a teammate with friendlyFire off,
// then on.
func TestTeamFriendlyFireGatesAPunch(t *testing.T) {
	t.Parallel()
	srv, connect := friendlyFireServer(t)
	fencer := connect()
	fencer.JoinAs("Fencer")
	fencer.MoveToGround(2.5, 64, 5.5, true)
	victim := connect()
	victimID := victim.JoinAs("Victim")
	victim.MoveToGround(4.5, 64, 5.5, true)
	fencer.Settle()
	victim.Settle()
	servertest.HoldTicks(t, srv)

	for _, line := range []string{"team add red", "team join red Fencer", "team join red Victim",
		"team modify red friendlyFire false"} {
		srv.RunLine(line)
	}
	servertest.RunTicks(t, srv, servertest.SwingRecovered)
	fencer.Attack(victimID)
	fencer.Settle()
	if got := srv.PlayerHealth("Victim"); math.Abs(got-20) > 0.01 {
		t.Fatalf("health after a teammate's punch = %v of twenty, want no hurt", got)
	}

	srv.RunLine("team modify red friendlyFire true")
	servertest.RunTicks(t, srv, servertest.SwingRecovered)
	fencer.Attack(victimID)
	fencer.Settle()
	if got := srv.PlayerHealth("Victim"); got >= 20 {
		t.Errorf("health after the punch with friendly fire on = %v, want a hurt", got)
	}
}

// TestTeamFriendlyFireProtectsFromOwnTNT blasts a player with their own TNT
// while their team has friendlyFire off: the attacker is the victim, whom
// the team still shields.
// vanilla: Player.canHarmPlayer
func TestTeamFriendlyFireProtectsFromOwnTNT(t *testing.T) {
	t.Parallel()
	srv, connect := friendlyFireServer(t)
	lighter := connect()
	lighter.JoinAs("Lighter")
	lighter.MoveToGround(2.5, 64, 5.5, true)
	lighter.Settle()
	servertest.HoldTicks(t, srv)
	for _, line := range []string{"team add red", "team join red Lighter", "team modify red friendlyFire false"} {
		srv.RunLine(line)
	}

	srv.SpawnPlayerTNT("Lighter", 2.5, 64, 5.5, 1)
	servertest.RunTicks(t, srv, 3)
	if got := srv.PlayerHealth("Lighter"); math.Abs(got-20) > 0.01 {
		t.Errorf("health after one's own TNT = %v of twenty, want no hurt", got)
	}

	srv.RunLine("team modify red friendlyFire true")
	srv.SpawnPlayerTNT("Lighter", 2.5, 64, 5.5, 1)
	servertest.RunTicks(t, srv, 3)
	if got := srv.PlayerHealth("Lighter"); got >= 20 {
		t.Errorf("health after one's own TNT with friendly fire on = %v, want a hurt", got)
	}
}
