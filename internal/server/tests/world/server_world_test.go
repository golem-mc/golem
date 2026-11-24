package world

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// joinTicks bounds the radius-two join: six batches at the opening pace of
// nine chunks a tick, with room for a loaded runner.
const joinTicks = 200

func TestDistancesFollowTheConfig(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	// Shorter than the render distance of 4 the test client asks for, so that
	// it is the server's setting that bounds the world it is sent.
	cfg.ViewDistance = 2
	// Inside the send view's reach, so the view alone holds its outer ring.
	cfg.SimulationDistance = 2

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
	// world holds that area too, wider than the simulation distance's five by
	// five; the holds are taken after the batch, so they are waited for.
	var chunks, batched int32
	batches := 0
	if !c.ReadTicks(srv, joinTicks, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayChunkData:
			chunks++
		case protocol.IDPlayChunkBatchFinished:
			got := protocol.NewReader(data).ReadVarInt()
			// Only the first batch is bound by the opening pace; the client's
			// acknowledgements widen the later ones.
			if batches == 0 && got > 9 {
				t.Errorf("batch size = %d, want no batch above the nine chunks per tick the server opens at", got)
			}
			batches++
			batched += got
			// The last batch is read through its end, so a chunk past the
			// 49 is counted and every chunk sent was in a batch.
			return chunks >= 7*7
		}
		return false
	}) {
		t.Fatalf("the join sent %d chunks within %d ticks, want %d", chunks, joinTicks, 7*7)
	}
	if chunks != 7*7 || batched != chunks {
		t.Errorf("join sent %d chunks, %d in batches, want %d", chunks, batched, 7*7)
	}
	servertest.UntilTicks(t, srv, joinTicks, "the join's 7x7 chunks held", func() bool { return srv.LoadedChunks() == 7*7 })
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
	// corners: one step east at radius 4 drops column -5 and gains column 6 on
	// rows -4..4, and drops column -4 and gains column 5 on rows -5 and 5.
	// vanilla: ChunkTrackingView.difference
	dropped, arrived := map[world.ChunkPos]int{}, map[world.ChunkPos]int{}
	for z := int32(-4); z <= 4; z++ {
		dropped[world.ChunkPos{X: -5, Z: z}] = 0
		arrived[world.ChunkPos{X: 6, Z: z}] = 0
	}
	for _, z := range []int32{-5, 5} {
		dropped[world.ChunkPos{X: -4, Z: z}] = 0
		arrived[world.ChunkPos{X: 5, Z: z}] = 0
	}
	batchSize := int32(-1)
	if !c.ReadTicks(srv, 200, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayUnloadChunk:
			// The coordinates are inverted on the wire, the client reading them as
			// one long.
			z, x := r.ReadI32(), r.ReadI32()
			p := world.ChunkPos{X: x, Z: z}
			if _, ok := dropped[p]; !ok {
				t.Errorf("unloaded chunk (%d, %d), which is not a chunk the view left", x, z)
			}
			dropped[p]++
		case protocol.IDPlayChunkData:
			x, z := r.ReadI32(), r.ReadI32()
			p := world.ChunkPos{X: x, Z: z}
			if _, ok := arrived[p]; !ok {
				t.Errorf("received chunk (%d, %d), which is not a chunk the view gained", x, z)
			}
			arrived[p]++
		case protocol.IDPlayChunkBatchFinished:
			batchSize = r.ReadVarInt()
			return true
		}
		return false
	}) {
		t.Fatal("the chunk batch did not finish")
	}
	for p, n := range dropped {
		if n != 1 {
			t.Errorf("chunk (%d, %d) unloaded %d times, want once", p.X, p.Z, n)
		}
	}
	for p, n := range arrived {
		if n != 1 {
			t.Errorf("chunk (%d, %d) sent %d times, want once", p.X, p.Z, n)
		}
	}
	if batchSize != int32(len(arrived)) {
		t.Errorf("batch size = %d, want %d", batchSize, len(arrived))
	}

	// The area is the same size as it was, only somewhere else: the held
	// square follows the player, its column ahead generated and the one
	// behind let go, whatever the client asked to be sent.
	servertest.PollUntil(t, 30*time.Second, func() bool { return srv.LoadedChunks() == 21*21 })

	// Nothing is left generated once the player is gone.
	c.Conn.Close()
	servertest.PollUntil(t, 5*time.Second, func() bool { return srv.LoadedChunks() == 0 })
}
