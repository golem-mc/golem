package servertest

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/golem-mc/golem/internal/config"
	"iter"
	"os"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
)

// PushedPacket is one packet held in Client.pushback.
type PushedPacket struct {
	Id   int32
	Data []byte
}

// ExpectTimeout is the hang guard of a wait for a packet when the caller names
// no window of its own. A wait ends as the packet comes, so the guard costs an
// idle machine nothing; it is generous because a loaded one can take tens of
// seconds over a dimension's first chunks, which the tick generates in step
// with a portal arrival.
const ExpectTimeout = time.Minute

// SettlePing is the payload a settle's Ping Request carries. The server
// echoes it, so the wait can tell this settle's answer from the response to a
// test's own fire-and-forget ping, which may still be unread on the wire
// ahead of it.
const SettlePing = 1

// ErrWaitOver is what a read that ran out of time fails with.
var ErrWaitOver = os.ErrDeadlineExceeded

// Read returns the next packet, waiting up to ExpectTimeout for it.
func (c *Client) Read() (int32, []byte, error) {
	return c.ReadBy(time.Now().Add(ExpectTimeout))
}

// ReadBy returns the next packet, the pushed-back ones first, or an error
// matching ErrWaitOver once the deadline passes. Only the wait for the
// packet's first byte is under the deadline: one that fell mid-packet would
// leave the stream in the middle of it, so a packet that stops partway is a
// broken stream, not the end of a wait. The data is the caller's to keep.
func (c *Client) ReadBy(deadline time.Time) (int32, []byte, error) {
	if len(c.Pushback) > 0 {
		p := c.Pushback[0]
		c.Pushback = c.Pushback[1:]
		return p.Id, p.Data, nil
	}
	if err := c.Conn.SetReadDeadline(deadline); err != nil {
		return 0, nil, err
	}
	if _, err := c.Conn.Peek(1); err != nil {
		return 0, nil, err
	}
	if err := c.Conn.SetReadDeadline(time.Now().Add(ExpectTimeout)); err != nil {
		return 0, nil, err
	}
	id, data, err := c.Conn.ReadPacket()
	if err != nil {
		return 0, nil, fmt.Errorf("packet cut short: %v", err)
	}
	if id == protocol.IDPlayLogin && c.Playing {
		// Read off here rather than in ReadJoin: a test may read the Login
		// itself and leave the rest of the join to ReadJoin.
		r := protocol.NewReader(data)
		r.ReadI32() // entity ID
		r.ReadBool()
		for range r.ReadVarInt() {
			r.ReadString(32767)
		}
		r.ReadVarInt() // max players
		serverView := r.ReadVarInt()
		if err := r.Err(); err != nil {
			c.T.Fatalf("login (play): %v", err)
		}
		c.ChunkRadius = min(serverView, max(int32(c.ViewAsk), config.MinDistance))
	}
	// A finished chunk batch is answered (play phase only: configuration Store
	// Cookie shares the ID), the way a client that wants the
	// rest of its chunks does; the answer keeps the paced stream flowing.
	// It asks for the most a client may, so a join's chunks arrive in a few
	// ticks rather than at the starting rate's nine a tick.
	// The pacing tests turn this off and answer the batches themselves.
	// vanilla: ChunkBatchSizeCalculator.getDesiredChunksPerTick (1 to 64)
	if c.Playing && id == protocol.IDPlayChunkBatchFinished && !c.NoBatchAcks {
		c.Send(protocol.IDPlayChunkBatchReceived, func(b *protocol.Buffer) {
			b.PutF32(64)
		})
	}
	if c.PlayerAttributes(id, data) {
		return c.ReadBy(deadline)
	}
	// A recipe book grant rides the loop's clock like a time sync, so it can
	// fall between any two packets a reader is waiting for; only the tests
	// that watch the book (WatchBook) see it.
	if c.Playing && !c.WatchBook && id == protocol.IDPlayRecipeBookAdd {
		return c.ReadBy(deadline)
	}
	if c.Playing && !c.WatchAdvancements && (id == protocol.IDPlayUpdateAdvancements || isAdvancementAnnouncement(id, data)) {
		return c.ReadBy(deadline)
	}
	if c.Playing && !c.WatchBundles && id == protocol.IDPlayBundleDelimiter {
		return c.ReadBy(deadline)
	}
	if !c.WatchWaypoints && id == protocol.IDPlayWaypoint {
		c.WaypointBacklog = append(c.WaypointBacklog, PushedPacket{Id: id, Data: data})
		return c.ReadBy(deadline)
	}
	if id == protocol.IDPlaySyncPosition {
		c.AcceptTeleport(data)
	}
	return id, data, nil
}

// PlayerAttributes notes the player's own entity and the player bodies
// spawned for this client, and reports whether a packet is a player's
// Update Attributes the reader is to drop. Nothing is filtered before the
// play phase's Login, the IDs meaning other things until then.
func (c *Client) PlayerAttributes(id int32, data []byte) bool {
	if c.PlayerEntityID == 0 {
		if id == protocol.IDPlayLogin && c.Playing {
			c.PlayerEntityID = protocol.NewReader(data).ReadI32()
		}
		return false
	}
	switch id {
	case protocol.IDPlaySpawnEntity:
		r := protocol.NewReader(data)
		entity := r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() == protocol.EntityTypePlayer && r.Err() == nil {
			if c.bodies == nil {
				c.bodies = map[int32]bool{}
			}
			c.bodies[entity] = true
		}
	case protocol.IDPlayUpdateAttributes:
		entity := protocol.NewReader(data).ReadVarInt()
		return !c.SeePlayerAttributes && (entity == c.PlayerEntityID || c.bodies[entity] || c.players[entity])
	}
	return false
}

// AcceptTeleport answers a Synchronize Player Position with Accept
// Teleportation as it is read, ahead of anything the test sends after, as a
// vanilla client does: the server ignores moves until then. No other state
// has a clientbound packet with this ID. The body moves as the packet says,
// relative axes and all. A failed write is left for the next read to report.
func (c *Client) AcceptTeleport(data []byte) {
	id := protocol.NewReader(data).ReadVarInt()
	c.ApplyTeleport(data)
	var b protocol.Buffer
	b.PutVarInt(id)
	b.PutF64(c.pos.X)
	b.PutF64(c.pos.Y)
	b.PutF64(c.pos.Z)
	b.PutF32(c.pos.Yaw)
	b.PutF32(c.pos.Pitch)
	_ = c.Conn.WritePacket(protocol.IDPlayConfirmTeleport, b.Bytes())
}

// ApplyTeleport moves the client's body to what a Synchronize Player Position
// says: a relative axis is added to where the body stands, and the pitch is
// clamped the way Entity.setXRot clamps it. The packet's own value is kept in
// LastTeleport, which ExpectTeleport reports; pos keeps the clamped pitch the
// next relative move measures from. vanilla: Entity.setXRot, RelativeMovement
func (c *Client) ApplyTeleport(data []byte) {
	r := protocol.NewReader(data)
	r.ReadVarInt() // teleport ID
	pos := player.Position{X: r.ReadF64(), Y: r.ReadF64(), Z: r.ReadF64()}
	r.ReadF64() // velocity
	r.ReadF64()
	r.ReadF64()
	pos.Yaw, pos.Pitch = r.ReadF32(), r.ReadF32()
	flags := r.ReadI32()
	if err := r.Err(); err != nil {
		c.T.Fatalf("synchronize player position: %v", err)
	}
	cur := c.pos
	if flags&0x01 != 0 {
		pos.X += cur.X
	}
	if flags&0x02 != 0 {
		pos.Y += cur.Y
	}
	if flags&0x04 != 0 {
		pos.Z += cur.Z
	}
	if flags&0x08 != 0 {
		pos.Yaw += cur.Yaw
	}
	if flags&0x10 != 0 {
		pos.Pitch += cur.Pitch
	}
	c.LastTeleport = pos
	c.pos = pos
	if c.pos.Pitch > 90 {
		c.pos.Pitch = 90
	}
	if c.pos.Pitch < -90 {
		c.pos.Pitch = -90
	}
}

// Unread puts packets back in front of whatever is still to be read, in
// their order, for the next reader.
func (c *Client) Unread(packets ...PushedPacket) {
	c.Pushback = append(packets, c.Pushback...)
}

// Packets yields the play packets that arrive within d, answering the
// keep-alives among them itself. The loop ends when d runs out; a broken
// connection fails the test.
func (c *Client) Packets(d time.Duration) iter.Seq2[int32, []byte] {
	deadline := time.Now().Add(d)
	return func(yield func(int32, []byte) bool) {
		// With the loop body's own Helper call, a failure in the body is
		// reported at the caller of the helper the loop is in.
		c.T.Helper()
		for {
			id, data, err := c.ReadBy(deadline)
			if errors.Is(err, ErrWaitOver) {
				return
			}
			if err != nil {
				c.T.Fatalf("read packet: %v", err)
			}
			if id == protocol.IDPlayKeepAlive {
				c.AnswerKeepAlive(data)
				continue
			}
			if !yield(id, data) {
				return
			}
		}
	}
}

// Pump hands consume every packet within d that skip does not name, until
// consume says the wait is over, and reports whether it did. A nil skip
// steps over nothing.
func (c *Client) Pump(d time.Duration, skip func(int32) bool, consume func(id int32, data []byte) bool) bool {
	c.T.Helper()
	for id, data := range c.Packets(d) {
		c.T.Helper()
		if skip != nil && skip(id) {
			continue
		}
		if consume(id, data) {
			return true
		}
	}
	return false
}

// Settle waits until the server has acted on everything sent so far, by asking
// it for something it answers directly: a connection is read one packet at a
// time, so an answer to the last one means the ones before it are done with.
// What arrives before the answer is dropped.
func (c *Client) Settle() {
	c.T.Helper()
	c.SettleAndCollect(func(int32, []byte) {})
}

// SettleAndCollect is settle handing record every packet before the answer
// but the time syncs.
func (c *Client) SettleAndCollect(record func(id int32, data []byte)) {
	c.T.Helper()
	c.SettleAndCollectWithin(ExpectTimeout, record)
}

// SettleAndCollectWithin is SettleAndCollect with the caller's own wait for
// the ping answer: a test that races the server against a watchdog uses a
// wait past the watchdog, so a real hang is the watchdog's panic with the
// goroutine stacks rather than a ping timeout.
func (c *Client) SettleAndCollectWithin(d time.Duration, record func(id int32, data []byte)) {
	c.T.Helper()
	c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(SettlePing) })
	for id, data := range c.Packets(d) {
		c.T.Helper()
		switch id {
		case protocol.IDPlayPingResponse:
			if IsPingAnswer(id, data, SettlePing) {
				return
			}
			// A test's own ping, answered late and still on the wire:
			// not the answer this settle is waiting on.
		case protocol.IDPlayUpdateTime:
		default:
			record(id, data)
		}
	}
	c.T.Fatalf("no ping response within %s", d)
}

// IsPingAnswer reports whether a packet is a Ping Response carrying payload:
// how a settle tells its own ping's answer from one an earlier ping left on
// the wire.
func IsPingAnswer(id int32, data []byte, payload int64) bool {
	return id == protocol.IDPlayPingResponse && protocol.NewReader(data).ReadI64() == payload
}

// PumpTicks is ReadTicks for n ticks, kept for its callers.
func (c *Client) PumpTicks(srv *server.Server, n uint64, see func(id int32, data []byte) bool) bool {
	c.T.Helper()
	return c.ReadTicks(srv, int(n), see)
}

// PumpTicksSince is PumpTicks counting from the tick since names rather than
// from the call, so the packets read between the two spend none of the window
// the earlier tick opened.
func (c *Client) PumpTicksSince(srv *server.Server, since, n uint64, see func(id int32, data []byte) bool) bool {
	c.T.Helper()
	left := int64(since+n) - int64(srv.TicksRun())
	return c.ReadTicks(srv, int(max(left, 0)), see)
}

// SettleKeeping settles and pushes back everything the wait collected, so
// the readers that follow see the packets as they arrived. It is the shape
// for a send whose answer travels the mailbox while the test holds the tick
// loop still: the send is known to have been acted on, and nothing that
// raced its answer was lost.
func (c *Client) SettleKeeping() {
	c.T.Helper()
	var kept []PushedPacket
	c.SettleAndCollect(func(id int32, data []byte) {
		kept = append(kept, PushedPacket{Id: id, Data: data})
	})
	c.Unread(kept...)
}

// ReadUntil reads until a packet of the given ID arrives and returns it. The
// timeout is a stall guard: running out of it reports false, never a pass.
func ReadUntil(t *testing.T, c *Client, timeout time.Duration, want int32) ([]byte, bool) {
	t.Helper()
	for id, data := range c.Packets(timeout) {
		t.Helper()
		if id == want {
			return data, true
		}
	}
	return nil, false
}

// ReadUntilViewSent reads until the batch that completes a whole view of
// chunks finishes, counting from the latest Respawn so the chunks of the
// dimension left behind do not count. Chunk Batch Finished alone is not the
// end of a view: a paced batch holds only the chunks that are ready.
// ExpectTimeout runs from the latest chunk, not the call, so a view a loaded
// machine generates slowly is not cut short: only a stalled one fails.
func ReadUntilViewSent(t *testing.T, c *Client) ([]byte, bool) {
	t.Helper()
	const timeout = ExpectTimeout
	sent := int32(0)
	whole := (2*c.ChunkRadius + 1) * (2*c.ChunkRadius + 1)
	deadline := time.Now().Add(timeout)
	for {
		id, data, err := c.ReadBy(deadline)
		if errors.Is(err, ErrWaitOver) {
			return nil, false
		}
		if err != nil {
			t.Fatalf("read packet: %v", err)
		}
		switch id {
		case protocol.IDPlayKeepAlive:
			c.AnswerKeepAlive(data)
		case protocol.IDPlayRespawn:
			sent = 0
			deadline = time.Now().Add(timeout)
		case protocol.IDPlayChunkData:
			sent++
			deadline = time.Now().Add(timeout)
		case protocol.IDPlayChunkBatchFinished:
			if sent >= whole {
				return data, true
			}
		}
	}
}

// ExpectViewWithinTicks reads the destination's entire view, counting chunks
// from the latest Respawn so batches from the old dimension cannot complete it.
func (c *Client) ExpectViewWithinTicks(srv *server.Server, n int) {
	c.T.Helper()
	sent := int32(0)
	whole := (2*c.ChunkRadius + 1) * (2*c.ChunkRadius + 1)
	if !c.ReadTicks(srv, n, func(id int32, _ []byte) bool {
		switch id {
		case protocol.IDPlayRespawn:
			sent = 0
		case protocol.IDPlayChunkData:
			sent++
		case protocol.IDPlayChunkBatchFinished:
			return sent >= whole
		}
		return false
	}) {
		c.T.Fatalf("destination view did not arrive within %d ticks", n)
	}
}

// AwaitViewSent is ReadUntilViewSent for a view that is on its way: it waits
// on the event, and a view that never completes fails as stalled.
func AwaitViewSent(t *testing.T, c *Client) []byte {
	t.Helper()
	sent := int32(0)
	whole := (2*c.ChunkRadius + 1) * (2*c.ChunkRadius + 1)
	for id, data := range c.Stream() {
		switch id {
		case protocol.IDPlayRespawn:
			sent = 0
		case protocol.IDPlayChunkData:
			sent++
		case protocol.IDPlayChunkBatchFinished:
			if sent >= whole {
				return data
			}
		}
	}
	return nil // unreachable: Stream ends only by the caller or a failure
}

// ReadJoin hands see every packet of a join up to the end of its chunk
// stream: the batch the join opens with holds only a quota's worth of
// chunks, so the end of the view is the last chunk of it rather than the
// first batch's finish. The view's size comes from the render distance the
// client asked for bounded by the server's, which the Login packet carries.
// JoinTimeout runs from the latest chunk, not the call, so a seeded view a
// loaded machine generates slowly is not cut short: only a stalled one fails.
func (c *Client) ReadJoin(see func(id int32, data []byte)) {
	c.T.Helper()
	sawChunks := int32(0)
	deadline := time.Now().Add(JoinTimeout)
	for {
		id, data, err := c.ReadBy(deadline)
		if errors.Is(err, ErrWaitOver) {
			c.T.Fatalf("the join chunks stalled: none arrived for %s (saw %d, radius %d)", JoinTimeout, sawChunks, c.ChunkRadius)
		}
		if err != nil {
			c.T.Fatalf("read packet: %v", err)
		}
		switch id {
		case protocol.IDPlayKeepAlive:
			c.AnswerKeepAlive(data)
			continue
		case protocol.IDPlayChunkData:
			sawChunks++
			deadline = time.Now().Add(JoinTimeout)
		case protocol.IDPlayChunkBatchFinished:
			// Whole: the batch that carried the last chunk of the view is
			// the end of the join's stream, read to its finish so no
			// packet of it trails into whatever the test reads next.
			see(id, data)
			if c.ChunkRadius > 0 && sawChunks >= (2*c.ChunkRadius+1)*(2*c.ChunkRadius+1) {
				return
			}
			continue
		}
		see(id, data)
	}
}

// ExpectNext is expect for the play state: the keep-alives in front of the
// packet are answered, the time syncs stepped over, and anything else in
// front of it fails the test.
func (c *Client) ExpectNext(packetID int32) []byte {
	c.T.Helper()
	for id, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		if id == protocol.IDPlayUpdateTime && packetID != protocol.IDPlayUpdateTime {
			continue
		}
		if id != packetID {
			c.T.Fatalf("got packet 0x%02X, want 0x%02X", id, packetID)
		}
		return data
	}
	c.T.Fatalf("packet 0x%02X did not arrive within %s", packetID, ExpectTimeout)
	return nil
}

// isAdvancementAnnouncement reports whether a packet is the System Chat that
// tells every player an advancement was made, which any tick can send.
func isAdvancementAnnouncement(id int32, data []byte) bool {
	return id == protocol.IDPlaySystemChat && bytes.Contains(data, []byte("chat.type.advancement."))
}
