// Package management is the Minecraft Server Management Protocol: JSON-RPC
// 2.0 over a WebSocket, authenticated by a shared secret.
package management

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golem-mc/golem/internal/config"
)

// Options are the management server's server.properties settings.
type Options struct {
	Host string
	// Port 0 picks a free port.
	Port   int
	Secret string
	// AllowedOrigins is the comma-separated Origin values a client sending
	// its key in Sec-WebSocket-Protocol may come from, matched exactly.
	AllowedOrigins string
	// HeartbeatSeconds is the interval between server/status notifications,
	// 0 for none.
	HeartbeatSeconds int
}

// queueSize is how many encoded messages may wait for a connection's
// writer; a client that falls further behind is dropped.
const queueSize = 256

// closeWait bounds how long Close waits for the connections' queued
// messages to go out.
const closeWait = 5 * time.Second

// Server serves management connections.
type Server struct {
	opts    Options
	origins map[string]bool
	methods map[string]method
	// lastID numbers connections from 1 for the log.
	lastID atomic.Int64

	mu     sync.Mutex
	http   *http.Server
	conns  map[*conn]struct{}
	closed bool

	host Host
	// now is the clock LoginActivity reads.
	now          func() time.Time
	lastActivity time.Time
	// stopBeat ends the heartbeat goroutine.
	stopBeat chan struct{}
}

// New builds a management server; it refuses an invalid secret.
// vanilla: JsonRpc.create
func New(opts Options, host Host) (*Server, error) {
	if !config.ValidManagementSecret(opts.Secret) {
		//nolint:staticcheck // ST1005: vanilla's message, capital included
		return nil, errors.New("Invalid management server secret, must be 40 alphanumeric characters")
	}
	origins := make(map[string]bool)
	for o := range strings.SplitSeq(opts.AllowedOrigins, ",") {
		origins[o] = true
	}
	s := &Server{opts: opts, origins: origins, host: host, now: time.Now, conns: make(map[*conn]struct{})}
	s.methods = s.served()
	s.SetHeartbeat(opts.HeartbeatSeconds)
	return s, nil
}

// Handler authenticates a handshake, then upgrades it to a connection served
// until it closes. vanilla: ManagementServer.start
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sub, reason := s.authenticate(r)
		if reason != "" {
			slog.Debug("Authentication rejected for management connection", "addr", r.RemoteAddr, "reason", reason)
			unauthorized(w, reason)
			return
		}
		if ws := upgrade(w, r, sub); ws != nil {
			s.serve(ws, r.RemoteAddr)
		}
	})
}

// Listen binds the configured host and port.
func (s *Server) Listen() (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort(s.opts.Host, strconv.Itoa(s.opts.Port)))
}

// Serve accepts connections on l until l or the server closes.
func (s *Server) Serve(l net.Listener) {
	hs := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, MaxHeaderBytes: maxMessage}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		l.Close()
		return
	}
	s.http = hs
	s.mu.Unlock()
	if err := hs.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		slog.Warn("management server stopped", "error", err)
	}
}

// Close stops the listener and closes every connection once its queued
// messages are sent, waiting a bounded time for them. A nil Server is a no-op.
func (s *Server) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	s.heartbeatLocked(0)
	if s.http != nil {
		s.http.Close()
	}
	conns := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
		c.close()
	}
	s.mu.Unlock()
	deadline := time.After(closeWait)
	for _, c := range conns {
		select {
		case <-c.written:
		case <-deadline:
			return
		}
	}
}

// conn is one management connection.
type conn struct {
	id   int64
	addr string
	ws   *WSConn

	// mu guards out's closing against sends.
	mu     sync.Mutex
	out    chan json.RawMessage
	closed bool
	// written is closed when the writer has sent the queue and closed ws.
	written chan struct{}
}

// logf logs at INFO with the connection's prefix. vanilla: JsonRpcLogger
func (c *conn) logf(format string, args ...any) {
	slog.Info(fmt.Sprintf("RPC Connection #%d: ", c.id) + fmt.Sprintf(format, args...))
}

// serve reads and answers ws's messages, one at a time, until it closes.
func (s *Server) serve(ws *WSConn, addr string) {
	c := &conn{id: s.lastID.Add(1), addr: addr, ws: ws,
		out: make(chan json.RawMessage, queueSize), written: make(chan struct{})}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = ws.Close(CloseNormal)
		return
	}
	s.conns[c] = struct{}{}
	s.mu.Unlock()
	c.logf("Management connection opened for %s", addr)
	go c.write()
	defer func() {
		c.close()
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
		c.logf("Management connection closed for %s", addr)
	}()
	for {
		msg, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if answer := s.handleMessage(c, msg); answer != nil {
			c.send(answer)
		}
	}
}

// send queues msg for the writer without blocking: a full queue drops the
// connection instead.
func (c *conn) send(msg json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	select {
	case c.out <- msg:
	default:
		slog.Warn(fmt.Sprintf("RPC Connection #%d: dropping a client that does not read its messages", c.id))
		c.closed = true
		close(c.out)
		// The writer may be stuck on the client, holding the frame lock;
		// closing the socket under it frees it.
		c.ws.abort()
	}
}

// close ends the queue; the writer sends what it holds, then closes ws.
func (c *conn) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.out)
	}
}

// write sends the queued messages in order until the queue closes.
func (c *conn) write() {
	defer close(c.written)
	for msg := range c.out {
		if err := c.ws.WriteText(msg); err != nil {
			break
		}
	}
	_ = c.ws.Close(CloseNormal)
}
