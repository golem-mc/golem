package server

import (
	"slices"
	"strings"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/management"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/google/uuid"
)

// bannedPlayersFile and bannedIPsFile are vanilla's ban lists in the
// working directory.
const (
	bannedPlayersFile = "banned-players.json"
	bannedIPsFile     = "banned-ips.json"
)

// banEntry is one entry of banned-players.json, in vanilla's shape.
type banEntry struct {
	UUID    string `json:"uuid"`
	Name    string `json:"name"`
	Created string `json:"created"`
	Source  string `json:"source"`
	Expires string `json:"expires"`
	Reason  string `json:"reason,omitempty"`
}

// ipBanEntry is a banned-ips.json entry: the same fields, with the address
// instead of identity.
type ipBanEntry struct {
	IP      string `json:"ip"`
	Created string `json:"created"`
	Source  string `json:"source"`
	Expires string `json:"expires"`
	Reason  string `json:"reason,omitempty"`
}

// banStore holds the player and IP ban lists, read on first use and written
// by /ban, /ban-ip, /pardon and /pardon-ip. A missing file means no bans.
type banStore struct {
	players *jsonList[banEntry]
	ips     *jsonList[ipBanEntry]
}

func newBanStore(playersPath, ipsPath string) *banStore {
	return &banStore{
		players: newJSONList[banEntry](playersPath),
		ips:     newJSONList[ipBanEntry](ipsPath),
	}
}

// banReasonMessage is a ban's shown reason: its text, or vanilla's "Banned
// by an operator." when empty.
func banReasonMessage(reason string) protocol.Text {
	if reason == "" {
		return protocol.Text{Translate: "multiplayer.disconnect.banned.reason.default"}
	}
	return protocol.Text{Text: reason}
}

// banExpired reports whether a ban's date passed; "forever", empty and
// unreadable never expire. vanilla: BanListEntry.hasExpired
func banExpired(expires string) bool {
	if expires == "" || expires == listExpiresNever {
		return false
	}
	t, err := time.Parse(listTimeLayout, expires)
	return err == nil && t.Before(time.Now())
}

// banDateLayout is how a refused client sees a ban's expiry, in the
// server's zone; the stored stamp uses listTimeLayout.
// vanilla: PlayerList.BAN_DATE_FORMAT
const banDateLayout = "2006-01-02 at 15:04:05 MST"

// banExpiry is a dated ban's lapse date; false for permanent or unreadable.
func banExpiry(expires string) (string, bool) {
	if expires == "" || expires == listExpiresNever {
		return "", false
	}
	t, err := time.Parse(listTimeLayout, expires)
	if err != nil {
		return "", false
	}
	return t.In(time.Local).Format(banDateLayout), true
}

// banEntryLike are the two ban entries the expiry prune reads.
type banEntryLike interface {
	banEntry | ipBanEntry
	expiresAt() string
}

func (e banEntry) expiresAt() string   { return e.Expires }
func (e ipBanEntry) expiresAt() string { return e.Expires }

// liveBans drops expired bans, reporting whether any were dropped so a
// write can persist it. vanilla: StoredUserList.removeExpired
func liveBans[T banEntryLike](entries []T) ([]T, bool) {
	kept := make([]T, 0, len(entries))
	for _, entry := range entries {
		if !banExpired(entry.expiresAt()) {
			kept = append(kept, entry)
		}
	}
	return kept, len(kept) != len(entries)
}

// bannedPlayer finds a ban by UUID, ignoring expired ones.
func (b *banStore) bannedPlayer(id uuid.UUID) (banEntry, bool) {
	entries, _ := liveBans(b.players.snapshot())
	for _, entry := range entries {
		if entry.UUID == id.String() {
			return entry, true
		}
	}
	return banEntry{}, false
}

// bannedProfile finds a ban of the joining profile: by UUID, or by name for
// an entry made before the player's UUID was known (empty UUID), which it
// then binds to the profile's UUID.
func (b *banStore) bannedProfile(profile player.Profile) (banEntry, bool) {
	if entry, ok := b.bannedPlayer(profile.UUID); ok {
		return entry, true
	}
	var found banEntry
	ok := false
	b.players.update(func(entries []banEntry) ([]banEntry, bool) {
		entries, pruned := liveBans(entries)
		for i, entry := range entries {
			if entry.UUID == "" && strings.EqualFold(entry.Name, profile.Name) {
				entries[i].UUID = profile.UUID.String()
				entries[i].Name = profile.Name
				found, ok = entries[i], true
				return entries, true
			}
		}
		return entries, pruned
	})
	return found, ok
}

// sameBanned reports whether two entries ban the same player: the UUID, or
// the name (ignoring case) while neither has a UUID.
func sameBanned(a, b banEntry) bool {
	if a.UUID != "" || b.UUID != "" {
		return a.UUID == b.UUID
	}
	return strings.EqualFold(a.Name, b.Name)
}

// removeName lifts the ban of an entry still without UUID, by name.
func (b *banStore) removeName(name string) bool {
	removed := false
	b.players.update(func(entries []banEntry) ([]banEntry, bool) {
		entries, pruned := liveBans(entries)
		for i, entry := range entries {
			if entry.UUID == "" && strings.EqualFold(entry.Name, name) {
				removed = true
				return append(entries[:i], entries[i+1:]...), true
			}
		}
		return entries, pruned
	})
	return removed
}

// putPlayer writes the entry, replacing the player's own, and reports false
// when that one was the same. vanilla: StoredUserList.add
func (b *banStore) putPlayer(e banEntry) bool {
	return putBan(b.players, e, func(o banEntry) bool { return sameBanned(o, e) },
		func(o banEntry) bool { o.Created = e.Created; return o == e })
}

// clearPlayers empties the list, returning the live entries it removed.
func (b *banStore) clearPlayers() []banEntry { return clearBans(b.players) }

// putBan writes e over the live entry same picks, or appends it; false, and
// the old entry kept, when equal reports it differs from e only in created.
// vanilla: StoredUserList.add, BanListEntry.equals
func putBan[T banEntryLike](l *jsonList[T], e T, same, equal func(T) bool) bool {
	changed := false
	l.update(func(entries []T) ([]T, bool) {
		entries, pruned := liveBans(entries)
		if i := slices.IndexFunc(entries, same); i >= 0 {
			if equal(entries[i]) {
				return entries, pruned
			}
			changed = true
			entries[i] = e
		} else {
			changed = true
			entries = append(entries, e)
		}
		return entries, changed || pruned
	})
	return changed
}

// clearBans empties l, returning the live entries it held.
func clearBans[T banEntryLike](l *jsonList[T]) []T {
	var removed []T
	l.update(func(entries []T) ([]T, bool) {
		removed, _ = liveBans(entries)
		return nil, len(entries) > 0
	})
	return removed
}

// removePlayer lifts a ban, reporting whether the player was banned.
func (b *banStore) removePlayer(id uuid.UUID) bool {
	removed := false
	b.players.update(func(entries []banEntry) ([]banEntry, bool) {
		entries, pruned := liveBans(entries)
		for i, entry := range entries {
			if entry.UUID == id.String() {
				removed = true
				return append(entries[:i], entries[i+1:]...), true
			}
		}
		return entries, pruned
	})
	return removed
}

// playerProfiles is the banned identities in file order, for /pardon's
// suggestions and names.
func (b *banStore) playerProfiles() []player.Profile {
	entries, _ := liveBans(b.players.snapshot())
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

// playerEntries is the banned players in file order, without the expired.
func (b *banStore) playerEntries() []banEntry {
	entries, _ := liveBans(b.players.snapshot())
	return entries
}

// bannedIP finds an IP ban by address, missing an entry whose date has passed.
func (b *banStore) bannedIP(ip string) (ipBanEntry, bool) {
	entries, _ := liveBans(b.ips.snapshot())
	for _, entry := range entries {
		if entry.IP == ip {
			return entry, true
		}
	}
	return ipBanEntry{}, false
}

// putIP is putPlayer for an address.
func (b *banStore) putIP(e ipBanEntry) bool {
	return putBan(b.ips, e, func(o ipBanEntry) bool { return o.IP == e.IP },
		func(o ipBanEntry) bool { o.Created = e.Created; return o == e })
}

// clearIPs is clearPlayers for addresses.
func (b *banStore) clearIPs() []ipBanEntry { return clearBans(b.ips) }

// removeIP lifts an IP ban, reporting whether the address was banned.
func (b *banStore) removeIP(ip string) bool {
	removed := false
	b.ips.update(func(entries []ipBanEntry) ([]ipBanEntry, bool) {
		entries, pruned := liveBans(entries)
		for i, entry := range entries {
			if entry.IP == ip {
				removed = true
				return append(entries[:i], entries[i+1:]...), true
			}
		}
		return entries, pruned
	})
	return removed
}

// ipAddresses is the banned addresses in file order, for /pardon-ip.
func (b *banStore) ipAddresses() []string {
	entries, _ := liveBans(b.ips.snapshot())
	addresses := make([]string, 0, len(entries))
	for _, entry := range entries {
		addresses = append(addresses, entry.IP)
	}
	return addresses
}

// ipEntries is the banned addresses in file order, without the expired.
func (b *banStore) ipEntries() []ipBanEntry {
	entries, _ := liveBans(b.ips.snapshot())
	return entries
}

// banExpiryTime is a stored expiry as a time; zero for "forever" and for an
// unreadable date, which vanilla's BanListEntry also reads as permanent.
func banExpiryTime(expires string) time.Time {
	t, err := time.Parse(listTimeLayout, expires)
	if err != nil {
		return time.Time{}
	}
	return t
}

// banExpiryString is banExpiryTime's inverse, in the server's zone as
// vanilla's SimpleDateFormat writes it.
func banExpiryString(t time.Time) string {
	if t.IsZero() {
		return listExpiresNever
	}
	return t.In(time.Local).Format(listTimeLayout)
}

// userBan is the entry as the management server sees it.
func (e banEntry) userBan() management.UserBan {
	id, _ := uuid.Parse(e.UUID)
	return management.UserBan{Profile: player.Profile{UUID: id, Name: e.Name},
		Reason: e.Reason, Source: e.Source, Expires: banExpiryTime(e.Expires)}
}

// ipBan is the entry as the management server sees it.
func (e ipBanEntry) ipBan() management.IPBan {
	return management.IPBan{IP: e.IP, Reason: e.Reason, Source: e.Source, Expires: banExpiryTime(e.Expires)}
}

// newBanEntry is a permanent ban of profile made now.
func newBanEntry(profile player.Profile, source, reason string) banEntry {
	return banEntry{UUID: profile.UUID.String(), Name: profile.Name, Created: listTime(),
		Source: source, Expires: listExpiresNever, Reason: reason}
}

// banPut writes a player ban, notifying when the list changed.
// vanilla: UserBanList.add
func (s *Server) banPut(e banEntry) {
	if s.bans.putPlayer(e) {
		s.mgmt.PlayerBanned(e.userBan())
	}
}

// banRemove lifts a player's ban, notifying when there was one; false when
// there was none. vanilla: UserBanList.remove
func (s *Server) banRemove(profile player.Profile) bool {
	ok := s.bans.removePlayer(profile.UUID)
	if ok {
		s.mgmt.PlayerUnbanned(profile)
	}
	return ok
}

// bansClear empties the player ban list, notifying each entry first.
// vanilla: UserBanList.clear
func (s *Server) bansClear() {
	for _, e := range s.bans.clearPlayers() {
		s.mgmt.PlayerUnbanned(e.userBan().Profile)
	}
}

// ipBanPut is banPut for an address. vanilla: IpBanList.add
func (s *Server) ipBanPut(e ipBanEntry) {
	if s.bans.putIP(e) {
		s.mgmt.IPBanned(e.ipBan())
	}
}

// ipBanRemove is banRemove for an address. vanilla: IpBanList.remove
func (s *Server) ipBanRemove(ip string) bool {
	ok := s.bans.removeIP(ip)
	if ok {
		s.mgmt.IPUnbanned(ip)
	}
	return ok
}

// ipBansClear is bansClear for addresses. vanilla: IpBanList.clear
func (s *Server) ipBansClear() {
	for _, e := range s.bans.clearIPs() {
		s.mgmt.IPUnbanned(e.IP)
	}
}
