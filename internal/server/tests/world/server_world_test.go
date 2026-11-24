package world

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
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
	// the view its render distance of 4 bounds the view distance to. The
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

	// The send view is vanilla's neighbor-inclusive one, rounded at its
	// corners, so what leaves and arrives is the difference of two views.
	was, now := world.ChunkPos{}, world.ChunkPos{X: 1}
	dropped, arrived := map[world.ChunkPos]bool{}, map[world.ChunkPos]bool{}
	for x := int32(-8); x <= 9; x++ {
		for z := int32(-8); z <= 8; z++ {
			p := world.ChunkPos{X: x, Z: z}
			in, wasIn := world.InSendView(p, now, 4), world.InSendView(p, was, 4)
			if wasIn && !in {
				dropped[p] = false
			}
			if in && !wasIn {
				arrived[p] = false
			}
		}
	}
	nDropped, nArrived := len(dropped), len(arrived)
	for range nDropped {
		r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayUnloadChunk))
		// The coordinates are inverted on the wire, the client reading them as
		// one long.
		z, x := r.ReadI32(), r.ReadI32()
		p := world.ChunkPos{X: x, Z: z}
		if seen, ok := dropped[p]; !ok || seen {
			t.Errorf("unloaded chunk (%d, %d), which is not a chunk the view left", x, z)
		}
		dropped[p] = true
	}

	c.ExpectPlay(protocol.IDPlayChunkBatchStart)
	for range nArrived {
		r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayChunkData))
		x, z := r.ReadI32(), r.ReadI32()
		p := world.ChunkPos{X: x, Z: z}
		if seen, ok := arrived[p]; !ok || seen {
			t.Errorf("received chunk (%d, %d), which is not a chunk the view gained", x, z)
		}
		arrived[p] = true
	}
	batch := protocol.NewReader(c.ExpectPlay(protocol.IDPlayChunkBatchFinished))
	if got := batch.ReadVarInt(); got != int32(nArrived) {
		t.Errorf("batch size = %d, want %d", got, nArrived)
	}

	// The area is the same size as it was, only somewhere else: the held
	// square follows the player, its column ahead generated and the one
	// behind let go, whatever the client asked to be sent.
	servertest.PollUntil(t, 30*time.Second, func() bool { return srv.LoadedChunks() == 21*21 })

	// Nothing is left generated once the player is gone.
	c.Conn.Close()
	servertest.PollUntil(t, 5*time.Second, func() bool { return srv.LoadedChunks() == 0 })
}
