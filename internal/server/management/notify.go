package management

import (
	"time"

	"github.com/golem-mc/golem/internal/server/player"
)

// activityInterval is the least time between two server/activity
// notifications. vanilla: ServerActivityMonitor
const activityInterval = 30 * time.Second

// notify queues a notification on every connection; param is left out when
// nil. vanilla: JsonRpcNotificationService, JsonRPCUtils.createRequest
func (s *Server) notify(method string, param any) {
	n := map[string]any{"jsonrpc": "2.0", "method": "minecraft:notification/" + method}
	if param != nil {
		n["params"] = []any{param}
	}
	msg := encode(n)
	s.mu.Lock()
	conns := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		c.send(msg)
	}
}

// ServerStarted tells the clients the server is up. Like the other
// notification methods, it does nothing on a nil Server.
func (s *Server) ServerStarted() {
	if s != nil {
		s.notify("server/started", nil)
	}
}

// ServerStopping tells the clients the server is shutting down.
func (s *Server) ServerStopping() {
	if s != nil {
		s.notify("server/stopping", nil)
	}
}

// SaveStarted tells the clients a save began.
func (s *Server) SaveStarted() {
	if s != nil {
		s.notify("server/saving", nil)
	}
}

// SaveCompleted tells the clients a save ended.
func (s *Server) SaveCompleted() {
	if s != nil {
		s.notify("server/saved", nil)
	}
}

// LoginActivity sends server/activity unless one went out in the last
// activityInterval. vanilla: ServerActivityMonitor.reportLoginActivity
func (s *Server) LoginActivity() {
	if s == nil {
		return
	}
	now := s.now()
	s.mu.Lock()
	due := s.lastActivity.IsZero() || now.Sub(s.lastActivity) >= activityInterval
	if due {
		s.lastActivity = now
	}
	s.mu.Unlock()
	if due {
		s.notify("server/activity", nil)
	}
}

// PlayerJoined tells the clients a player joined.
func (s *Server) PlayerJoined(p player.Profile) {
	if s != nil {
		s.notify("players/joined", playerOut{p.UUID, p.Name})
	}
}

// PlayerLeft tells the clients a player left.
func (s *Server) PlayerLeft(p player.Profile) {
	if s != nil {
		s.notify("players/left", playerOut{p.UUID, p.Name})
	}
}

// Heartbeat sends the server state to the clients.
// vanilla: ManagementServer.scheduleHeartbeat
func (s *Server) Heartbeat() {
	if s != nil {
		s.notify("server/status", s.state())
	}
}

// SetHeartbeat sends a heartbeat every seconds, none for 0 or less,
// replacing the schedule in force.
func (s *Server) SetHeartbeat(seconds int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heartbeatLocked(seconds)
}

// heartbeatLocked restarts the heartbeat goroutine; mu is held.
func (s *Server) heartbeatLocked(seconds int) {
	if s.stopBeat != nil {
		close(s.stopBeat)
		s.stopBeat = nil
	}
	if seconds <= 0 || s.closed {
		return
	}
	stop := make(chan struct{})
	s.stopBeat = stop
	go func() {
		t := time.NewTicker(time.Duration(seconds) * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.Heartbeat()
			case <-stop:
				return
			}
		}
	}()
}

// AllowlistAdded tells the clients a player joined the white-list.
func (s *Server) AllowlistAdded(p player.Profile) {
	if s != nil {
		s.notify("allowlist/added", playerOut{p.UUID, p.Name})
	}
}

// AllowlistRemoved tells the clients a player left the white-list.
func (s *Server) AllowlistRemoved(p player.Profile) {
	if s != nil {
		s.notify("allowlist/removed", playerOut{p.UUID, p.Name})
	}
}

// OperatorAdded tells the clients an operator entry was written.
func (s *Server) OperatorAdded(op Operator) {
	if s != nil {
		s.notify("operators/added", operatorOutOf(op))
	}
}

// OperatorRemoved tells the clients an operator entry was removed.
func (s *Server) OperatorRemoved(op Operator) {
	if s != nil {
		s.notify("operators/removed", operatorOutOf(op))
	}
}

// PlayerBanned tells the clients a player ban was written.
func (s *Server) PlayerBanned(b UserBan) {
	if s != nil {
		s.notify("bans/added", userBanOutOf(b))
	}
}

// PlayerUnbanned tells the clients a player's ban was lifted.
func (s *Server) PlayerUnbanned(p player.Profile) {
	if s != nil {
		s.notify("bans/removed", playerOut{p.UUID, p.Name})
	}
}

// IPBanned tells the clients an address ban was written.
func (s *Server) IPBanned(b IPBan) {
	if s != nil {
		s.notify("ip_bans/added", ipBanOutOf(b))
	}
}

// IPUnbanned tells the clients an address's ban was lifted.
func (s *Server) IPUnbanned(ip string) {
	if s != nil {
		s.notify("ip_bans/removed", ip)
	}
}
