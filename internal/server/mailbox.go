package server

import (
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// mailboxLimit is the bytes that may wait for a client before it counts as
// not reading. Vanilla queues without limit, leaving a stalled client to
// the keep-alive timeout (a burst of mob deaths is thousands of packets);
// this only stops a never-reading client holding memory until then.
const mailboxLimit = 32 << 20

// outbound is a packet waiting to be written to a client.
type outbound struct {
	id int32
	// playerChat keeps converted System Chat subject to full-chat visibility.
	playerChat bool
	data       []byte
	shared     *protocol.CachedPacket
	// raw skips compression: set for a body deflate cannot shrink.
	raw bool
	// lazy: queued without waking the writer; it goes with the next flush
	// (the tick end or any other wake).
	lazy bool
}

// mailbox is a client's queue of packets from goroutines other than its
// connection's (broadcasts, the tick loop). Each connection drains its own,
// so a client that stopped reading cannot hold up others for a write
// timeout. Direct replies flush the queue first, preserving packet order.
type mailbox struct {
	c *client

	// writing is held by the writer around each batch and by a dimension change
	// for its whole send-off; taken before mu.
	writing sync.Mutex
	held    atomic.Bool

	// holdSeam, test-only, signals the next holdWriter acquisition.
	holdSeam atomic.Pointer[func()]

	// mu guards queue, queued (bytes) and closing.
	mu    sync.Mutex
	queue []outbound
	spare []outbound // the last batch's storage, emptied, for the next queue
	// Consecutive flushes of each backing slice that used under a quarter of
	// its oversized capacity; they move with the slices and reset when one is dropped.
	queueSlack int
	spareSlack int
	queued     int
	closing    bool
	wake       chan struct{}

	// ready is closed once the client is in the world. A player registers
	// before the world is sent, so nothing is missed, and packets queue until
	// the client knows where it is.
	ready     chan struct{}
	readyOnce sync.Once
	released  atomic.Bool

	done chan struct{}
	once sync.Once
}

// startMailbox starts the writer goroutine, writing nothing until release;
// it ends on stop or a write error.
func (c *client) startMailbox() *mailbox {
	m := &mailbox{
		c:     c,
		wake:  make(chan struct{}, 1),
		ready: make(chan struct{}),
		done:  make(chan struct{}),
	}

	go func() {
		select {
		case <-m.done:
			return
		case <-m.ready:
		}
		for {
			select {
			case <-m.done:
				return
			case <-m.wake:
			}
			if !m.writeBatch() {
				return
			}
			// A client marked for closing goes once the queue is empty: a server
			// disconnect's reason must be written first, and a write stalling on a
			// non-reading client holds up only this writer.
			m.mu.Lock()
			closing, drained := m.closing, len(m.queue) == 0
			m.mu.Unlock()
			if closing && drained {
				c.conn.Close()
				return
			}
		}
	}()

	return m
}

const (
	spareMin          = 1024  // capacity kept without question
	spareMax          = 16384 // capacity never kept
	spareSlackFlushes = 8
)

// writeBatch writes everything waiting, later arrivals going next round;
// it returns false once the writer should stop.
func (m *mailbox) writeBatch() bool {
	m.writing.Lock()
	defer m.writing.Unlock()
	m.mu.Lock()
	batch := m.queue
	slack := m.queueSlack
	m.queue, m.queueSlack = m.spare, m.spareSlack
	m.spare, m.spareSlack = nil, 0
	m.queued = 0
	m.mu.Unlock()
	select {
	case <-m.done:
		return false
	default:
	}
	err := m.c.conn.WritePacketsRaw(len(batch), func(i int) (int32, []byte, *protocol.CachedPacket, bool) {
		return batch[i].id, batch[i].data, batch[i].shared, batch[i].raw
	})
	// A busy client refills the same capacity every tick, so keep it up to
	// spareMax; a join's chunk burst is released after spareSlackFlushes
	// flushes of this slice that need little of it.
	if cap(batch) > spareMin {
		if len(batch) <= cap(batch)/4 {
			slack++
		} else {
			slack = 0
		}
	}
	if cap(batch) <= spareMax && (cap(batch) <= spareMin || slack < spareSlackFlushes) {
		clear(batch) // do not pin the payloads
		m.mu.Lock()
		m.spare, m.spareSlack = batch[:0], slack
		m.mu.Unlock()
	}
	if err != nil {
		// The read loop reports the connection ending.
		m.c.log.Debug("stopped writing to client", "error", err)
		m.c.conn.Close()
		return false
	}
	return true
}

// send reports whether the packet was accepted, including a chat-visibility drop.
func (c *client) send(packetID int32, data []byte) bool { return c.sendIn(nil, packetID, data) }

// sendImmediate queues a reply that must keep flowing even during a held tick.
// Waking the writer also preserves the order of packets already queued.
func (c *client) sendImmediate(packetID int32, data []byte) bool {
	return c.sendImmediateOutbound(outbound{id: packetID, data: data})
}

func (c *client) sendImmediateOutbound(p outbound) bool {
	if !c.sendOutboundIn(nil, p) {
		return false
	}
	c.out.signal()
	return true
}

// sendIn queues a packet about world w, dropping it if the client is no
// longer in w: a body paired before a dimension change but queued after
// would otherwise follow the Respawn into the new world. A nil w concerns
// no world. A client with over mailboxLimit bytes waiting is dropped.
func (c *client) sendIn(w *world.World, packetID int32, data []byte) bool {
	return c.sendOutboundIn(w, outbound{id: packetID, data: data})
}

func (c *client) sendOutboundIn(w *world.World, p outbound) bool {
	p, ok := c.filterOutbound(p)
	if !ok {
		return true
	}
	return c.queueIn(w, p)
}

// filterOutbound applies the per-packet filters; ok is false for a packet
// this client is not sent.
func (c *client) filterOutbound(p outbound) (_ outbound, ok bool) {
	if p.playerChat && !c.acceptsChatMessages() {
		return p, false
	}
	if c.sky.Load() != nil {
		data, ok := c.skyFilter(p.id, p.data)
		if !ok {
			return p, false
		}
		p.data = data
	}
	// vanilla: ServerPlayer.acceptsSystemMessages
	if p.id == protocol.IDPlaySystemChat && c.chatMode.Load() == protocol.ChatModeHidden && len(p.data) > 0 && p.data[len(p.data)-1] == 0 {
		return p, false
	}
	return p, true
}

// sendOutboundsIn queues the entries, all bound for c, in order under one
// mailbox lock, with the filters, world check and limit of sendOutboundIn.
// It reuses entries as scratch.
func (c *client) sendOutboundsIn(entries []sendEntry) {
	m := c.out
	if m == nil {
		return
	}
	kept := entries[:0]
	for _, e := range entries {
		if p, ok := c.filterOutbound(e.p); ok {
			e.p = p
			kept = append(kept, e)
		}
	}
	if len(kept) == 0 {
		return
	}
	wake := false
	soon := c.srv.tickFlushSoon()
	m.mu.Lock()
	for _, e := range kept {
		if e.w != nil && c.theWorld() != e.w {
			continue
		}
		if queued := m.queued; queued+len(e.p.data) > mailboxLimit {
			m.mu.Unlock()
			c.log.Warn("dropping client that is not keeping up", "queuedBytes", queued)
			c.conn.Close()
			return
		}
		m.queue = append(m.queue, e.p)
		m.queued += len(e.p.data)
		wake = wake || !e.p.lazy || !soon
	}
	m.mu.Unlock()
	// Read after enqueue, as in queueIn.
	if wake && !c.srv.deferWakes.Load() {
		m.signal()
	}
}

// sendUnfiltered bypasses chat visibility for the hidden-chat refusal itself.
func (c *client) sendUnfiltered(packetID int32, data []byte) bool {
	return c.queueIn(nil, outbound{id: packetID, data: data})
}

// queueIn applies world and capacity checks before enqueueing a packet.
func (c *client) queueIn(w *world.World, p outbound) bool {
	data := p.data
	m := c.out
	if m == nil {
		return false
	}
	m.mu.Lock()
	if w != nil && c.theWorld() != w {
		m.mu.Unlock()
		return false
	}
	if queued := m.queued; queued+len(data) > mailboxLimit {
		m.mu.Unlock()
		c.log.Warn("dropping client that is not keeping up", "queuedBytes", queued)
		c.conn.Close()
		return false
	}
	m.queue = append(m.queue, p)
	m.queued += len(data)
	m.mu.Unlock()
	// Read after enqueue: a racing tick-end sweep either sees the packet or
	// leaves wakes enabled for this send.
	if !c.srv.deferWakes.Load() && (!p.lazy || !c.srv.tickFlushSoon()) {
		m.signal()
	}
	return true
}

// tickFlushSoon reports whether a tick end will flush the queues within a
// tick: the loop runs on the wall clock. A stopped loop or a test clock
// may never tick.
func (s *Server) tickFlushSoon() bool {
	return s.loopRunning() && !s.hasTickClock()
}

// sendAllIn queues packets about world w, in order; see sendIn.
func (c *client) sendAllIn(w *world.World, packets []outbound) {
	for _, p := range packets {
		c.sendOutboundIn(w, p)
	}
}

// shareOutbound avoids cache overhead below the default compression threshold
// unless at least sixteen recipients can reuse the framing work.
func shareOutbound(p *outbound, recipients int) {
	if p.shared == nil && (len(p.data) >= 256 || recipients >= 16) {
		p.shared = protocol.NewCachedPacket(p.id, p.data)
	}
}

func sendToAll(w *world.World, recipients []*client, p outbound) {
	if len(recipients) > 1 {
		shareOutbound(&p, len(recipients))
	}
	for _, c := range recipients {
		c.sendOutboundIn(w, p)
	}
}

// sendPacketsToAll prepares shared frames before any recipient can read them.
func sendPacketsToAll(w *world.World, recipients []*client, packets []outbound) {
	if len(recipients) > 1 {
		for i := range packets {
			shareOutbound(&packets[i], len(recipients))
		}
	}
	for _, c := range recipients {
		c.sendAllIn(w, packets)
	}
}

// holdWriter holds off the writer until the returned release and flushes
// the queue, so the caller's direct writes come after everything queued
// before the hold and before everything after. The release must run even
// if the flush fails, or the writer waits on the lock forever.
func (c *client) holdWriter() (release func(), err error) {
	m := c.out
	if m == nil {
		return func() {}, nil
	}
	if before := m.holdSeam.Swap(nil); before != nil {
		(*before)()
	}
	m.writing.Lock()
	m.held.Store(true)
	m.mu.Lock()
	before := m.queue
	m.queue, m.queueSlack = nil, 0
	m.queued = 0
	m.mu.Unlock()
	return func() { m.held.Store(false); m.writing.Unlock() }, c.writeAll(before)
}

// switchDimension sets the client's dimension and holds the writer until
// release, so the caller's direct writes (Respawn, arrival) fall between
// what was queued before and after. The store takes mu separately, which
// sendIn's check orders against: otherwise a packet queued during the
// hold's flush could pass the check with the old dimension and reach the
// client in the world it left.
func (c *client) switchDimension(dim *dimension) (release func(), err error) {
	release, err = c.holdWriter()
	if err != nil {
		return release, err
	}
	if m := c.out; m != nil {
		m.mu.Lock()
		before := m.queue
		m.queue, m.queueSlack = nil, 0
		m.queued = 0
		c.dim.Store(dim)
		m.mu.Unlock()
		err = c.writeAll(before)
	} else {
		c.dim.Store(dim)
	}
	return release, err
}

// release starts writing, once the client is in the world.
func (m *mailbox) release() {
	m.readyOnce.Do(func() {
		close(m.ready)
		m.released.Store(true)
		m.signal()
	})
}

// joined reports whether the client is in the world: the mailbox is
// released once the join sent the newcomer its view of others, so the tick
// may pair for it only after, or both would send the same body.
func (c *client) joined() bool {
	return c.out != nil && c.out.released.Load()
}

// disconnectAfterDrain queues a Disconnect with message and closes the
// connection once the mailbox drains, as every server-initiated disconnect
// does, so the reason arrives before the close; queued so it follows
// everything already owed.
func (c *client) disconnectAfterDrain(message protocol.Text) {
	if !c.queuePluginKick(message) {
		c.drainDisconnect(message)
	}
}

func (c *client) drainDisconnect(message protocol.Text) {
	c.disconnectReason.Store(&message)
	if c.out == nil {
		c.conn.Close()
		return
	}
	var b protocol.Buffer
	b.PutText(message)
	c.send(protocol.IDPlayDisconnect, b.Bytes())
	c.out.closeAfterDrain()
}

// closeAfterDrain marks the connection to close once every queued packet is
// written, so a server disconnect's reason arrives first.
func (m *mailbox) closeAfterDrain() {
	m.mu.Lock()
	m.closing = true
	m.mu.Unlock()
	m.signal()
}

// signal wakes the writer unless a wake is already pending.
func (m *mailbox) signal() {
	select {
	case m.wake <- struct{}{}:
	default: // a wake is already pending
	}
}

// stop ends the writer goroutine; queued packets are discarded.
func (m *mailbox) stop() { m.once.Do(func() { close(m.done) }) }

// wakeMailboxes resumes immediate wakes and flushes packets queued during the tick.
func (s *Server) wakeMailboxes() {
	s.deferWakes.Store(false)
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	for _, c := range clients {
		if m := c.out; m != nil {
			m.mu.Lock()
			pending := len(m.queue) != 0
			m.mu.Unlock()
			if pending {
				m.signal()
			}
		}
	}
}
