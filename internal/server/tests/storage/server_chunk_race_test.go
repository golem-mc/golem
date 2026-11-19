package storage

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestRenderDistanceChangesWhileCrossingChunks walks a client east across
// chunk borders while changing its render distance at every step, the way a
// player fiddling with their options would while travelling. Applying the new
// distance is the read loop's work and acting on it the chunk sender's, which
// are two different goroutines, so the race detector is what sees whether the
// two stay out of each other's chunk bookkeeping.
//
// The walk ends at chunk (199, 0) with a render distance of 3, and the test
// then checks the client holds exactly the chunks of that final view: not
// only that the changes did not trip over each other, but that the last of
// them took effect.
func TestRenderDistanceChangesWhileCrossingChunks(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	// The walk floods the chunk sender with work, and everything it does is
	// slow under the race detector - slowly enough that the real keep-alive
	// rhythm would drop the client mid-test over an answer the read loop has not
	// reached yet. What becomes of the connection when a client stops
	// answering is the keep-alive tests' business; this one leaves the
	// exchange out.
	srv, c := servertest.Join(t, cfg, "Racer", func(s *server.Server) {
		s.SetKeepAlive(time.Hour, servertest.DefaultKeepAliveTimeout)
	})

	// One goroutine reads everything the server sends, keeping the set of
	// chunks the client has been sent less the ones told to unload, and
	// answering any keep-alive that arrives along the way. It is also the
	// barrier for the end of the walk: the server echoes a ping back after
	// the packet that asked for it, so the response to one sent last says
	// the read loop has handled everything sent before it.
	//
	// The reads wait far longer than the harness's own expectTimeout: a run of
	// the chunk sender can spend minutes under the race detector before its
	// first packet of a batch goes out, and the reader quitting halfway
	// would leave the walk's outcome unread rather than settled.
	const finalPing = int64(0x5AFE1)
	var mu sync.Mutex
	loaded := make(map[world.ChunkPos]struct{})
	pinged := make(chan struct{})
	// changed wakes the final wait after each chunk or unload the reader sees.
	changed := make(chan struct{}, 1)
	wake := func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}
	go func() {
		for {
			id, data, err := c.ReadBy(time.Now().Add(2 * time.Minute))
			if err != nil {
				return
			}
			switch id {
			case protocol.IDPlayKeepAlive:
				c.AnswerKeepAlive(data)
			case protocol.IDPlayPingResponse:
				if protocol.NewReader(data).ReadI64() == finalPing {
					close(pinged)
				}
			case protocol.IDPlayChunkData:
				// The chunk coordinates lead the packet.
				r := protocol.NewReader(data)
				x, z := r.ReadI32(), r.ReadI32()
				if r.Err() == nil {
					mu.Lock()
					loaded[world.ChunkPos{X: x, Z: z}] = struct{}{}
					mu.Unlock()
					wake()
				}
			case protocol.IDPlayUnloadChunk:
				// Z comes first, the client reading the pair as one
				// big-endian long.
				r := protocol.NewReader(data)
				z, x := r.ReadI32(), r.ReadI32()
				if r.Err() == nil {
					mu.Lock()
					delete(loaded, world.ChunkPos{X: x, Z: z})
					mu.Unlock()
					wake()
				}
			}
		}
	}()

	// Two hundred steps of a render distance that keeps changing, each with
	// a step into the next chunk column. The last one ends at x = 3184.5,
	// chunk (199, 0), with a render distance of 3: the view the client is
	// left holding.
	const steps = 200
	lastX, lastDist := 0.0, int8(0)
	settings := func(b *protocol.Buffer) {
		b.PutString("en_GB")
		b.PutI8(lastDist)
		b.PutVarInt(0)
		b.PutBool(true)
		b.PutU8(0x7F)
		b.PutVarInt(1)
		b.PutBool(false)
		b.PutBool(true)
		b.PutVarInt(0)
	}
	for i := range steps {
		lastDist = int8(2 + i%6)
		c.Send(protocol.IDPlayClientInformation, settings)
		lastX = float64(i*16) + 0.5
		c.MoveTo(lastX, 64, 0.5, 0, 0)
	}

	// One more of the same, beyond the walk. The sender works through the
	// walk's asks one at a time, and an ask that arrives while it is busy
	// only leaves a token if the token slot is free, so the ones the walk
	// queued behind a send in progress are all the sender needs - but the
	// last of them may have found the slot full and gone. This one cannot:
	// by the time the read loop is through the walk, either the slot holds a
	// token or the sender is idle, and either way one more send happens and
	// reads the player where they ended up. The settings are the final
	// step's, so the send it asks for is the one the last view needs.
	c.Send(protocol.IDPlayClientInformation, settings)
	c.MoveTo(lastX, 64, 0.5, 0, 0)
	c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(finalPing) })

	// The echo says the read loop has handled everything, nudge included;
	// the send it asked for may still be under way, so the final view is
	// waited for rather than expected at once.
	select {
	case <-pinged:
	case <-time.After(90 * time.Second):
		t.Fatalf("the server had not answered after the last of %d steps within 90s", steps)
	}

	center := world.ChunkPos{X: world.ChunkOf(lastX), Z: 0}
	radius := int32(lastDist)
	if srv.Config.ViewDistance < int(radius) {
		radius = int32(srv.Config.ViewDistance)
	}
	want := (2*radius + 1) * (2*radius + 1)
	stall := time.NewTimer(90 * time.Second)
	defer stall.Stop()
	for {
		held, matched := 0, 0
		mu.Lock()
		held = len(loaded)
		for x := center.X - radius; x <= center.X+radius; x++ {
			for z := center.Z - radius; z <= center.Z+radius; z++ {
				if _, found := loaded[world.ChunkPos{X: x, Z: z}]; found {
					matched++
				}
			}
		}
		mu.Unlock()
		if held == int(want) && matched == int(want) {
			return
		}
		select {
		case <-changed:
			continue
		case <-stall.C:
		}
		break
	}
	mu.Lock()
	held := len(loaded)
	mu.Unlock()
	t.Fatalf("after the walk the client holds %d chunks, want the %d of the %dx%d square around %v",
		held, want, 2*radius+1, 2*radius+1, center)
}
