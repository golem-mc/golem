package server

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

const (
	// defaultKeepAliveInterval is how often the server pings; vanilla uses 15
	// seconds.
	defaultKeepAliveInterval = 15 * time.Second
	// defaultKeepAliveTimeout is how long a ping may go unanswered before the
	// client is dropped; vanilla drops 15 seconds after sending.
	defaultKeepAliveTimeout = 15 * time.Second
)

// keepAliveTicker is a client's keep-alive state: the unanswered ping, when
// it was sent, and any drop reason. The main loop ticks players;
// configuration runs its own ticker.
type keepAliveTicker struct {
	mu      sync.Mutex
	pending int64 // ID of the unanswered keep-alive, or 0 if there is none
	sentAt  time.Time
	// started is set when the exchange begins; no ping is sent before, so none
	// waits behind the join while its timeout runs.
	started bool
	// latency is the smoothed round trip in milliseconds.
	latency int32
	// lastPing is when the last ping went out, from which the interval counts.
	lastPing time.Time
	err      error
}

// start begins the exchange, so no ping waits behind the join while its
// timeout runs.
func (k *keepAliveTicker) start(now time.Time) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.started = true
	k.lastPing = now
}

// tick runs a turn of the exchange: it sends a ping via send once the
// interval passes, and errors once an unanswered one is overdue, which the
// caller answers with the timeout Disconnect and close. The ping counts as
// sent when handed off, so a client too slow to read it is dropped like one
// not answering. vanilla: ServerCommonPacketListenerImpl.keepConnectionAlive
func (k *keepAliveTicker) tick(now time.Time, interval, timeout time.Duration, send func(id int64)) error {
	k.mu.Lock()
	if !k.started {
		k.mu.Unlock()
		return nil
	}
	if k.pending != 0 {
		if now.Sub(k.sentAt) >= timeout {
			k.mu.Unlock()
			return fmt.Errorf("client did not answer keep-alive within %s", timeout)
		}
		// A ping is still out; the next one waits until it is answered.
		k.mu.Unlock()
		return nil
	}
	if now.Sub(k.lastPing) < interval {
		k.mu.Unlock()
		return nil
	}
	k.pending = rand.Int64() | 1 // never 0, which marks "nothing pending"
	k.sentAt = now
	k.lastPing = now
	id := k.pending
	k.mu.Unlock()

	send(id)
	return nil
}

// sendPlayKeepAlive queues a ping via the mailbox, so a client that stopped
// reading cannot stall the main loop.
func (c *client) sendPlayKeepAlive(id int64) {
	var b protocol.Buffer
	b.PutI64(id)
	c.send(protocol.IDPlayKeepAlive, b.Bytes())
}

// sendConfigKeepAlive writes a configuration ping unless the phase ended.
// The write takes the connection's write lock, held while registries
// stream and by a stalled client up to the write timeout, so the main loop
// calls this on its own goroutine.
func (c *client) sendConfigKeepAlive(id int64) {
	c.configPingMu.Lock()
	defer c.configPingMu.Unlock()
	if !c.configPingOpen {
		return
	}
	var b protocol.Buffer
	b.PutI64(id)
	_ = c.conn.WritePacket(protocol.IDConfigKeepAlive, b.Bytes())
}

// armReadDeadline sets the read deadline to the outstanding ping's timeout,
// or clears it with none out. It is wall-clock, so it is placed by the time
// left on the keep-alive clock (now), not the ping's timestamp. The setter
// runs under the lock so a concurrent ping cannot leave it stale. Only the
// drop is a deadline: one for a ping to send would cut an arriving packet
// in half.
func (k *keepAliveTicker) armReadDeadline(set func(time.Time) error, now time.Time, timeout time.Duration) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.pending != 0 {
		return set(time.Now().Add(timeout - now.Sub(k.sentAt)))
	}
	return set(time.Time{})
}

// startConfigKeepAlives registers the client for configuration's
// keep-alive, run each tick by tickConfigKeepAlives. stop unregisters it,
// waits out a tick using it (so its deadline never reaches play) and any
// in-flight ping write (so no configuration packet follows).
// vanilla: ServerConfigurationPacketListenerImpl.tick
func (c *client) startConfigKeepAlives() (stop func()) {
	c.configPingMu.Lock()
	c.configPingOpen = true
	c.configPingMu.Unlock()
	c.configTickMu.Lock()
	c.configTicking = true
	c.configTickMu.Unlock()
	c.srv.configMu.Lock()
	if c.srv.configClients == nil {
		c.srv.configClients = map[*client]struct{}{}
	}
	c.srv.configClients[c] = struct{}{}
	c.srv.configMu.Unlock()
	return func() {
		c.srv.configMu.Lock()
		delete(c.srv.configClients, c)
		c.srv.configMu.Unlock()
		c.configTickMu.Lock()
		c.configTicking = false
		c.configTickMu.Unlock()
		c.configPingMu.Lock()
		c.configPingOpen = false
		c.configPingMu.Unlock()
	}
}

// tickConfigKeepAlives runs the keep-alive of every client in
// configuration. A ping due mid-read does not cut the read, since only the
// drop is a deadline. Writes go to a goroutine so a non-reading client
// cannot stall the tick; with at most one ping outstanding per client, at
// most one such goroutine exists per client.
func (s *Server) tickConfigKeepAlives(now time.Time) {
	s.configMu.Lock()
	clients := slices.Collect(maps.Keys(s.configClients))
	s.configMu.Unlock()
	timeout := time.Duration(s.keepAliveTimeout.Load())
	interval := time.Duration(s.keepAliveInterval.Load())
	for _, c := range clients {
		c.configTickMu.Lock()
		if c.configTicking {
			// The error means an outstanding ping expired; the deadline armed below
			// ends the connection.
			_ = c.keepAlive.tick(now, interval, timeout, func(id int64) {
				go c.sendConfigKeepAlive(id)
			})
			_ = c.keepAlive.armReadDeadline(c.conn.SetReadDeadline, now, timeout)
		}
		c.configTickMu.Unlock()
	}
}

// keepAliveClock is the exchange's clock: the main loop's, the wall clock
// on a real server unless a test replaced it.
func (s *Server) keepAliveClock() time.Time {
	if s.keepAliveNow != nil {
		return s.keepAliveNow()
	}
	return s.loopClock().Now()
}

// answer records a keep-alive response, reporting whether it was the
// pending one (vanilla: ServerCommonPacketListenerImpl.handleKeepAlive
// disconnects on anything else, even with none outstanding), and returns the
// new smoothed latency in milliseconds.
// vanilla: ServerCommonPacketListenerImpl.handleKeepAlive
func (k *keepAliveTicker) answer(id int64, now time.Time) (latency int32, ok bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.pending != 0 && id == k.pending {
		k.pending = 0
		k.latency = (k.latency*3 + int32(now.Sub(k.sentAt).Milliseconds())) / 4
		return k.latency, true
	}
	return 0, false
}

// handleKeepAliveAnswer records the pending ping's response, or disconnects
// when none was pending or the id differs.
// vanilla: ServerCommonPacketListenerImpl.handleKeepAlive
func (c *client) handleKeepAliveAnswer(k *keepAliveTicker, r *protocol.Reader) error {
	id := r.ReadI64()
	if err := r.Err(); err != nil {
		return c.decodeFailure("keep_alive", err)
	}
	if latency, ok := k.answer(id, c.srv.keepAliveClock()); ok {
		if c.player != nil {
			c.player.SetLatency(latency)
		}
		return nil
	}
	return c.disconnect(
		fmt.Errorf("client answered a keep-alive it was not sent"),
		protocol.Text{Translate: "disconnect.timeout"})
}

// retryTimeout keeps the outstanding ping answerable while a kick is decided.
func (k *keepAliveTicker) retryTimeout(now time.Time) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.pending != 0 {
		k.sentAt = now
	}
}

// setFailure records why the connection was dropped.
func (k *keepAliveTicker) setFailure(err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.err = err
}

// failure returns why the keep-alive dropped the connection, if it did.
func (k *keepAliveTicker) failure() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.err
}
