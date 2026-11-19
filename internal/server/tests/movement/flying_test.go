package movement

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func disallowFlight(s *server.Server) { s.SetAllowFlight(false) }

var flyingKick = protocol.Text{Translate: "multiplayer.disconnect.flying"}

// expectNoKick runs n ticks and fails on a Disconnect among them.
func expectNoKick(c *servertest.Client, srv *server.Server, n int) {
	c.T.Helper()
	c.ExpectNoneWithinTicks(srv, n, servertest.PacketIs(protocol.IDPlayDisconnect))
}

// joinHeld joins with allow-flight off and the loop held.
func joinHeld(t *testing.T, cfg *config.Config, name string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := joinWithMoveChecks(t, cfg, name, disallowFlight)
	servertest.HoldTicks(t, srv)
	return srv, c
}

// hover reports one step 5 blocks above the spawn block.
func hover(c *servertest.Client) {
	c.T.Helper()
	c.MoveToGrounded(c.Pos().X, c.Pos().Y+5, c.Pos().Z, 0, 0, false)
}

func TestAHoveringSurvivalPlayerIsKickedAfterEightyTicks(t *testing.T) {
	srv, c := joinHeld(t, servertest.SurvivalTestConfig(), "Hover")
	hover(c)
	expectNoKick(c, srv, 79)
	servertest.RunTicks(t, srv, 4)
	servertest.ExpectDisconnect(t, c, flyingKick)
}

func TestAHoverShortOfTheLimitIsNotKicked(t *testing.T) {
	srv, c := joinHeld(t, servertest.SurvivalTestConfig(), "Brief")
	ground := c.Pos().Y
	hover(c)
	expectNoKick(c, srv, 60)
	// A landing step resets the count.
	c.MoveToGrounded(c.Pos().X, ground, c.Pos().Z, 0, 0, true)
	expectNoKick(c, srv, 60)
}

func TestCreativeAndSpectatorPlayersMayHover(t *testing.T) {
	for _, mode := range []string{"creative", "spectator"} {
		t.Run(mode, func(t *testing.T) {
			cfg := servertest.SurvivalTestConfig()
			cfg.GameMode = mode
			srv, c := joinHeld(t, cfg, "Mode")
			hover(c)
			expectNoKick(c, srv, 100)
		})
	}
}

// A passenger is never floating, whatever its seat hangs over.
// vanilla: ServerGamePacketListenerImpl.tickPlayer
func TestAPlayerRidingAHorseIsNotKickedForFloating(t *testing.T) {
	srv, c := joinHeld(t, servertest.SurvivalTestConfig(), "Horseman")
	x, y, z := int(math.Floor(c.Pos().X)), int(math.Floor(c.Pos().Y)), int(math.Floor(c.Pos().Z))
	srv.RunLine(fmt.Sprintf("setblock %d %d %d minecraft:sand", x, y-1, z))
	srv.RunLine(fmt.Sprintf("summon minecraft:horse %d.5 %d %d.5 {NoAI:1b,Tame:1b,equipment:{saddle:{id:\"minecraft:saddle\",count:1}}}", x, y, z))
	horse := c.AwaitSpawn(protocol.EntityTypeHorse)
	srv.RunLine("ride Horseman mount @e[type=horse,limit=1]")
	if got := servertest.AwaitPassengersFor(c, horse.EntityID); len(got) != 1 {
		t.Fatalf("horse passengers = %v, want the player", got)
	}
	for i := range 120 {
		c.SendRotation(float32(i), 0, false)
		expectNoKick(c, srv, 1)
	}
}
