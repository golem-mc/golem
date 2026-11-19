package movement

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

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

func TestTheMovementCheckRuleLetsAFarMoveThrough(t *testing.T) {
	srv, c := joinWithMoveChecks(t, servertest.SurvivalTestConfig(), "Blink", func(s *server.Server) {
		s.SetGameRule("player_movement_check", false)
	})
	from := c.Pos()
	c.MoveTo(from.X+12, from.Y, from.Z, 0, 0)
	expectNoTeleport(srv, c)
}

func TestAMoveIntoTheGroundIsSentBack(t *testing.T) {
	for _, mode := range []string{"survival", "creative"} {
		t.Run(mode, func(t *testing.T) {
			cfg := servertest.SurvivalTestConfig()
			cfg.GameMode = mode
			_, c := joinWithMoveChecks(t, cfg, "Mole")
			from := c.Pos()
			// The flat world's stone stands up to y=63; feet at 63.5 are in it.
			c.MoveTo(from.X+1, from.Y-0.5, from.Z, 45, 20)
			got := servertest.ExpectTeleport(t, c)
			if got.X != from.X || got.Y != from.Y || got.Z != from.Z {
				t.Fatalf("sent back to (%v, %v, %v), want where the step began (%v, %v, %v)",
					got.X, got.Y, got.Z, from.X, from.Y, from.Z)
			}
			// The look is the refused packet's.
			if got.Yaw != 45 || got.Pitch != 20 {
				t.Fatalf("sent back facing %v/%v, want the packet's 45/20", got.Yaw, got.Pitch)
			}
		})
	}
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

func TestAHalfScalePlayerWalksUnderAOneBlockCeiling(t *testing.T) {
	srv, c := halfScale(t, "Small")
	from := c.Pos()
	srv.SetBlock(int32(math.Floor(from.X))+2, int32(math.Floor(from.Y))+1, int32(math.Floor(from.Z)), registry.MustState("minecraft:stone", nil))
	c.MoveTo(from.X+2, from.Y, from.Z, 0, 0)
	expectNoTeleport(srv, c)
}

func TestAHalfScalePlayerStillHitsAFullHeightWall(t *testing.T) {
	srv, c := halfScale(t, "SmallBump")
	from := c.Pos()
	srv.SetBlock(int32(from.X)+2, int32(from.Y), int32(from.Z), registry.MustState("minecraft:stone", nil))
	srv.SetBlock(int32(from.X)+2, int32(from.Y)+1, int32(from.Z), registry.MustState("minecraft:stone", nil))
	c.MoveTo(from.X+2, from.Y, from.Z, 0, 0)
	if got := servertest.ExpectTeleport(t, c); got.X != from.X {
		t.Fatalf("sent back to x=%v, want %v", got.X, from.X)
	}
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

func TestAStepAgainstACactusIsTaken(t *testing.T) {
	srv, c := joinWithMoveChecks(t, servertest.SurvivalTestConfig(), "Prickle")
	from := c.Pos()
	cx := int32(math.Floor(from.X)) + 1
	srv.SetBlock(cx, int32(from.Y), int32(from.Z), registry.MustState("minecraft:cactus", map[string]string{"age": "0"}))
	// The cactus fills 1/16 to 15/16 of its cell; the body stops a hair
	// short of its side.
	c.MoveTo(float64(cx)+1.0/16-0.3-0.001, from.Y, from.Z, 0, 0)
	expectNoTeleport(srv, c)
}

func TestAStepOntoHoneyIsTaken(t *testing.T) {
	srv, c := joinWithMoveChecks(t, servertest.SurvivalTestConfig(), "Sticky")
	from := c.Pos()
	hx := int32(math.Floor(from.X)) + 1
	srv.SetBlock(hx, int32(from.Y), int32(from.Z), registry.MustState("minecraft:honey_block", nil))
	c.MoveTo(float64(hx)+0.5, from.Y+15.0/16, from.Z, 0, 0)
	expectNoTeleport(srv, c)
}

func TestAStepRefusedIntoABlockBanksNoFall(t *testing.T) {
	srv, c := joinWithMoveChecks(t, servertest.SurvivalTestConfig(), "Plunge")
	from := c.Pos()
	// The refused step is undone before the fall check, so its descent
	// does not count.
	c.MoveToGrounded(from.X, from.Y-0.5, from.Z, 0, 0, false)
	servertest.ExpectTeleport(t, c)
	if got := srv.PlayerFallDistance("Plunge"); got != 0 {
		t.Fatalf("fall distance %v after the refused step, want 0", got)
	}
}

func TestAGlidingBodyHasThreeHundredBlocksSquaredPerPacket(t *testing.T) {
	for _, tc := range []struct {
		name      string
		rule      bool
		distance  float64
		wantBack  bool
		glide     bool
		ruleValue bool
	}{
		{"within the glide budget", true, 15, false, true, true},
		{"past the glide budget", true, 18, true, true, true},
		{"on foot at the same distance", true, 15, true, false, true},
		{"past the budget with the elytra rule off", true, 25, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := joinWithMoveChecks(t, servertest.SurvivalTestConfig(), "Swoop", func(s *server.Server) {
				s.SetGameRule("elytra_movement_check", tc.ruleValue)
			})
			from := c.Pos()
			if tc.glide {
				srv.GiveSlot("Swoop", 6, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:elytra"), Count: 1})
				c.MoveToGrounded(from.X, from.Y+5, from.Z, 0, 0, false)
				servertest.StartFallFlying(c)
				servertest.WaitFor(t, srv, 100, func() bool { return srv.PlayerFallFlying("Swoop") })
			}
			afterTick(t, srv)
			pos := srv.PlayerPosition("Swoop")
			c.MoveToGrounded(pos.X+tc.distance, pos.Y, pos.Z, 0, 0, false)
			if tc.wantBack {
				servertest.ExpectTeleport(t, c)
				return
			}
			expectNoTeleport(srv, c)
		})
	}
}

func TestMorePacketsThanTheBurstShareOnePacketsBudget(t *testing.T) {
	srv, c := joinWithMoveChecks(t, servertest.SurvivalTestConfig(), "Burst")
	from := c.Pos()
	afterTick(t, srv)
	// Five packets in place, then the sixth a 11 blocks away: a squared 121
	// is within five packets' 500 but not the single packet's 100 the burst
	// falls back to.
	for range 5 {
		c.MoveTo(from.X, from.Y, from.Z, 0, 0)
	}
	c.MoveTo(from.X+11, from.Y, from.Z, 0, 0)
	servertest.ExpectTeleport(t, c)
}

func TestATeleportMovesTheTicksAnchor(t *testing.T) {
	srv, c := joinWithMoveChecks(t, servertest.SurvivalTestConfig(), "Warp")
	from := c.Pos()
	afterTick(t, srv)
	c.MoveTo(from.X+5, from.Y, from.Z, 0, 0)
	srv.RunLine("tp Warp 200 64 0")
	servertest.ExpectTeleport(t, c)
	// Measured from the tick's first position, 200 blocks back, this step
	// would be far too long; from the teleport's it is five blocks.
	c.MoveTo(205, 64, 0, 0, 0)
	expectNoTeleport(srv, c)
}

func TestAFrozenTickRateLetsAFarMoveThrough(t *testing.T) {
	srv, c := joinWithMoveChecks(t, servertest.SurvivalTestConfig(), "Still")
	srv.RunLine("tick freeze")
	servertest.AwaitTicks(t, srv, 2)
	from := c.Pos()
	c.MoveTo(from.X+50, from.Y, from.Z, 0, 0)
	expectNoTeleport(srv, c)
}
