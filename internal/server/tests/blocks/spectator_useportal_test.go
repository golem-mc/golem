package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// spectatorBesideBlock joins a spectator with the loop stopped.
func spectatorBesideBlock(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Ghost", servertest.OpsListing(t, "Ghost"))
	c.SendChangeGameMode(protocol.GameModeSpectator)
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.PlayerGameMode("Ghost") == protocol.GameModeSpectator })
	srv.StopTick()
	return srv, c
}

// TestASpectatorUsingAnEndPortalEntersTheEnd checks the use of an end portal
// block takes a spectator to the end, with no cooldown.
// vanilla: ServerPlayerGameMode.useItemOn, EndPortalBlock.getPortalDestination
func TestASpectatorUsingAnEndPortalEntersTheEnd(t *testing.T) {
	t.Parallel()
	srv, c := spectatorBesideBlock(t)
	if !srv.SetBlock(10, 63, 10, srv.EndPortalState()) {
		t.Fatal("placing the end portal block failed")
	}
	c.MoveTo(10.5, 63, 8.5, 0, 0)
	c.Settle()
	c.UseBlock(10, 63, 10)
	servertest.WaitFor(t, srv, 400, func() bool { return srv.PlayerDimension("Ghost") == world.EndName })
	if got := srv.PortalCooldown("Ghost"); got != 0 {
		t.Errorf("portal cooldown = %d, want 0", got)
	}
}

// TestASpectatorUsingAnEndGatewayMovesToItsExit checks the use of a gateway
// with an exit teleports a spectator there.
// vanilla: ServerPlayerGameMode.useItemOn, EndGatewayBlock.getPortalDestination
func TestASpectatorUsingAnEndGatewayMovesToItsExit(t *testing.T) {
	t.Parallel()
	srv, c := spectatorBesideBlock(t)
	srv.SetLoadedBlock(3, gatewayY, 3, servertest.BlockState(t, "minecraft:end_gateway"))
	srv.SetLoadedBlock(20, 120, 20, servertest.BlockState(t, "minecraft:stone"))
	srv.SetEndGatewayExit(3, gatewayY, 3, 20, 120, 20, true)
	c.MoveTo(3.5, gatewayY, 1.5, 0, 0)
	c.Settle()
	c.UseBlock(3, gatewayY, 3)
	got := servertest.ReadSyncPosition(t, c.AwaitPlay(protocol.IDPlaySyncPosition))
	if got.X != 20.5 || got.Y != 120 || got.Z != 20.5 {
		t.Errorf("landed at (%v, %v, %v), want the exit's bottom centre (20.5, 120, 20.5)", got.X, got.Y, got.Z)
	}
}
