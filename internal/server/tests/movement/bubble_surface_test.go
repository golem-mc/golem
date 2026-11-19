package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// surfaceParticles counts the splash and bubble particles of one tick.
func surfaceParticles(t *testing.T, srv *server.Server, c *servertest.Client) (splash, bubble int) {
	t.Helper()
	c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		if id != protocol.IDPlayLevelParticles {
			return false
		}
		p, ok := servertest.ParseLevelParticles(data)
		if !ok {
			t.Fatalf("invalid particle packet: %x", data)
		}
		switch {
		case p.Type == protocol.ParticleSplash && p.SpeedX == 1.0 && p.Y == 64:
			splash++
		case p.Type == protocol.ParticleBubble && p.SpeedX == 0.2 && p.Y == 64 && p.DY == 0.01:
			bubble++
		}
		return false
	})
	return splash, bubble
}

// A player in the top cell of a bubble column kicks up a splash and bubbles
// each tick; a spectator does not.
// vanilla: Player.onAboveBubbleColumn, Entity.sendBubbleColumnParticles
func TestPlayerAtBubbleColumnSurfaceSplashes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Elevator", func(srv *server.Server) {
		servertest.FreezeUntilHeld(t, srv)
	})
	servertest.HoldTicks(t, srv)
	release := srv.HoldChunk(0, 0)
	defer release()
	up := servertest.MustState(t, "minecraft:bubble_column", map[string]string{"drag": "false"})
	srv.SetBlock(2, 57, 8, servertest.MustState(t, "minecraft:soul_sand", nil))
	for y := int32(58); y <= 63; y++ {
		srv.SetBlock(2, y, 8, up)
	}
	c.Settle()
	c.MoveToGrounded(2.5, 63, 8.5, 0, 0, false)
	if splash, bubble := surfaceParticles(t, srv, c); splash == 0 || bubble == 0 {
		t.Errorf("survival player at the surface saw %d splash and %d bubble particles, want some of each", splash, bubble)
	}

	srv.RunLine("gamemode spectator Elevator")
	c.Settle()
	if splash, _ := surfaceParticles(t, srv, c); splash != 0 {
		t.Errorf("spectator saw %d splash particles, want none", splash)
	}
}
