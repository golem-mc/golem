package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// withMoveChecks turns back on the move checks the harness turns off.
func withMoveChecks(s *server.Server) { s.SetMoveChecks(true) }

// joinWithMoveChecks puts a player in the flat world, standing at the spawn
// block, with the move checks on.
func joinWithMoveChecks(t *testing.T, cfg *config.Config, name string, setup ...func(*server.Server)) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, cfg, name, append([]func(*server.Server){withMoveChecks}, setup...)...)
	return srv, c
}

// expectNoTeleport reads the quiet window and fails on a Synchronize Player
// Position, the answer a refused step gets.
func expectNoTeleport(srv *server.Server, c *servertest.Client) {
	c.T.Helper()
	c.ReadTicks(srv, servertest.QuietTicks, func(id int32, _ []byte) bool {
		if id == protocol.IDPlaySyncPosition {
			c.T.Fatalf("step sent back to (%v, %v, %v), want it taken",
				c.LastTeleport.X, c.LastTeleport.Y, c.LastTeleport.Z)
		}
		return false
	})
}

func TestAMoveTooFarIsSentBack(t *testing.T) {
	_, c := joinWithMoveChecks(t, servertest.SurvivalTestConfig(), "Bolt")
	from := c.Pos()
	c.MoveTo(from.X+50, from.Y, from.Z, 90, 10)
	got := servertest.ExpectTeleport(t, c)
	if got.X != from.X || got.Y != from.Y || got.Z != from.Z {
		t.Fatalf("sent back to (%v, %v, %v), want where the body stood (%v, %v, %v)",
			got.X, got.Y, got.Z, from.X, from.Y, from.Z)
	}
	// The look is the body's own, not the refused packet's.
	if got.Yaw != from.Yaw || got.Pitch != from.Pitch {
		t.Fatalf("sent back facing %v/%v, want %v/%v", got.Yaw, got.Pitch, from.Yaw, from.Pitch)
	}
}

func TestAMoveWithinTheBudgetIsTaken(t *testing.T) {
	srv, c := joinWithMoveChecks(t, servertest.SurvivalTestConfig(), "Stroll")
	from := c.Pos()
	c.MoveTo(from.X+6, from.Y, from.Z+6, 0, 0)
	expectNoTeleport(srv, c)
}

func TestAMoveIntoAWallIsSentBack(t *testing.T) {
	srv, c := joinWithMoveChecks(t, servertest.SurvivalTestConfig(), "Bump")
	from := c.Pos()
	srv.SetBlock(int32(from.X)+2, int32(from.Y), int32(from.Z), registry.MustState("minecraft:stone", nil))
	srv.SetBlock(int32(from.X)+2, int32(from.Y)+1, int32(from.Z), registry.MustState("minecraft:stone", nil))
	c.MoveTo(from.X+2, from.Y, from.Z, 0, 0)
	got := servertest.ExpectTeleport(t, c)
	if got.X != from.X {
		t.Fatalf("sent back to x=%v, want %v", got.X, from.X)
	}
}

// halfScale joins a player with the move checks on and sets their
// minecraft:scale to 0.5, a 0.3x0.9 body.
func halfScale(t *testing.T, name string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := joinWithMoveChecks(t, servertest.SurvivalTestConfig(), name, servertest.OpsListing(t, name))
	c.ChatCommand("attribute @s minecraft:scale base set 0.5")
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
	return srv, c
}

func TestAStepAlongTheGroundIsTaken(t *testing.T) {
	// The feet resting on the stone's top cross nothing new.
	srv, c := joinWithMoveChecks(t, servertest.SurvivalTestConfig(), "Walker")
	from := c.Pos()
	c.MoveTo(from.X+0.3, from.Y, from.Z+0.3, 0, 0)
	expectNoTeleport(srv, c)
}

func TestASpectatorMovesThroughTheGround(t *testing.T) {
	cfg := servertest.SurvivalTestConfig()
	cfg.GameMode = "spectator"
	srv, c := joinWithMoveChecks(t, cfg, "Ghost")
	from := c.Pos()
	c.MoveTo(from.X, from.Y-3, from.Z, 0, 0)
	expectNoTeleport(srv, c)
}

// afterTick waits for a tick boundary, so the packets sent next fall into one
// tick's budget.
func afterTick(t *testing.T, srv *server.Server) {
	t.Helper()
	since := srv.TicksRun()
	servertest.WaitFor(t, srv, 600, func() bool { return srv.TicksRun() > since })
}
