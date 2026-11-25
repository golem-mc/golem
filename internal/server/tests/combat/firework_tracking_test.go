package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
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

// TestAFireworkRocketsBurnEndsIsTold sets a rocket alight for four ticks and
// flies it on: the watcher is told the fire flag once, then the flag clearing
// on the fourth tick, off the rocket's ten-tick movement period, while the
// rocket is still flying.
// vanilla: Entity.baseTick, ServerEntity.sendChanges
func TestAFireworkRocketsBurnEndsIsTold(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Watcher")
	servertest.HoldTicks(t, srv)
	rocket := srv.SpawnFireworkAtAngle(3.5, 65, 3.5, 0, 0, 0, 200)
	srv.RunLine("data merge entity " + rocket.UUID.String() + " {Fire:4s}")

	set, cleared := 0, false
	c.ReadTicks(srv, 4, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != rocket.ID {
			return false
		}
		flags, ok := servertest.ReadEntityFlags(r)
		if !ok {
			return false
		}
		if flags&protocol.EntityFlagOnFire != 0 {
			set++
			return false
		}
		cleared = set > 0
		return cleared
	})
	if set != 1 || !cleared {
		t.Fatalf("fire flag told set %d times, then cleared = %v within four ticks; want once, then cleared", set, cleared)
	}
	if _, _, ok := srv.FireworkState(rocket.UUID); !ok {
		t.Fatal("the rocket ended before its burn did")
	}
}
