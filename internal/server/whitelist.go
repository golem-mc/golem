package server

import (
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// whitelistFile is vanilla's white-list in the working directory.
const whitelistFile = "whitelist.json"

// whitelistEntry is one entry of whitelist.json, in vanilla's shape.
type whitelistEntry struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

// whitelistStore holds the white-list and its on flag, which starts at
// server.properties' white-list and is toggled by /whitelist on and off;
// entries load from whitelist.json on first use.
type whitelistStore struct {
	list    *jsonList[whitelistEntry]
	enabled atomic.Bool
}

func newWhitelistStore(path string, enabled bool) *whitelistStore {
	w := &whitelistStore{list: newJSONList[whitelistEntry](path)}
	w.enabled.Store(enabled)
	return w
}

// isEnabled reports whether the white-list is on.
func (w *whitelistStore) isEnabled() bool { return w.enabled.Load() }

// setEnabled turns the white-list on or off.
func (w *whitelistStore) setEnabled(enabled bool) { w.enabled.Store(enabled) }

// contains reports whether the player is listed, whether or not the list
// is on.
func (w *whitelistStore) contains(id uuid.UUID) bool {
	for _, entry := range w.list.snapshot() {
		if entry.UUID == id.String() {
			return true
		}
	}
	return false
}

// add puts a player on the list, reporting false when they are already there.
func (w *whitelistStore) add(profile player.Profile) bool {
	added := false
	w.list.update(func(entries []whitelistEntry) ([]whitelistEntry, bool) {
		for _, entry := range entries {
			if entry.UUID == profile.UUID.String() {
				return entries, false
			}
		}
		added = true
		return append(entries, whitelistEntry{UUID: profile.UUID.String(), Name: profile.Name}), true
	})
	return added
}

// remove takes a player off the list, reporting whether they were on it.
func (w *whitelistStore) remove(id uuid.UUID) bool {
	removed := false
	w.list.update(func(entries []whitelistEntry) ([]whitelistEntry, bool) {
		for i, entry := range entries {
			if entry.UUID == id.String() {
				removed = true
				return append(entries[:i], entries[i+1:]...), true
			}
		}
		return entries, false
	})
	return removed
}

// clear empties the list and returns the players it held.
func (w *whitelistStore) clear() []player.Profile {
	removed := w.profiles()
	w.list.update(func(entries []whitelistEntry) ([]whitelistEntry, bool) {
		return nil, len(entries) > 0
	})
	return removed
}

// profiles is the whitelisted players in file order.
func (w *whitelistStore) profiles() []player.Profile {
	entries := w.list.snapshot()
	profiles := make([]player.Profile, 0, len(entries))
	for _, entry := range entries {
		id, err := uuid.Parse(entry.UUID)
		if err != nil {
			continue
		}
		profiles = append(profiles, player.Profile{UUID: id, Name: entry.Name})
	}
	return profiles
}

// reload drops the reading so that the next read comes from disk again.
func (w *whitelistStore) reload() { w.list.reload() }

// allowlistAdd lists the player and, if that changed the list, notifies the
// management clients. vanilla: UserWhiteList.add
func (s *Server) allowlistAdd(profile player.Profile) {
	if s.whitelist.add(profile) {
		s.mgmt.AllowlistAdded(profile)
	}
}

// allowlistRemove is allowlistAdd's opposite. vanilla: UserWhiteList.remove
func (s *Server) allowlistRemove(profile player.Profile) {
	if s.whitelist.remove(profile.UUID) {
		s.mgmt.AllowlistRemoved(profile)
	}
}

// allowlistClear empties the list, notifying each entry first.
// vanilla: UserWhiteList.clear
func (s *Server) allowlistClear() {
	for _, profile := range s.whitelist.clear() {
		s.mgmt.AllowlistRemoved(profile)
	}
}

// kickUnlisted disconnects, with enforce-whitelist and the white-list on,
// every online player not on the raw list. Operators are exempt only from
// the login check. vanilla: MinecraftServer.kickUnlistedPlayers
func (s *Server) kickUnlisted() {
	if !s.enforceWhitelist.Load() || !s.whitelist.isEnabled() {
		return
	}
	kick := protocol.Text{Translate: "multiplayer.disconnect.not_whitelisted"}
	for _, c := range s.playerClients() {
		if !s.whitelist.contains(c.player.UUID) {
			c.Disconnect(kick)
		}
	}
}
