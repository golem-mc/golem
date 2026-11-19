package servertest

import (
	"errors"
	"fmt"
	"iter"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// Tests measure the game in ticks, never in wall time. Two modes drive the
// main loop:
//
//   - Free (the default): the loop runs at its own 20 a second. A wait is
//     counted in the ticks srv.TicksRun counts, so a slow machine is given
//     the ticks, not a share of a window.
//   - Held (HoldTicks): the loop runs a tick only when the test calls
//     RunTicks, as many as it asks for, back to back and as fast as the
//     machine goes. Tick counts are then exact.
//
// "Nothing arrives" is never a quiet window: the readers below run their
// ticks, then send a Ping Request and read up to its answer. The connection
// answers it through the same mailbox the ticks and the broadcasts queue to,
// so whatever the ticks sent, and whatever the packets the test sent before
// it caused, is on the wire in front of the answer.
//
// The wall clock is only the safety net: a loop that runs no tick, or a
// connection that sends nothing, for ExpectTimeout fails the test as stalled.
// Running out of that time never makes a test pass.

// TickBarrierPing is the payload of the barrier ping ReadTicks sends, told
// apart from SettlePing so the two can share a wire.
const TickBarrierPing = 2

// heldClock is the tick clock of a held loop: each Wait takes one permit, so
// each RunTicks permit is one tick, whatever the tick rate or a sprint
// would have the loop wait. It starts at the wall time of the hold, so the
// keep-alive and auto-save stamps taken before it stay behind it.
type heldClock struct {
	mu      sync.Mutex
	now     time.Time
	permits chan struct{}
}

// Now reads the time the loop last waited for, so the loop never sees
// itself behind and never drops a backlog.
func (c *heldClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *heldClock) Wait(t time.Time, done <-chan struct{}) bool {
	select {
	case <-c.permits:
	case <-done:
		return false
	}
	c.mu.Lock()
	if t.After(c.now) {
		c.now = t
	}
	c.mu.Unlock()
	return true
}

// heldLoops maps each held *server.Server to its clock.
var heldLoops sync.Map

func heldClockOf(srv *server.Server) *heldClock {
	if c, ok := heldLoops.Load(srv); ok {
		return c.(*heldClock)
	}
	return nil
}

// frozenJoins holds each *server.Server FreezeUntilHeld froze.
var frozenJoins sync.Map

// FreezeUntilHeld freezes srv's game (SetTickFrozen) until HoldTicks holds its
// loop, for a test whose join has to run on the free loop but whose world
// must not move before the hold: the join's ticks run the players alone, so
// no entity flies, no clock or timer advances and no chunk's saved entities
// wake. HoldTicks lifts the freeze once the loop is held, so the first tick
// the test runs is the world's first.
func FreezeUntilHeld(t *testing.T, srv *server.Server) {
	t.Helper()
	srv.SetTickFrozen(true)
	frozenJoins.Store(srv, true)
	t.Cleanup(func() { frozenJoins.Delete(srv) })
}

// HoldTicks restarts srv's main loop held: from here a tick runs only when
// RunTicks (or a reader below) asks for it. Join the players first: a join
// waits up to a second for two ticks to wake its chunks, and the tracking of
// other players only runs on a tick. The keep-alive exchange runs on the
// held ticks too, so a test must not sit idle with the loop held for longer
// than the keep-alive timeout. A freeze FreezeUntilHeld put on is lifted.
func HoldTicks(t *testing.T, srv *server.Server) {
	t.Helper()
	srv.StopTick()
	clock := &heldClock{now: time.Now(), permits: make(chan struct{}, 1)}
	srv.SetTickClock(clock, nil)
	heldLoops.Store(srv, clock)
	t.Cleanup(func() { heldLoops.Delete(srv) })
	srv.StartTick()
	if _, ok := frozenJoins.LoadAndDelete(srv); ok {
		srv.SetTickFrozen(false)
	}
}

// ReleaseTicks puts a held loop back on the wall clock at its own 20 a
// second, for a join in the middle of a held test; FreezeUntilHeld first
// keeps the world where the held ticks left it until the next HoldTicks.
func ReleaseTicks(t *testing.T, srv *server.Server) {
	t.Helper()
	if heldClockOf(srv) == nil {
		t.Fatal("releasing the main loop with it not held")
	}
	srv.StopTick()
	srv.SetTickClock(nil, nil)
	heldLoops.Delete(srv)
	srv.StartTick()
}

// SkipTime moves a held loop's clock d ahead without running a tick, the
// wall time a lagging server spends inside its ticks. Keep d under the
// loop's overload threshold (2s at 20 ticks a second), or the loop drops
// the backlog and logs a warning.
func SkipTime(t *testing.T, srv *server.Server, d time.Duration) {
	t.Helper()
	clock := heldClockOf(srv)
	if clock == nil {
		t.Fatal("skipping time with the main loop not held")
	}
	clock.mu.Lock()
	clock.now = clock.now.Add(d)
	clock.mu.Unlock()
}

// awaitEnd waits for ended to close, failing the test if no tick ends within
// the safety net.
func awaitEnd(t *testing.T, ended <-chan struct{}) {
	t.Helper()
	if ended == nil {
		t.Fatal("waiting on a tick with the main loop stopped")
	}
	timer := time.NewTimer(ExpectTimeout)
	defer timer.Stop()
	select {
	case <-ended:
	case <-timer.C:
		t.Fatalf("the main loop ran no tick for %s", ExpectTimeout)
	}
}

// RunTicks returns once n whole ticks have run after the call. A held loop
// runs exactly n; a free one is waited on for n+1 tick ends, the first of
// which may close a tick that started before the call.
func RunTicks(t *testing.T, srv *server.Server, n int) {
	t.Helper()
	if n <= 0 {
		return
	}
	if clock := heldClockOf(srv); clock != nil {
		for range n {
			ended := srv.TickEnded()
			clock.permits <- struct{}{}
			awaitEnd(t, ended)
		}
		return
	}
	for range n + 1 {
		awaitEnd(t, srv.TickEnded())
	}
}

// UntilTicks checks cond now and after each tick, and fails naming what once
// n ticks have run without it. On a held loop the ticks are run here, so cond
// has to be something a tick brings about; state a connection's own handling
// changes is waited on with that client's Settle first.
func UntilTicks(t *testing.T, srv *server.Server, n int, what string, cond func() bool) {
	t.Helper()
	if cond() {
		return
	}
	held := heldClockOf(srv) != nil
	for range n {
		if held {
			RunTicks(t, srv, 1)
		} else {
			awaitEnd(t, srv.TickEnded())
		}
		if cond() {
			return
		}
	}
	if !held {
		// A free loop's first end may have closed a tick begun before the
		// call: one more makes the budget n whole ticks.
		awaitEnd(t, srv.TickEnded())
		if cond() {
			return
		}
	}
	t.Fatalf("%s: not within %d ticks", what, n)
}

// TickReader is a test client as the tick-bounded readers drive it, so the
// in-package harness's client can share them.
type TickReader struct {
	T *testing.T
	// Conn is written to from a second goroutine on a free loop; its writes
	// are serialised.
	Conn *protocol.Conn
	// ReadBy returns the next packet, the pushed-back ones first, failing
	// with ErrWaitOver at the deadline.
	ReadBy          func(deadline time.Time) (int32, []byte, error)
	Unread          func(packets ...PushedPacket)
	AnswerKeepAlive func(data []byte)
}

func (c *Client) tickReader() TickReader {
	return TickReader{T: c.T, Conn: c.Conn, ReadBy: c.ReadBy, Unread: c.Unread, AnswerKeepAlive: c.AnswerKeepAlive}
}

func sendBarrier(conn *protocol.Conn) error {
	var b protocol.Buffer
	b.PutI64(TickBarrierPing)
	return conn.WritePacket(protocol.IDPlayPingRequest, b.Bytes())
}

// readToBarrier hands see every packet up to the barrier's answer, the
// keep-alives answered on the way. Once see has said done, the packets after
// are kept for the next reader. It reports whether see said done. The
// barrier is already on the wire, so a quiet connection is a stall.
func readToBarrier(r TickReader, done bool, see func(id int32, data []byte) bool) bool {
	r.T.Helper()
	var kept []PushedPacket
	// Overall, not per packet: a free loop's time syncs never let a busy
	// connection go quiet.
	deadline := time.Now().Add(ExpectTimeout)
	for {
		id, data, err := r.ReadBy(deadline)
		if err != nil {
			r.T.Fatalf("waiting for the barrier ping's answer: %v", err)
		}
		switch {
		case IsPingAnswer(id, data, TickBarrierPing):
			r.Unread(kept...)
			return done
		case id == protocol.IDPlayKeepAlive:
			r.AnswerKeepAlive(data)
		case done:
			kept = append(kept, PushedPacket{Id: id, Data: data})
		default:
			done = see(id, data)
		}
	}
}

// ReadTicksVia is ReadTicks for any client the TickReader describes.
func ReadTicksVia(r TickReader, srv *server.Server, n int, see func(id int32, data []byte) bool) bool {
	r.T.Helper()
	if n > 0 {
		// The ticks are counted from once the connection has handled what
		// the test sent before the call: on a loaded machine a packet can
		// wait behind the reader for ticks, and the effects its next tick
		// brings (a block ack, a death) would fall past the window.
		if err := sendBarrier(r.Conn); err != nil {
			r.T.Fatalf("send barrier ping: %v", err)
		}
		if readToBarrier(r, false, see) {
			return true
		}
	}
	if heldClockOf(srv) != nil {
		// Held: one tick, one barrier, so see stops at the very tick.
		for range max(n, 1) {
			RunTicks(r.T, srv, min(n, 1))
			if err := sendBarrier(r.Conn); err != nil {
				r.T.Fatalf("send barrier ping: %v", err)
			}
			if readToBarrier(r, false, see) {
				return true
			}
		}
		return false
	}

	// Free: the ticks run while the test reads, and a watcher sends the
	// barrier once they have. stopped, sent and failure are under mu, so a
	// barrier is either sent before the reader stops or not at all.
	var (
		mu      sync.Mutex
		stopped bool
		sent    bool
		sentAt  time.Time
		failure error
	)
	quit := make(chan struct{})
	stop := func() (barrierSent bool) {
		mu.Lock()
		defer mu.Unlock()
		if !stopped {
			stopped = true
			close(quit)
		}
		return sent
	}
	defer stop()
	go func() {
		err := func() error {
			if n == 0 {
				return nil
			}
			for range n + 1 {
				ended := srv.TickEnded()
				if ended == nil {
					return errors.New("the main loop is stopped")
				}
				timer := time.NewTimer(ExpectTimeout)
				select {
				case <-ended:
					timer.Stop()
				case <-quit:
					timer.Stop()
					return nil
				case <-timer.C:
					return fmt.Errorf("the main loop ran no tick for %s", ExpectTimeout)
				}
			}
			return nil
		}()
		mu.Lock()
		defer mu.Unlock()
		switch {
		case stopped:
		case err != nil:
			failure = err
		default:
			sent, sentAt = true, time.Now()
			failure = sendBarrier(r.Conn)
		}
	}()

	for {
		// Until the barrier is sent the watcher bounds the wait; once it
		// is, its answer is due within ExpectTimeout of the send, however
		// busy the connection.
		mu.Lock()
		deadline := time.Now().Add(ExpectTimeout)
		if sent {
			deadline = sentAt.Add(ExpectTimeout)
		}
		mu.Unlock()
		id, data, err := r.ReadBy(deadline)
		if errors.Is(err, ErrWaitOver) {
			mu.Lock()
			barrierSent, werr := sent, failure
			mu.Unlock()
			if werr != nil {
				r.T.Fatalf("tick barrier: %v", werr)
			}
			if barrierSent {
				r.T.Fatalf("the barrier ping went unanswered for %s", ExpectTimeout)
			}
			continue // still waiting on the ticks, which the watcher bounds
		}
		if err != nil {
			r.T.Fatalf("read packet: %v", err)
		}
		switch {
		case IsPingAnswer(id, data, TickBarrierPing):
			return false
		case id == protocol.IDPlayKeepAlive:
			r.AnswerKeepAlive(data)
			continue
		}
		if !see(id, data) {
			continue
		}
		if stop() {
			// The barrier is on the wire: read up to its answer, keeping
			// the rest, so no stray answer reaches the next reader.
			readToBarrier(r, true, see)
		}
		return true
	}
}

// ReadTicks hands see every packet that arrives while the next n ticks run,
// counted from once the connection has handled the packets the test sent
// before the call, up to a barrier ping sent once they have, until see says
// the wait is over;
// it reports whether see did. Packets after see's yes are kept for the next
// reader. A free loop may run a tick or two more before the barrier's
// answer; a held one runs exactly the ticks see needed, at most n. With n 0
// no tick runs: the barrier alone, the shape for an answer the connection
// sends itself.
func (c *Client) ReadTicks(srv *server.Server, n int, see func(id int32, data []byte) bool) bool {
	c.T.Helper()
	return ReadTicksVia(c.tickReader(), srv, n, see)
}

// ExpectWithinTicks reads until a packet of id arrives, failing once n ticks
// and the barrier behind them have passed without one; the packets before it
// are dropped.
func (r TickReader) ExpectWithinTicks(srv *server.Server, n int, id int32) []byte {
	r.T.Helper()
	var got []byte
	if !ReadTicksVia(r, srv, n, func(gotID int32, data []byte) bool {
		if gotID == id {
			got = data
			return true
		}
		return false
	}) {
		r.T.Fatalf("packet 0x%02X did not arrive within %d ticks", id, n)
	}
	return got
}

// ExpectNoneWithinTicks runs n ticks and the barrier behind them and fails
// on any packet bad picks out. A nil bad fails on any packet at all.
func (r TickReader) ExpectNoneWithinTicks(srv *server.Server, n int, bad func(id int32, data []byte) bool) {
	r.T.Helper()
	ReadTicksVia(r, srv, n, func(id int32, data []byte) bool {
		if bad == nil || bad(id, data) {
			r.T.Fatalf("packet 0x%02X arrived within %d ticks, want none: % X", id, n, data)
		}
		return false
	})
}

// ExpectWithinTicks is TickReader.ExpectWithinTicks on this client.
func (c *Client) ExpectWithinTicks(srv *server.Server, n int, id int32) []byte {
	c.T.Helper()
	return c.tickReader().ExpectWithinTicks(srv, n, id)
}

// ExpectNoneWithinTicks is TickReader.ExpectNoneWithinTicks on this client.
func (c *Client) ExpectNoneWithinTicks(srv *server.Server, n int, bad func(id int32, data []byte) bool) {
	c.T.Helper()
	c.tickReader().ExpectNoneWithinTicks(srv, n, bad)
}

// PacketIs picks out the packets of the listed ids, for ExpectNoneWithinTicks.
func PacketIs(ids ...int32) func(id int32, data []byte) bool {
	return func(id int32, _ []byte) bool {
		return slices.Contains(ids, id)
	}
}

// PacketAbout picks out the packets of the listed ids whose leading VarInt
// is entityID, for ExpectNoneWithinTicks.
func PacketAbout(entityID int32, ids ...int32) func(id int32, data []byte) bool {
	return func(id int32, data []byte) bool {
		return slices.Contains(ids, id) && protocol.NewReader(data).ReadVarInt() == entityID
	}
}

// StreamTimeout bounds a whole Stream. The deadline is overall, not per
// packet: a free loop's time syncs keep the connection busy, so a per-read
// deadline would never fire on a packet that never comes.
const StreamTimeout = 2 * ExpectTimeout

// Stream yields the play packets as they arrive, the keep-alives answered,
// for a reader that waits on an event and not a window: it ends when the
// caller stops, and fails the test once StreamTimeout has passed without
// the caller stopping.
func (c *Client) Stream() iter.Seq2[int32, []byte] {
	return func(yield func(int32, []byte) bool) {
		c.T.Helper()
		limit := StreamTimeout
		if c.streamTimeout > 0 {
			limit = c.streamTimeout
		}
		deadline := time.Now().Add(limit)
		for {
			id, data, err := c.ReadBy(deadline)
			if errors.Is(err, ErrWaitOver) {
				c.T.Fatalf("the awaited packet did not come within %s", limit)
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

// AwaitServed waits for the server to finish with the connection after it
// ended it: the player off the list and saved.
func (c *Client) AwaitServed() {
	c.T.Helper()
	AwaitServed(c.T, c.Served)
}

// AwaitServed is Client.AwaitServed for a client that keeps its own served
// channel.
func AwaitServed(t *testing.T, served <-chan struct{}) {
	t.Helper()
	timer := time.NewTimer(ExpectTimeout)
	defer timer.Stop()
	select {
	case <-served:
	case <-timer.C:
		t.Fatalf("the server did not finish with the connection within %s", ExpectTimeout)
	}
}

// AwaitPacket reads until a packet of id arrives, dropping the others, for a
// packet the test knows is coming; one not there within StreamTimeout fails
// the test.
func (c *Client) AwaitPacket(id int32) []byte {
	c.T.Helper()
	for got, data := range c.Stream() {
		if got == id {
			return data
		}
	}
	return nil // unreachable: Stream ends only by the caller or a failure
}

// KeepAlivePingTicks and KeepAliveWaitTicks are the keep-alive timings the
// tests run the exchange on, in ticks of the held loop: a ping every tick,
// dropped once one has stayed unanswered for KeepAliveWaitTicks.
const (
	KeepAlivePingTicks = 1
	KeepAliveWaitTicks = 40
)

// HoldKeepAlive holds srv's loop and puts the exchange on the timings above.
// It is called once the player is in, so a slow join is not held to them.
func HoldKeepAlive(t *testing.T, srv *server.Server, timeoutTicks int) {
	t.Helper()
	HoldTicks(t, srv)
	srv.SetKeepAlive(KeepAlivePingTicks*time.Second/20, time.Duration(timeoutTicks)*time.Second/20)
}
