package management

import (
	"encoding/json"
	"slices"

	"github.com/golem-mc/golem/internal/server/player"
)

// allowlistMethods are the allowlist service's methods.
// vanilla: IncomingRpcMethods.registerAllowListService, AllowlistService
func (s *Server) allowlistMethods(m map[string]method) {
	m["minecraft:allowlist"] = method{run: func(*conn, json.RawMessage) (any, error) {
		return s.allowlist(), nil
	}}
	m["minecraft:allowlist/add"] = method{run: s.allowlistAdd}
	m["minecraft:allowlist/remove"] = method{run: s.allowlistRemove}
	m["minecraft:allowlist/set"] = method{run: s.allowlistSet}
	m["minecraft:allowlist/clear"] = method{run: func(c *conn, _ json.RawMessage) (any, error) {
		c.logf("Clear allowlist")
		s.host.AllowlistClear()
		return s.allowlist(), nil
	}}
}

// allowlist is the white-list as players, in file order.
func (s *Server) allowlist() []playerOut {
	list := s.host.Allowlist()
	out := make([]playerOut, len(list))
	for i, p := range list {
		out[i] = playerOut{p.UUID, p.Name}
	}
	return out
}

func (s *Server) allowlistAdd(c *conn, p json.RawMessage) (any, error) {
	players, err := s.resolvePlayers(p)
	if err != nil {
		return nil, err
	}
	for _, pl := range players {
		c.logf("Add player '%s' to allowlist", pl.Name)
		s.host.AllowlistAdd(pl)
	}
	return s.allowlist(), nil
}

func (s *Server) allowlistRemove(c *conn, p json.RawMessage) (any, error) {
	players, err := s.resolvePlayers(p)
	if err != nil {
		return nil, err
	}
	for _, pl := range players {
		c.logf("Remove player '%s' from allowlist", pl.Name)
		s.host.AllowlistRemove(pl)
	}
	s.kickUnlisted(c)
	return s.allowlist(), nil
}

// allowlistSet removes the entries the final list lacks, then adds the new
// ones; entries compare by id and name, as vanilla's NameAndId does.
func (s *Server) allowlistSet(c *conn, p json.RawMessage) (any, error) {
	final, err := s.resolvePlayers(p)
	if err != nil {
		return nil, err
	}
	current := s.host.Allowlist()
	for _, pl := range current {
		if !containsUser(final, pl) {
			c.logf("Remove player '%s' from allowlist", pl.Name)
			s.host.AllowlistRemove(pl)
		}
	}
	for i, pl := range final {
		if !containsUser(current, pl) && !containsUser(final[:i], pl) {
			c.logf("Add player '%s' to allowlist", pl.Name)
			s.host.AllowlistAdd(pl)
		}
	}
	s.kickUnlisted(c)
	return s.allowlist(), nil
}

// containsUser reports whether list holds p's id and name.
func containsUser(list []player.Profile, p player.Profile) bool {
	return slices.ContainsFunc(list, func(q player.Profile) bool { return q.UUID == p.UUID && q.Name == p.Name })
}

func (s *Server) kickUnlisted(c *conn) {
	c.logf("Kick unlisted players")
	s.host.KickUnlisted()
}
