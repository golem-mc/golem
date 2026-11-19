package storage

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// The acknowledgement-paced chunk stream. The tests here join with a view
// distance of ten, so the client is owed 441 chunks, and answer none of the
// batches by default (noBatchAcks): whatever more than the pacing allows is
// the server's own doing, which is what each of them watches for.
// vanilla: PlayerChunkSender

// pacingQuiet is how many chunk-sender beats a client that has said nothing
// is watched for chunks it should not have been sent. Five without one is far
// past the pace even the slowest acked stream holds.
const pacingQuiet = 5

// awaitBeats returns once the chunk senders have made n more passes. The
// senders beat on their own clock beside the tick loop, so a held loop does
// not stall them; a sender that never passes fails as stalled.
func awaitBeats(t *testing.T, srv *server.Server, n int) {
	t.Helper()
	for range n {
		select {
		case <-srv.ChunkBeat():
		case <-time.After(servertest.ExpectTimeout):
			t.Fatalf("no chunk sender pass within %s", servertest.ExpectTimeout)
		}
	}
}

// quietFor lets n sender beats pass, then settles the connection and hands
// see every packet that reached the client up to the settle's answer.
func quietFor(srv *server.Server, c *servertest.Client, n int, see func(id int32)) {
	c.T.Helper()
	awaitBeats(c.T, srv, n)
	c.SettleAndCollect(func(id int32, _ []byte) { see(id) })
}

// joinUnacknowledged runs the join of a client that answers no chunk batch
// and reads through the first finished one, returning how many chunks it
// carried. The batches stop with the first: until an acknowledgement has
// arrived, one batch at a time is all the pacing lets run ahead.
func joinUnacknowledged(c *servertest.Client) (chunks, batchSize int32) {
	c.T.Helper()
	for packetID, data := range c.Packets(servertest.ExpectTimeout) {
		switch packetID {
		case protocol.IDPlayChunkData:
			chunks++
			r := protocol.NewReader(data)
			if c.JoinChunks == nil {
				c.JoinChunks = map[[2]int32][]byte{}
			}
			c.JoinChunks[[2]int32{r.ReadI32(), r.ReadI32()}] = data
		case protocol.IDPlayChunkBatchFinished:
			batchSize = protocol.NewReader(data).ReadVarInt()
			return chunks, batchSize
		}
	}
	c.T.Fatalf("the join's first chunk batch did not finish within %s", servertest.ExpectTimeout)
	return 0, 0
}

// acknowledge sends the Chunk Batch Received a client answers a finished
// batch with: the chunks-per-tick rate it asks the server for.
func acknowledge(c *servertest.Client, rate float32) {
	c.T.Helper()
	c.Send(protocol.IDPlayChunkBatchReceived, func(b *protocol.Buffer) {
		b.PutF32(rate)
	})
}

// expectNoChunks fails if any packet of the chunk stream arrives within
// pacingQuiet beats on a stream that should be standing still.
func expectNoChunks(srv *server.Server, c *servertest.Client) {
	c.T.Helper()
	quietFor(srv, c, pacingQuiet, func(id int32) {
		switch id {
		case protocol.IDPlayKeepAlive, protocol.IDPlayUpdateLight:
		default:
			c.T.Fatalf("packet 0x%02X arrived on a stopped stream", id)
		}
	})
}

// expectBatch reads one chunk batch and returns how many chunks it carried.
func expectBatch(c *servertest.Client) int32 {
	c.T.Helper()
	c.ExpectPlay(protocol.IDPlayChunkBatchStart)
	var chunks int32
	for packetID := range c.Packets(servertest.ExpectTimeout) {
		switch packetID {
		case protocol.IDPlayChunkData:
			chunks++
		case protocol.IDPlayChunkBatchFinished:
			return chunks
		}
	}
	c.T.Fatalf("the resumed batch did not finish within %s", servertest.ExpectTimeout)
	return 0
}

// TestUnacknowledgedBatchesHoldTheChunks checks the hold the pacing puts a
// client that never acknowledges under: the join's first batch carries the
// opening quota alone — nine chunks, the start rate — and nothing follows
// it until the client answers.
func TestUnacknowledgedBatchesHoldTheChunks(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Dial(t, cfg, func(s *server.Server) { warm(t, s, 0) })
	c.NoBatchAcks = true
	c.ViewAsk = 10
	c.Login("Withholder")
	c.Configure()

	chunks, batchSize := joinUnacknowledged(c)
	if batchSize != 9 {
		t.Errorf("the unacknowledged join's first batch = %d chunks, want the nine the start quota allows", batchSize)
	}
	if chunks != 9 {
		t.Errorf("the unacknowledged join sent %d chunks, want 9", chunks)
	}

	// One batch, unacknowledged, is the whole allowance: nothing else of
	// the 441 the view distance owes may follow.
	expectNoChunks(srv, c)
}

// TestAnsweredBatchResumesSendingAtTheNamedRate checks the other half of
// the pacing: an acknowledgement names the rate the client wants, and the
// batches that follow it carry exactly that many chunks at a time, a
// batch's worth a tick, whether or not the client keeps answering.
func TestAnsweredBatchResumesSendingAtTheNamedRate(t *testing.T) {
	t.Parallel()
	_, c := joinPaced(t, "Answerer")

	// Four chunks a tick is what this client asks for; the batches after
	// the answer carry four each.
	acknowledge(c, 4)
	if got := expectBatch(c); got != 4 {
		t.Errorf("the first answered batch = %d chunks, want 4", got)
	}
	// The answer lifted the cap to ten batches ahead, so the pace holds
	// without another answer.
	if got := expectBatch(c); got != 4 {
		t.Errorf("the unacknowledged follow-up batch = %d chunks, want 4", got)
	}
}

// TestAnsweredBatchClampsTheNamedRate checks the top of the range the
// pacing holds the named rate to: a client asking for a thousand chunks a
// tick is answered with sixty-four-chunk batches, the most a batch carries.
func TestAnsweredBatchClampsTheNamedRate(t *testing.T) {
	t.Parallel()
	_, c := joinPaced(t, "Clamper")

	acknowledge(c, 1000)
	if got := expectBatch(c); got != 64 {
		t.Errorf("the batch after a rate of a thousand = %d chunks, want the clamp's 64", got)
	}
}

// setRenderDistance reports a new render distance, the way the options menu
// does mid-play.
func setRenderDistance(c *servertest.Client, distance int8) {
	c.T.Helper()
	c.Send(protocol.IDPlayClientInformation, func(b *protocol.Buffer) {
		b.PutString("en_GB")
		b.PutI8(distance)
		b.PutVarInt(0)
		b.PutBool(true)
		b.PutU8(0x7F)
		b.PutVarInt(1)
		b.PutBool(false)
		b.PutBool(true)
		b.PutVarInt(0)
	})
}

// warm generates and holds, until the server shuts down, the view-10 square of chunks around the block
// x, 0, so a join there finds every chunk ready and its first batch carries
// the whole quota rather than whichever chunks the generator has finished.
func warm(t *testing.T, srv *server.Server, x int32) {
	t.Helper()
	for cx := int32(-10); cx <= 10; cx++ {
		for cz := int32(-10); cz <= 10; cz++ {
			srv.HoldChunk(x+cx*16, cz*16)
		}
	}
}

// joinPaced starts a join at view distance 10 whose batches go unanswered,
// returning once the first batch (nine chunks of the 441) is through.
func joinPaced(t *testing.T, name string, opts ...func(*server.Server)) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Dial(t, cfg, append([]func(*server.Server){func(s *server.Server) { warm(t, s, 0) }}, opts...)...)
	c.NoBatchAcks = true
	c.ViewAsk = 10
	c.Login(name)
	c.Configure()
	if _, batchSize := joinUnacknowledged(c); batchSize != 9 {
		t.Fatalf("the join's first batch = %d chunks, want 9", batchSize)
	}
	return srv, c
}

// readView acknowledges every batch until the client holds want distinct
// chunks, failing on a chunk sent twice or on an unload, and returns the
// chunks it read.
func readView(c *servertest.Client, want int) map[[2]int32]bool {
	c.T.Helper()
	seen := map[[2]int32]bool{}
	for packetID, data := range c.Packets(servertest.ExpectTimeout) {
		switch packetID {
		case protocol.IDPlayChunkBatchFinished:
			if len(seen) >= want {
				return seen
			}
			acknowledge(c, 64)
		case protocol.IDPlayChunkData:
			r := protocol.NewReader(data)
			pos := [2]int32{r.ReadI32(), r.ReadI32()}
			if seen[pos] {
				c.T.Errorf("chunk %v sent twice", pos)
			}
			seen[pos] = true
		case protocol.IDPlayUnloadChunk:
			r := protocol.NewReader(data)
			z, x := r.ReadI32(), r.ReadI32()
			c.T.Errorf("chunk (%d, %d) unloaded from a still player's view", x, z)
		}
	}
	c.T.Fatalf("the view was not sent whole: %d of %d chunks", len(seen), want)
	return nil
}
