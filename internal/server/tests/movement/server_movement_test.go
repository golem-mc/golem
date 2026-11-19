package movement

import (
	"bytes"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// TestAnInvalidMoveDisconnectsItsSender sends each move packet with a value
// vanilla refuses and checks the sender is dropped with the right message: a
// NaN position or a non-finite rotation for a player, the vehicle's own
// message for a vehicle. vanilla:
// ServerGamePacketListenerImpl.containsInvalidValues
func TestAnInvalidMoveDisconnectsItsSender(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		key  string
		send func(c *servertest.Client)
	}{
		{"position", "multiplayer.disconnect.invalid_player_movement", func(c *servertest.Client) {
			c.Send(protocol.IDPlayPosition, func(b *protocol.Buffer) {
				b.PutF64(math.NaN())
				b.PutF64(64)
				b.PutF64(0.5)
				b.PutU8(0)
			})
		}},
		{"position and rotation", "multiplayer.disconnect.invalid_player_movement", func(c *servertest.Client) {
			c.Send(protocol.IDPlayPositionAndRotation, func(b *protocol.Buffer) {
				b.PutF64(math.NaN())
				b.PutF64(64)
				b.PutF64(0.5)
				b.PutF32(0)
				b.PutF32(0)
				b.PutU8(0)
			})
		}},
		{"rotation", "multiplayer.disconnect.invalid_player_movement", func(c *servertest.Client) {
			c.Send(protocol.IDPlayRotation, func(b *protocol.Buffer) {
				b.PutF32(0)
				b.PutF32(float32(math.Inf(1)))
				b.PutU8(0)
			})
		}},
		{"vehicle move", "multiplayer.disconnect.invalid_vehicle_movement", func(c *servertest.Client) {
			c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
				b.PutF64(math.NaN())
				b.PutF64(64)
				b.PutF64(0.5)
				b.PutF32(0)
				b.PutF32(0)
				b.PutBool(false)
			})
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.GameMode = "creative"
			_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

			var want protocol.Buffer
			want.PutText(protocol.Text{Translate: tc.key})

			tc.send(c)
			if reason := c.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Equal(reason, want.Bytes()) {
				t.Errorf("disconnect = % X, want % X", reason, want.Bytes())
			}
			c.Disconnect()
		})
	}
}

// TestAnAcceptTeleportWithNothingPendingDisconnects checks that a confirm
// carrying the latest ID, sent after the reader already accepted it, kicks.
// vanilla: ServerGamePacketListenerImpl.handleAcceptTeleportPacket
func TestAnAcceptTeleportWithNothingPendingDisconnects(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	var id int32
	c.ReadJoin(func(packetID int32, data []byte) {
		if packetID == protocol.IDPlaySyncPosition {
			id = protocol.NewReader(data).ReadVarInt()
		}
	})
	c.Send(protocol.IDPlayConfirmTeleport, func(b *protocol.Buffer) {
		b.PutVarInt(id)
		b.PutF64(0.5)
		b.PutF64(64)
		b.PutF64(0.5)
		b.PutF32(0)
		b.PutF32(0)
	})
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: "multiplayer.disconnect.invalid_player_movement"})
	if reason := c.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Equal(reason, want.Bytes()) {
		t.Errorf("disconnect = % X, want % X", reason, want.Bytes())
	}
	c.Disconnect()
}
