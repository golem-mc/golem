package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// TestATeleportResentOnAMoveIsAbsolute runs a relative /tp and resends it on a
// move: the resend names the absolute target with no relative flags, so the
// client does not apply the offset twice.
// vanilla: ServerGamePacketListenerImpl.updateAwaitingTeleport
func TestATeleportResentOnAMoveIsAbsolute(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Mover", servertest.OpsListing(t, "Mover"))
	start := srv.PlayerPosition("Mover")

	c.ChatCommand("tp @s ~ ~10 ~")
	servertest.WaitForTicks(t, srv, 200, func() bool { return srv.PlayerPosition("Mover").Y != start.Y })
	servertest.AwaitTicks(t, srv, 22)

	c.MoveTo(start.X, start.Y+10, start.Z, 0, 0)
	var syncs []servertest.SyncPositionView
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySyncPosition {
			syncs = append(syncs, servertest.ReadSyncPosition(t, data))
		}
	})
	if len(syncs) != 2 {
		t.Fatalf("the teleport and its resend came to %d synchronize player position packets, want 2", len(syncs))
	}
	if syncs[1].Y != start.Y+10 {
		t.Errorf("the resent teleport has Y %v, want the absolute %v", syncs[1].Y, start.Y+10)
	}
	if syncs[1].Flags != 0 {
		t.Errorf("the resent teleport has flags %#x, want 0", syncs[1].Flags)
	}
}
