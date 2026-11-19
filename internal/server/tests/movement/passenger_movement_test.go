package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: ServerGamePacketListenerImpl.handlePlayerPositionChange
func TestAMinecartPassengerIgnoresReportedPosition(t *testing.T) {
	for _, packet := range []struct {
		name string
		id   int32
	}{
		{"position", protocol.IDPlayPosition},
		{"position and rotation", protocol.IDPlayPositionAndRotation},
	} {
		t.Run(packet.name, func(t *testing.T) {
			srv, c := joinWithMoveChecks(t, servertest.SurvivalTestConfig(), "Rider")
			servertest.HoldTicks(t, srv)
			servertest.CartRails(t, srv, 1, 4)
			cart := srv.SpawnMinecart(2.5, 64.0625, 1.5)
			c.UseOn(cart)
			if riders := servertest.AwaitPassengersFor(c, cart); len(riders) != 1 || riders[0] != c.PlayerEntityID {
				t.Fatalf("passengers = %v, want Rider", riders)
			}
			c.ExpectNoneWithinTicks(srv, 2, servertest.PacketIs(protocol.IDPlaySyncPosition))
			before := srv.PlayerPosition("Rider")
			for _, y := range []float64{64.05, 63.9} {
				// Report feet inside the rail, then its solid support. Neither
				// can move the seat or trigger a collision correction.
				c.Send(packet.id, func(b *protocol.Buffer) {
					b.PutF64(2.5)
					b.PutF64(y)
					b.PutF64(1.5)
					if packet.id == protocol.IDPlayPositionAndRotation {
						b.PutF32(450)
						b.PutF32(120)
					}
					b.PutU8(3)
				})
				c.ExpectNoneWithinTicks(srv, 5, servertest.PacketIs(protocol.IDPlaySyncPosition))
				if riders := srv.MinecartPassengerIDs(cart); len(riders) != 1 || riders[0] != c.PlayerEntityID {
					t.Fatalf("passengers after move = %v, want Rider", riders)
				}
				got := srv.PlayerPosition("Rider")
				if got.X != before.X || got.Y != before.Y || got.Z != before.Z || got.OnGround != before.OnGround {
					t.Errorf("seat after move = %+v, want position and ground flag from %+v", got, before)
				}
				wantYaw, wantPitch := before.Yaw, before.Pitch
				if packet.id == protocol.IDPlayPositionAndRotation {
					wantYaw, wantPitch = 90, 90
				}
				if got.Yaw != wantYaw || got.Pitch != wantPitch {
					t.Errorf("look after move = %v/%v, want %v/%v", got.Yaw, got.Pitch, wantYaw, wantPitch)
				}
			}
		})
	}
}
