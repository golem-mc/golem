package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/management"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

// newManagement builds the management server cfg enables, or nil. The secret
// is checked before TLS, and both before the world loads.
// vanilla: JsonRpc.create
func newManagement(s *Server, cfg *config.Config) (*management.Server, error) {
	if !cfg.ManagementServerEnabled {
		return nil, nil
	}
	m, err := management.New(management.Options{
		Host:             cfg.ManagementServerHost,
		Port:             cfg.ManagementServerPort,
		Secret:           cfg.ManagementServerSecret,
		AllowedOrigins:   cfg.ManagementServerAllowedOrigins,
		HeartbeatSeconds: cfg.StatusHeartbeatInterval,
	}, managementHost{s})
	if err != nil {
		return nil, err
	}
	// Deliberately unlike vanilla, which serves TLS from a keystore.
	if cfg.ManagementServerTLSEnabled {
		return nil, errors.New("management-server-tls-enabled needs a PKCS12 keystore, which this server " +
			"does not read; set management-server-tls-enabled=false")
	}
	return m, nil
}

// listenManagement binds the management port and serves it until a stop
// request, which accepts no new connection; the open ones close at the end
// of shutdown. A failed bind stops startup. vanilla: ManagementServer.start
func (s *Server) listenManagement() error {
	if s.mgmt == nil {
		return nil
	}
	slog.Info("Starting json RPC server on " +
		net.JoinHostPort(s.Config.ManagementServerHost, strconv.Itoa(s.Config.ManagementServerPort)))
	l, err := s.mgmt.Listen()
	if err != nil {
		return fmt.Errorf("management server: %w", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	slog.Info(fmt.Sprintf("Json-RPC Management connection listening on %s:%d", s.Config.ManagementServerHost, port))
	s.mgmtAddr = net.JoinHostPort(s.Config.ManagementServerHost, strconv.Itoa(port))
	close(s.mgmtBound)
	go s.mgmt.Serve(l)
	go func() {
		<-s.stopReq
		l.Close()
	}()
	return nil
}

// managementHost lets the management methods reach the server.
type managementHost struct{ s *Server }

func (h managementHost) OnlinePlayers() []management.OnlinePlayer {
	clients := h.s.playerClients()
	out := make([]management.OnlinePlayer, len(clients))
	for i, c := range clients {
		out[i] = management.OnlinePlayer{Profile: c.player.Profile, IP: c.IP()}
	}
	return out
}

func (h managementHost) SaveAll(flush bool) bool { return h.s.SaveAll(flush) }

func (h managementHost) RequestStop() { h.s.RequestStop() }

func (h managementHost) TellAll(text protocol.Text, overlay bool) {
	h.s.broadcast(protocol.IDPlaySystemChat, systemChatPayload(text, overlay))
}

func (h managementHost) Tell(id uuid.UUID, text protocol.Text, overlay bool) {
	if c := h.s.clientByUUID(id); c != nil {
		c.send(protocol.IDPlaySystemChat, systemChatPayload(text, overlay))
	}
}

// LookupProfile resolves an id through the online players and the user
// cache, then as given when a name comes with it: deliberately unlike
// vanilla's fetchUserById, as this server has no Mojang lookup. A name alone
// goes through the same, then, in offline mode, its offline UUID.
// vanilla: MinecraftPlayerListService.getUser
func (h managementHost) LookupProfile(id *uuid.UUID, name string) (player.Profile, bool) {
	ws := worldScope{srv: h.s}
	if id != nil {
		if p, ok := ws.FetchProfileByID(*id); ok {
			return p, true
		}
		return player.Profile{UUID: *id, Name: name}, name != ""
	}
	if p, ok := ws.FetchProfileByName(name); ok {
		return p, true
	}
	if !h.s.Config.OnlineMode {
		return player.Profile{UUID: protocol.OfflineUUID(name), Name: name}, true
	}
	return player.Profile{}, false
}

func (h managementHost) Allowlist() []player.Profile { return h.s.whitelist.profiles() }

func (h managementHost) AllowlistAdd(p player.Profile) { h.s.allowlistAdd(p) }

func (h managementHost) AllowlistRemove(p player.Profile) { h.s.allowlistRemove(p) }

func (h managementHost) AllowlistClear() { h.s.allowlistClear() }

func (h managementHost) KickUnlisted() { h.s.kickUnlisted() }

func (h managementHost) Operators() []management.Operator {
	entries := h.s.ops.list.snapshot()
	out := make([]management.Operator, len(entries))
	for i, e := range entries {
		out[i] = e.operator()
	}
	return out
}

func (h managementHost) DefaultOpLevel() int { return int(h.s.opLevel.Load()) }

// OpPut, Deop and OpsClear refresh an online player's cached level and
// command tree, as /op does. Deliberately unlike vanilla's clear, which
// leaves the client's tree stale. vanilla: PlayerList.op, deop
func (h managementHost) OpPut(op management.Operator) {
	h.s.opPut(opEntry{UUID: op.Profile.UUID.String(), Name: op.Profile.Name,
		Level: op.Level, BypassesPlayerLimit: op.BypassesPlayerLimit})
	h.refreshLevel(op.Profile.UUID)
}

func (h managementHost) Deop(p player.Profile) {
	if h.s.deop(p.UUID) {
		h.refreshLevel(p.UUID)
	}
}

func (h managementHost) OpsClear() {
	for _, e := range h.s.opsClear() {
		if id, err := uuid.Parse(e.UUID); err == nil {
			h.refreshLevel(id)
		}
	}
}

// refreshLevel tells an online player the level the list now gives them.
func (h managementHost) refreshLevel(id uuid.UUID) {
	if c := h.s.clientByUUID(id); c != nil {
		c.SetPermLevel(h.s.ops.Level(id))
		c.ResendPermissionState()
	}
}

func (h managementHost) Bans() []management.UserBan {
	entries := h.s.bans.playerEntries()
	out := make([]management.UserBan, len(entries))
	for i, e := range entries {
		out[i] = e.userBan()
	}
	return out
}

func (h managementHost) BanPut(b management.UserBan) {
	h.s.banPut(banEntry{UUID: b.Profile.UUID.String(), Name: b.Profile.Name, Created: listTime(),
		Source: b.Source, Expires: banExpiryString(b.Expires), Reason: b.Reason})
}

func (h managementHost) BanRemove(p player.Profile) { h.s.banRemove(p) }

func (h managementHost) BansClear() { h.s.bansClear() }

func (h managementHost) IPBans() []management.IPBan {
	entries := h.s.bans.ipEntries()
	out := make([]management.IPBan, len(entries))
	for i, e := range entries {
		out[i] = e.ipBan()
	}
	return out
}

func (h managementHost) IPBanPut(b management.IPBan) {
	h.s.ipBanPut(ipBanEntry{IP: b.IP, Created: listTime(), Source: b.Source,
		Expires: banExpiryString(b.Expires), Reason: b.Reason})
}

func (h managementHost) IPBanRemove(ip string) { h.s.ipBanRemove(ip) }

func (h managementHost) IPBansClear() { h.s.ipBansClear() }

func (h managementHost) Kick(id uuid.UUID, reason protocol.Text) bool {
	c := h.s.clientByUUID(id)
	if c == nil {
		return false
	}
	c.Disconnect(reason)
	return true
}

// GameRules walks the table, skipping the feature-gated rule as the Edit
// Game Rules screen does.
func (h managementHost) GameRules() []management.GameRule {
	var out []management.GameRule
	for _, r := range world.GameRuleTable {
		if !r.FeatureGated {
			out = append(out, h.s.managementRule(r))
		}
	}
	return out
}

func (h managementHost) SetGameRule(r management.GameRule) {
	if r.Integer {
		h.s.world.SetIntRule(world.RuleIndex(r.ID), r.Int)
	} else {
		h.s.world.SetBoolRule(world.RuleIndex(r.ID), r.Bool)
	}
	worldScope{srv: h.s}.RuleChanged(r.ID)
}

// managementRule is rule r with its current value.
func (s *Server) managementRule(r world.GameRule) management.GameRule {
	if r.Kind == world.RuleInt {
		return management.GameRule{ID: r.ID, Integer: true, Int: s.world.IntRule(world.RuleIndex(r.ID)), Min: r.Min, Max: r.Max}
	}
	return management.GameRule{ID: r.ID, Bool: s.world.BoolRule(world.RuleIndex(r.ID))}
}

// The serversettings methods look the name up in the server's tables.
func (h managementHost) BoolSetting(name string) bool { return boolSettings[name].get(h.s) }

func (h managementHost) SetBoolSetting(name string, v bool) bool {
	e := boolSettings[name]
	e.set(h.s, v)
	return e.get(h.s)
}

func (h managementHost) IntSetting(name string) int { return intSettings[name].get(h.s) }

func (h managementHost) SetIntSetting(name string, v int) int {
	e := intSettings[name]
	e.set(h.s, v)
	return e.get(h.s)
}

func (h managementHost) StringSetting(name string) string { return stringSettings[name].get(h.s) }

func (h managementHost) SetStringSetting(name string, v string) string {
	e := stringSettings[name]
	e.set(h.s, v)
	return e.get(h.s)
}
