package movement

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// TestATeleportResentOnAMoveKeepsTheOldRotation teleports a player and, before
// their client accepts, sends a move carrying a different yaw once the teleport
// has waited past its resend window: the resent teleport carries the rotation
// from before the move, not the move's own.
// vanilla: ServerGamePacketListenerImpl.handleMovePlayer, updateAwaitingTeleport
func TestATeleportResentOnAMoveKeepsTheOldRotation(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Mover", servertest.OpsListing(t, "Mover"))

	// The teleport is left unaccepted: the harness accepts a Synchronize
	// Player Position as it reads one, so nothing is read until the move has
	// been sent behind it and the server has had its say.
	c.ChatCommand("tp @s 100.5 64 100.5 90 0")
	servertest.WaitForTicks(t, srv, 200, func() bool { return srv.PlayerPosition("Mover").X == 100.5 })
	// Past teleportResendTicks, so the next move has the teleport sent again.
	servertest.AwaitTicks(t, srv, 22)

	c.MoveTo(100.5, 64, 100.5, 45, 0)
	var syncs []servertest.SyncPositionView
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySyncPosition {
			syncs = append(syncs, servertest.ReadSyncPosition(t, data))
		}
	})
	if len(syncs) != 2 {
		t.Fatalf("the teleport and its resend came to %d synchronize player position packets, want 2", len(syncs))
	}
	if syncs[1].Yaw != 90 {
		t.Errorf("the resent teleport faces yaw %v, want the 90 from before the move", syncs[1].Yaw)
	}
}

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
