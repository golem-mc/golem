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

// TestARocketsAnglesAreToldOnce flies a rocket level at a steady speed: once
// a Position-Rotation has told its watcher the angles the flight settles
// on, the later movement ticks move it without telling them again.
// vanilla: ServerEntity.sendChanges, lastSentYRot and lastSentXRot
func TestARocketsAnglesAreToldOnce(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	c := connect().Join("Still")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnFireworkAtAngle(3.5, 66, 3.5, 0.3, 0, 0.1, 70).ID

	var (
		turns, moves int
		yaw, pitch   byte
		known        bool
	)
	c.ReadTicks(srv, 70, func(packetID int32, data []byte) bool {
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		switch packetID {
		case protocol.IDPlayEntityPosition:
			moves++
			return false
		case protocol.IDPlayTeleportEntity:
			known = false // its angles are floats, told afresh
			return false
		case protocol.IDPlayEntityPositionRot:
			r.ReadVarInt() // properties
			r.ReadI16()
			r.ReadI16()
			r.ReadI16()
		case protocol.IDPlayEntityRotation:
			r.ReadBool() // onGround
		default:
			return false
		}
		y, p := r.ReadU8(), r.ReadU8()
		if known && y == yaw && p == pitch {
			t.Errorf("the rocket's angles (%d, %d) were told again, packet 0x%02X", y, p, packetID)
		}
		yaw, pitch, known = y, p, true
		turns++
		return false
	})
	if turns == 0 || moves == 0 {
		t.Fatalf("the rocket was turned %d and moved %d times, want both", turns, moves)
	}
}
