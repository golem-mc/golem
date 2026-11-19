package storage

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
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

// TestAnsweredBatchFallsARateOfNaNToTheFloor checks the bottom: a client
// whose answer arrives as NaN - not a number, whatever wrote it - is served
// at the floor of a hundredth of a chunk a tick: one chunk, then silence
// while the quota builds back towards the next one.
func TestAnsweredBatchFallsARateOfNaNToTheFloor(t *testing.T) {
	t.Parallel()
	srv, c := joinPaced(t, "Floater")

	acknowledge(c, float32(math.NaN()))
	if got := expectBatch(c); got != 1 {
		t.Errorf("the batch after a NaN rate = %d chunks, want 1", got)
	}
	// A hundredth of a chunk a tick takes a hundred ticks to afford the
	// next one: none of it falls inside the quiet beats.
	expectNoChunks(srv, c)
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

// TestAcknowledgementIsReadWhileTheSenderIsBusy checks that Chunk Batch
// Received does not wait on the chunk sender: a client's answer arriving
// while a batch is mid-write must not hold up the packets behind it.
func TestAcknowledgementIsReadWhileTheSenderIsBusy(t *testing.T) {
	t.Parallel()
	var release func()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Dial(t, cfg, func(s *server.Server) { release = s.HoldFirstChunk() })
	defer release()
	c.NoBatchAcks = true
	c.ViewAsk = 10
	c.Login("Busybody")
	c.Configure()
	c.AwaitPlay(protocol.IDPlayChunkBatchStart)

	acknowledge(c, 4)
	c.Chat("still reading")
	c.ExpectChat("Busybody", "still reading")
}

// TestShrunkRenderDistanceDropsTheQueuedChunks checks that chunks queued
// beyond a render distance the client then shrinks to are never sent.
func TestShrunkRenderDistanceDropsTheQueuedChunks(t *testing.T) {
	t.Parallel()
	srv, c := joinPaced(t, "Shrinker")

	setRenderDistance(c, 2)
	acknowledge(c, 64)
	// Five by five is 25 chunks, nine of which the join already sent.
	if got := expectBatch(c); got != 16 {
		t.Errorf("the batch after shrinking to view 2 = %d chunks, want 16", got)
	}
	expectNoChunks(srv, c)
}

// TestMovingRebuildsTheQueuedChunksAroundThePlayer checks that a player who
// moves while chunks are queued is sent the ones around the new position.
func TestMovingRebuildsTheQueuedChunksAroundThePlayer(t *testing.T) {
	t.Parallel()
	_, c := joinPaced(t, "Walker", servertest.OpsListing(t, "Walker"), func(s *server.Server) { warm(t, s, 640) })

	c.MarkLoaded()
	c.ChatCommand("tp @s 640.5 64 0.5")
	// The centre chunk moves once the queue has been rebuilt around the
	// body, on the chunk sender's own beat, which can go before or after
	// the teleport's sync; either way it comes before any chunk.
	centred := false
	see := func(id int32, data []byte) {
		if id == protocol.IDPlayCenterChunk {
			r := protocol.NewReader(data)
			centred = r.ReadVarInt() == 40 && r.ReadVarInt() == 0
		}
	}
	if !c.Pump(servertest.ExpectTimeout, nil, func(id int32, data []byte) bool {
		see(id, data)
		return id == protocol.IDPlaySyncPosition
	}) {
		t.Fatal("the teleport sent no position sync")
	}
	c.MoveTo(640.5, 64, 0.5, 0, 0)
	if !centred && !c.Pump(servertest.ExpectTimeout, nil, func(id int32, data []byte) bool {
		see(id, data)
		return centred
	}) {
		t.Fatal("the centre chunk never moved to (40, 0)")
	}
	acknowledge(c, 64)
	// Every batch to the end of the view: a queue that appended the new
	// area after the stale chunks would send the old position's chunks last.
	whole := (2*c.ChunkRadius + 1) * (2*c.ChunkRadius + 1)
	seen := map[[2]int32]bool{}
	first := true
	for packetID, data := range c.Packets(servertest.ExpectTimeout) {
		if packetID == protocol.IDPlayChunkBatchFinished {
			if int32(len(seen)) >= whole {
				return
			}
			acknowledge(c, 64)
			continue
		}
		if packetID != protocol.IDPlayChunkData {
			continue
		}
		r := protocol.NewReader(data)
		x, z := r.ReadI32(), r.ReadI32()
		if first && (x != 40 || z != 0) {
			t.Errorf("the first chunk after the move = (%d, %d), want the new centre (40, 0)", x, z)
		}
		first = false
		if x < 30 || x > 50 || z < -10 || z > 10 {
			t.Errorf("chunk (%d, %d) sent around the old position, want the view around (40, 0)", x, z)
		}
		seen[[2]int32{x, z}] = true
	}
	t.Fatalf("the view around the new position was not sent whole: %d of %d chunks", len(seen), whole)
}

// TestDimensionChangeKeepsThePacingAndSendsTheNewWorld checks that a
// crossing with a batch unanswered holds the new dimension's chunks until
// the answer, and that the chunks after it are the new dimension's.
func TestDimensionChangeKeepsThePacingAndSendsTheNewWorld(t *testing.T) {
	t.Parallel()
	srv, c := joinPaced(t, "Crosser")

	srv.MovePlayerToDimension("Crosser", world.NetherName, 8.5, 80, 8.5)
	c.AwaitPlay(protocol.IDPlayRespawn)
	quietFor(srv, c, pacingQuiet, func(id int32) {
		if id == protocol.IDPlayChunkBatchStart || id == protocol.IDPlayChunkData {
			t.Fatalf("packet 0x%02X arrived in the new dimension before the answer", id)
		}
	})

	acknowledge(c, 64)
	// The chunks the nether has generated by then are all that is ready.
	c.ExpectPlay(protocol.IDPlayChunkBatchStart)
	var got int32
	for packetID, data := range c.Packets(servertest.ExpectTimeout) {
		if packetID == protocol.IDPlayChunkBatchFinished {
			if got < 1 || got > 64 {
				t.Errorf("the first batch of the new dimension = %d chunks, want 1 to 64", got)
			}
			return
		}
		if packetID != protocol.IDPlayChunkData {
			continue
		}
		got++
		r := protocol.NewReader(data)
		x, z := r.ReadI32(), r.ReadI32()
		if old, ok := c.JoinChunks[[2]int32{x, z}]; ok && bytes.Equal(old, data) {
			t.Errorf("chunk (%d, %d) is byte for byte the overworld's, want the nether's", x, z)
		}
	}
	t.Fatal("the new dimension's batch did not finish")
}

// TestRaisedRenderDistanceSendsChunksStillWaitingForTheirHold checks that a
// client who joins at a small render distance and then raises it is sent the
// whole larger square, including the chunks the server had queued to hold but
// not yet taken.
func TestRaisedRenderDistanceSendsChunksStillWaitingForTheirHold(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Dial(t, cfg)
	c.ViewAsk = 2
	c.Login("Raiser")
	c.Configure()
	held := map[[2]int32]struct{}{}
	var centre [2]int32
	c.ReadJoin(func(id int32, data []byte) {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayCenterChunk:
			centre = [2]int32{r.ReadVarInt(), r.ReadVarInt()}
		case protocol.IDPlayChunkData:
			held[[2]int32{r.ReadI32(), r.ReadI32()}] = struct{}{}
		}
	})
	c.MarkLoaded()

	// The simulation distance is 10, so the join queued the ring beyond view 2
	// for takeHolds at four chunks a beat; the raise lands long before it is done.
	setRenderDistance(c, 10)

	// Every chunk of the 21x21 square of view 10 must arrive, the ones the
	// join sent and the ones sent after the raise alike.
	missing := func() int {
		n := 0
		for x := centre[0] - 10; x <= centre[0]+10; x++ {
			for z := centre[1] - 10; z <= centre[1]+10; z++ {
				if _, ok := held[[2]int32{x, z}]; !ok {
					n++
				}
			}
		}
		return n
	}
	deadline := time.Now().Add(60 * time.Second)
	for missing() > 0 {
		id, data, err := c.ReadBy(deadline)
		if err != nil {
			t.Fatalf("after raising to view 10 %d chunks of the square around %v never arrived: %v", missing(), centre, err)
		}
		switch id {
		case protocol.IDPlayKeepAlive:
			c.AnswerKeepAlive(data)
		case protocol.IDPlayChunkData:
			r := protocol.NewReader(data)
			held[[2]int32{r.ReadI32(), r.ReadI32()}] = struct{}{}
		}
	}
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

// TestAStillPlayerIsSentEachChunkOnce checks that a player who stands still
// is sent each chunk of the view once, and nothing of the chunk stream after
// it: the beat's pass over a view that has not changed is a no-op.
func TestAStillPlayerIsSentEachChunkOnce(t *testing.T) {
	t.Parallel()
	srv, c := joinPaced(t, "Statue")

	// The join's first batch carried nine of the 441.
	acknowledge(c, 64)
	readView(c, 441-9)
	// Twenty beats with the view whole.
	quietFor(srv, c, 20, func(id int32) {
		switch id {
		case protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkData,
			protocol.IDPlayUnloadChunk, protocol.IDPlayCenterChunk:
			t.Fatalf("packet 0x%02X of the chunk stream arrived for a still player with a whole view", id)
		}
	})
}

// TestACommandTeleportMovesTheViewWithoutAMove checks that a teleport by
// command moves the view of a client that reports no movement after it: the
// chunk sender's beat follows the body, not the client's moves.
// vanilla: ChunkMap.tick, updateChunkTracking
func TestACommandTeleportMovesTheViewWithoutAMove(t *testing.T) {
	t.Parallel()
	_, c := joinPaced(t, "Pawn", servertest.OpsListing(t, "Pawn"), func(s *server.Server) { warm(t, s, 1600) })

	acknowledge(c, 64)
	readView(c, 441-9)
	c.MarkLoaded()
	c.ChatCommand("tp @s 1600.5 64 0.5")
	if !c.Pump(servertest.ExpectTimeout, nil, func(id int32, data []byte) bool {
		if id != protocol.IDPlayCenterChunk {
			return false
		}
		r := protocol.NewReader(data)
		return r.ReadVarInt() == 100 && r.ReadVarInt() == 0
	}) {
		t.Fatal("the centre chunk never moved to (100, 0) without a reported move")
	}
}
