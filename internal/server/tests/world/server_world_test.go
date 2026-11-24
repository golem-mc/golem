package world

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

func TestDistancesFollowTheConfig(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	// Shorter than the render distance of 4 the test client asks for, so that
	// it is the server's setting that bounds the world it is sent.
	cfg.ViewDistance = 2
	cfg.SimulationDistance = 3

	srv, c := servertest.Dial(t, cfg)
	c.Login("Testificate")
	c.Configure()

	login := protocol.NewReader(c.Expect(protocol.IDPlayLogin))
	login.ReadI32()  // entity ID
	login.ReadBool() // hardcore
	for range login.ReadVarInt() {
		login.ReadString(32767) // dimension names
	}
	login.ReadVarInt() // max players
	if got := login.ReadVarInt(); got != int32(cfg.ViewDistance) {
		t.Errorf("view distance = %d, want %d", got, cfg.ViewDistance)
	}
	if got := login.ReadVarInt(); got != int32(cfg.SimulationDistance) {
		t.Errorf("simulation distance = %d, want %d", got, cfg.SimulationDistance)
	}

	// The chunks arrive in batches the client's acknowledgements pace
	// rather than one: read the whole view before the server's holds are
	// judged. The client was sent the view the render distance of
	// 4 bounds the server's view distance to, plus the neighbor ring the client
	// needs to light its edge chunks: seven by seven, as vanilla sends. The
	// world holds the same area, the simulation distance's, for its own work - the
	// natural spawn and the ticking among it - and those holds are taken
	// after the batch, so they are waited for.
	var chunks int32
	for packetID, data := range c.Packets(servertest.JoinTimeout) {
		switch packetID {
		case protocol.IDPlayChunkData:
			chunks++
		case protocol.IDPlayChunkBatchFinished:
			if got := protocol.NewReader(data).ReadVarInt(); got > 9 {
				t.Errorf("batch size = %d, want no batch above the nine chunks per tick the server opens at", got)
			}
		}
		if chunks == 7*7 {
			servertest.PollUntil(t, 30*time.Second, func() bool { return srv.LoadedChunks() == 7*7 })
			return
		}
	}
	t.Fatalf("the join did not finish within %s", servertest.JoinTimeout)
}

func TestChunksFollowThePlayer(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	// The simulation distance the harness would have taken down to the
	// render distance stays at its default ten: this is the test of the
	// load radius, taken at its full reach.
	srv, c := servertest.Dial(t, cfg, servertest.FullSimulationDistance)
	c.Login("Testificate")
	c.Configure() // which asks for a render distance of 4
	c.EnterWorld("Testificate")

	// Joining held the simulation distance's 21x21 square around spawn, in
	// chunk (0, 0), for the world's own work, of which the client was sent
	// the 9x9 its render distance of 4 bounds the view distance to. The
	// holds are taken after the batch that ends the loading screen, so they
	// are waited for.
	servertest.PollUntil(t, 30*time.Second, func() bool { return srv.LoadedChunks() == 21*21 })

	// Walking east into the next chunk takes that area with the player: the
	// column of chunks ahead of them is generated and sent, and the one they
	// left behind is unloaded.
	c.Send(protocol.IDPlayPosition, func(b *protocol.Buffer) {
		b.PutF64(16.5)
		b.PutF64(64)
		b.PutF64(0.5)
		b.PutU8(0) // movement flags
	})

	center := protocol.NewReader(c.ExpectPlay(protocol.IDPlayCenterChunk))
	if x, z := center.ReadVarInt(), center.ReadVarInt(); x != 1 || z != 0 {
		t.Errorf("center chunk = (%d, %d), want (1, 0)", x, z)
	}

	dropped := map[int32]bool{}
	for range 9 {
		r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayUnloadChunk))
		// The coordinates are inverted on the wire, the client reading them as
		// one long.
		z, x := r.ReadI32(), r.ReadI32()
		if x != -4 {
			t.Errorf("unloaded chunk (%d, %d), want the column at x=-4", x, z)
		}
		dropped[z] = true
	}
	if len(dropped) != 9 {
		t.Errorf("unloaded %d distinct chunks, want 9", len(dropped))
	}

	c.ExpectPlay(protocol.IDPlayChunkBatchStart)
	arrived := map[int32]bool{}
	for range 9 {
		r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayChunkData))
		x, z := r.ReadI32(), r.ReadI32()
		if x != 5 {
			t.Errorf("received chunk (%d, %d), want the column at x=5", x, z)
		}
		arrived[z] = true
	}
	if len(arrived) != 9 {
		t.Errorf("received %d distinct chunks, want 9", len(arrived))
	}
	batch := protocol.NewReader(c.ExpectPlay(protocol.IDPlayChunkBatchFinished))
	if got := batch.ReadVarInt(); got != 9 {
		t.Errorf("batch size = %d, want 9", got)
	}

	// The area is the same size as it was, only somewhere else: the held
	// square follows the player, its column ahead generated and the one
	// behind let go, whatever the client asked to be sent.
	servertest.PollUntil(t, 30*time.Second, func() bool { return srv.LoadedChunks() == 21*21 })

	// Nothing is left generated once the player is gone.
	c.Conn.Close()
	servertest.PollUntil(t, 5*time.Second, func() bool { return srv.LoadedChunks() == 0 })
}
