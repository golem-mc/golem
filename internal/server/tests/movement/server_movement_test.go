package movement

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

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

// TestAnAcceptTeleportWithANaNPositionDisconnects checks that a confirm whose
// ID matches but whose position is NaN is kicked.
// vanilla: ServerGamePacketListenerImpl.handleAcceptTeleportPacket
func TestAnAcceptTeleportWithANaNPositionDisconnects(t *testing.T) {
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
		b.PutF64(math.NaN())
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

// TestAnInvalidMoveWhileLoadingStillDisconnects checks that the invalid-value
// check stands ahead of the loading screen: a NaN move from a client that has
// not sent Player Loaded kicks rather than being dropped with the other moves.
// vanilla: ServerGamePacketListenerImpl.handleMovePlayer
func TestAnInvalidMoveWhileLoadingStillDisconnects(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.EnterWorldLoading("Testificate")

	c.Send(protocol.IDPlayPosition, func(b *protocol.Buffer) {
		b.PutF64(math.NaN())
		b.PutF64(64)
		b.PutF64(0.5)
		b.PutU8(0)
	})
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: "multiplayer.disconnect.invalid_player_movement"})
	if reason := c.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Equal(reason, want.Bytes()) {
		t.Errorf("disconnect = % X, want % X", reason, want.Bytes())
	}
	c.Disconnect()
}

// TestAnInvalidVehicleMoveWhileLoadingStillDisconnects checks that the
// vehicle's invalid-value check also stands ahead of the loading screen: a NaN
// Move Vehicle from a client that has not sent Player Loaded kicks rather than
// being dropped with the other moves.
// vanilla: ServerGamePacketListenerImpl.handleMoveVehicle
func TestAnInvalidVehicleMoveWhileLoadingStillDisconnects(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.EnterWorldLoading("Testificate")

	c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(math.NaN())
		b.PutF64(64)
		b.PutF64(0.5)
		b.PutF32(0)
		b.PutF32(0)
		b.PutBool(false)
	})
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: "multiplayer.disconnect.invalid_vehicle_movement"})
	if reason := c.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Equal(reason, want.Bytes()) {
		t.Errorf("disconnect = % X, want % X", reason, want.Bytes())
	}
	c.Disconnect()
}

// TestAnOutOfRangeMoveIsNotKicked sends a position past the world border and
// an infinite one, both of which vanilla clamps rather than disconnects on,
// and checks the sender stays connected: a follow-up the server answers means
// the connection is still up. vanilla:
// ServerGamePacketListenerImpl.containsInvalidValues
func TestAnOutOfRangeMoveIsNotKicked(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		send func(c *servertest.Client)
	}{
		{"past the border", func(c *servertest.Client) {
			c.Send(protocol.IDPlayPosition, func(b *protocol.Buffer) {
				b.PutF64(30_000_001)
				b.PutF64(64)
				b.PutF64(0.5)
				b.PutU8(0)
			})
		}},
		{"infinity", func(c *servertest.Client) {
			c.Send(protocol.IDPlayPosition, func(b *protocol.Buffer) {
				b.PutF64(math.Inf(1))
				b.PutF64(64)
				b.PutF64(0.5)
				b.PutU8(0)
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

			tc.send(c)
			// A ping the server answers settles the move behind it; a
			// disconnected connection would fail the read instead.
			c.Settle()
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

// TestAnAcceptTeleportAppliesItsPositionBeforePlayerLoaded checks that a
// pending teleport's confirm moves the player to the position and rotation it
// carries, with on-ground false, though the client has not sent Player Loaded.
// vanilla: ServerGamePacketListenerImpl.handleAcceptTeleportPacket
func TestAnAcceptTeleportAppliesItsPositionBeforePlayerLoaded(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.EnterWorldLoading("Testificate")

	// Read raw: the harness reader would accept the teleport itself.
	c.ChatCommand("tp Testificate 8 80 8")
	var id int32
	var sync servertest.SyncPositionView
	for id == 0 {
		pid, data, err := c.Conn.ReadPacket()
		if err != nil {
			t.Fatalf("read packet: %v", err)
		}
		if pid == protocol.IDPlaySyncPosition {
			sync = servertest.ReadSyncPosition(t, data)
			id = protocol.NewReader(data).ReadVarInt()
		}
	}
	c.Send(protocol.IDPlayConfirmTeleport, func(b *protocol.Buffer) {
		b.PutVarInt(id)
		b.PutF64(sync.X + 0.25)
		b.PutF64(sync.Y)
		b.PutF64(sync.Z - 0.25)
		b.PutF32(30)
		b.PutF32(10)
	})
	servertest.UntilTicks(t, srv, 100, "the confirmed position", func() bool {
		p := srv.PlayerPosition("Testificate")
		return p.X == sync.X+0.25 && p.Y == sync.Y && p.Z == sync.Z-0.25 &&
			p.Yaw == 30 && p.Pitch == 10 && !p.OnGround
	})
	c.Disconnect()
}
