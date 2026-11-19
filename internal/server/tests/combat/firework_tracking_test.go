package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAFireworkRocketsVelocityGoesToItsWatchersAlone flies a rocket beside
// one player while another stands past its tracking range: the velocity
// its movement ticks carry is told to the player watching it and to nobody
// else.
// vanilla: ServerEntity.sendChanges, sendToTrackingPlayers
func TestAFireworkRocketsVelocityGoesToItsWatchersAlone(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4 // the rocket is then seen from 64 blocks
	srv, connect := servertest.Start(t, cfg)
	near := connect().Join("Near")
	far := connect().Join("Far")
	servertest.HoldTicks(t, srv)
	// 120 blocks east is out of the 64 blocks four chunks of sight reach.
	far.MoveToGround(120.5, 64, 0.5, true)
	far.ReadTicks(srv, 0, func(int32, []byte) bool { return false })

	// Flight 3: a life of 40 to 51 ticks, so the 25 cover three movement
	// ticks of a rise that speeds up.
	id := srv.SpawnFireworkStackEntity(servertest.FireworkStack(3, 0), 3.5, 65, 3.5).ID
	motionOf := servertest.PacketAbout(id, protocol.IDPlaySetEntityMotion)
	far.ExpectNoneWithinTicks(srv, 25, motionOf)

	var told int
	near.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if motionOf(id, data) {
			told++
		}
		return false
	})
	if told == 0 {
		t.Fatal("the watching player was told no velocity of the rising rocket")
	}
}
