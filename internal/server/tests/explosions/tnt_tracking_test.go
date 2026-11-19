package explosions

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAPrimedTNTsVelocityGoesToItsWatchersAlone drops a primed TNT beside
// one player while another stands past its tracking range: the falling
// body's velocity is told to the player watching it and to nobody else.
// vanilla: ServerEntity.sendChanges, sendToTrackingPlayers
func TestAPrimedTNTsVelocityGoesToItsWatchersAlone(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4 // the TNT is then seen from 64 blocks
	srv, connect := servertest.Start(t, cfg)
	near := connect().Join("Near")
	far := connect().Join("Far")
	servertest.HoldTicks(t, srv)
	// 120 blocks east is out of the 64 blocks four chunks of sight reach.
	far.MoveToGround(120.5, 64, 0.5, true)
	far.ReadTicks(srv, 0, func(int32, []byte) bool { return false })

	// Sixteen blocks up, so it falls, and the velocity changes, every tick.
	id := srv.SpawnTNT(3.5, 80, 3.5, 70)
	motionOf := servertest.PacketAbout(id, protocol.IDPlaySetEntityMotion)
	far.ExpectNoneWithinTicks(srv, 10, motionOf)

	var told int
	near.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if motionOf(id, data) {
			told++
		}
		return false
	})
	if told == 0 {
		t.Fatal("the watching player was told no velocity of the falling TNT")
	}
}
