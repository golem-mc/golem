package server

import (
	"strings"
	"time"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

func (h *pluginHost) Bans() plugin.BanList        { return pluginBanList{h.srv, false} }
func (h *pluginHost) IPBans() plugin.BanList      { return pluginBanList{h.srv, true} }
func (h *pluginHost) Whitelist() plugin.Whitelist { return pluginWhitelist{h.srv} }
func (h *pluginHost) Ops() plugin.Ops             { return pluginOps{h.srv} }

type pluginBanList struct {
	srv *Server
	ip  bool
}

// Ban only records the ban; the caller kicks with its own message.
func (b pluginBanList) Ban(target, reason string, expires time.Time, source string) {
	if b.ip {
		b.srv.ipBanPut(ipBanEntry{IP: target, Reason: reason, Source: source, Created: listTime(), Expires: banExpiryString(expires)})
		return
	}
	id, err := uuid.Parse(target)
	if err != nil {
		// A name that may never have joined: the entry gets its UUID at login.
		b.srv.banPut(banEntry{Name: target, Reason: reason, Source: source, Created: listTime(), Expires: banExpiryString(expires)})
		return
	}
	name, _ := b.srv.usercache.nameOf(id)
	if c := b.srv.clientByUUID(id); c != nil {
		name = c.player.Name
	}
	b.srv.banPut(banEntry{UUID: id.String(), Name: name, Reason: reason, Source: source, Created: listTime(), Expires: banExpiryString(expires)})
}
func (b pluginBanList) Pardon(target string) {
	if b.ip {
		b.srv.ipBanRemove(target)
		return
	}
	if id, err := uuid.Parse(target); err == nil {
		b.srv.banRemove(player.Profile{UUID: id})
		return
	}
	if b.srv.bans.removeName(target) {
		b.srv.mgmt.PlayerUnbanned(player.Profile{Name: target})
	}
}
func (b pluginBanList) Entry(target string) (plugin.Ban, bool) {
	if !b.ip {
		if id, err := uuid.Parse(target); err == nil {
			target = id.String()
		}
	}
	for _, e := range b.Entries() {
		if e.Target == target || !b.ip && strings.EqualFold(e.Name, target) {
			return e, true
		}
	}
	return plugin.Ban{}, false
}
func (b pluginBanList) Entries() []plugin.Ban {
	out := make([]plugin.Ban, 0)
	add := func(target, name, reason, source, created, expires string) {
		t, _ := time.Parse(listTimeLayout, created)
		out = append(out, plugin.Ban{Target: target, Name: name, Reason: reason, Source: source, Created: t, Expires: banExpiryTime(expires)})
	}
	if b.ip {
		for _, e := range b.srv.bans.ipEntries() {
			add(e.IP, "", e.Reason, e.Source, e.Created, e.Expires)
		}
	} else {
		for _, e := range b.srv.bans.playerEntries() {
			add(e.UUID, e.Name, e.Reason, e.Source, e.Created, e.Expires)
		}
	}
	return out
}

type pluginWhitelist struct{ srv *Server }

func (w pluginWhitelist) Enabled() bool      { return w.srv.whitelist.isEnabled() }
func (w pluginWhitelist) SetEnabled(v bool)  { boolSettings["use_allowlist"].set(w.srv, v) }
func (w pluginWhitelist) Enforced() bool     { return w.srv.enforceWhitelist.Load() }
func (w pluginWhitelist) SetEnforced(v bool) { boolSettings["enforce_allowlist"].set(w.srv, v) }
func (w pluginWhitelist) Add(id plugin.UUID, name string) {
	w.srv.allowlistAdd(player.Profile{UUID: uuid.UUID(id), Name: name})
}
func (w pluginWhitelist) Remove(id plugin.UUID) {
	name := ""
	for _, p := range w.srv.whitelist.profiles() {
		if p.UUID == uuid.UUID(id) {
			name = p.Name
			break
		}
	}
	w.srv.allowlistRemove(player.Profile{UUID: uuid.UUID(id), Name: name})
	w.srv.kickUnlisted()
}
func (w pluginWhitelist) Contains(id plugin.UUID) bool {
	return w.srv.whitelist.contains(uuid.UUID(id))
}

type pluginOps struct{ srv *Server }

func (o pluginOps) Level(id plugin.UUID) int { return o.srv.ops.Level(uuid.UUID(id)) }
func (o pluginOps) Set(id plugin.UUID, name string, level int, bypassLimit bool) {
	if level <= 0 {
		o.srv.deop(uuid.UUID(id))
	} else {
		o.srv.opPut(opEntry{UUID: uuid.UUID(id).String(), Name: name, Level: min(level, 4), BypassesPlayerLimit: bypassLimit})
	}
	managementHost{s: o.srv}.refreshLevel(uuid.UUID(id))
	if level <= 0 {
		o.srv.kickUnlisted()
	}
}
