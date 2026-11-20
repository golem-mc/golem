package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/google/uuid"
)

// usercacheFile is vanilla's user cache in the working directory: name to
// UUID for every player who joined.
const usercacheFile = "usercache.json"

// userCacheMRULimit is how many most recent names vanilla writes.
const userCacheMRULimit = 1000

// userCacheEntry is one entry of usercache.json, in vanilla's shape.
type userCacheEntry struct {
	UUID      string `json:"uuid"`
	Name      string `json:"name"`
	ExpiresOn string `json:"expiresOn"`
}

// userCacheStore holds the name-to-UUID cache, read lazily and rewritten
// whole on every join; a missing file means none.
type userCacheStore struct {
	list *jsonList[userCacheEntry]
}

func newUserCacheStore(path string) *userCacheStore {
	return &userCacheStore{list: newJSONList[userCacheEntry](path)}
}

// add records a joining player with a fresh expiry, replacing their UUID's
// entry and putting it first (most recent first). Every join rewrites the
// file. vanilla: CachedUserNameToIdResolver.add
func (s *userCacheStore) add(profile player.Profile) {
	s.list.update(func(entries []userCacheEntry) ([]userCacheEntry, bool) {
		kept := make([]userCacheEntry, 0, len(entries))
		for _, entry := range entries {
			// The UUID's entry is replaced; an entry with an unreadable UUID or stamp is
			// dropped too, as vanilla's load does, so neither is written back.
			if entry.UUID != profile.UUID.String() && userCacheReadable(entry) {
				kept = append(kept, entry)
			}
		}
		entries = append([]userCacheEntry{{
			UUID:      profile.UUID.String(),
			Name:      profile.Name,
			ExpiresOn: userCacheExpiry(time.Now()).Format(listTimeLayout),
		}}, kept...)
		if len(entries) > userCacheMRULimit {
			entries = entries[:userCacheMRULimit]
		}
		return entries, true
	})
}

// nameOf is the name cached for a UUID, expired or not.
// vanilla: CachedUserNameToIdResolver.get(UUID)
func (s *userCacheStore) nameOf(id uuid.UUID) (string, bool) {
	for _, entry := range s.list.snapshot() {
		if entry.UUID == id.String() && userCacheReadable(entry) {
			return entry.Name, true
		}
	}
	return "", false
}

// names answers the cached name for every readable UUID, expired or not.
func (s *userCacheStore) names() map[uuid.UUID]string {
	entries := s.list.snapshot()
	out := make(map[uuid.UUID]string, len(entries))
	for _, entry := range entries {
		id, err := uuid.Parse(entry.UUID)
		if err != nil {
			continue
		}
		if _, err := time.Parse(listTimeLayout, entry.ExpiresOn); err != nil {
			continue
		}
		if _, exists := out[id]; !exists {
			out[id] = entry.Name
		}
	}
	return out
}

// profiles is the cached identities in file order, most recent first,
// skipping expired or unreadable entries.
// vanilla: CachedUserNameToIdResolver.readGameProfile, get
func (s *userCacheStore) profiles() []player.Profile {
	entries := s.list.snapshot()
	profiles := make([]player.Profile, 0, len(entries))
	for _, entry := range entries {
		id, err := uuid.Parse(entry.UUID)
		if err != nil || !userCacheLive(entry.ExpiresOn) {
			continue
		}
		profiles = append(profiles, player.Profile{UUID: id, Name: entry.Name})
	}
	return profiles
}

// userCacheLive reports whether an entry has not expired; an unreadable
// stamp is not live.
func userCacheLive(expiresOn string) bool {
	t, err := time.Parse(listTimeLayout, expiresOn)
	return err == nil && time.Now().Before(t)
}

// userCacheReadable reports whether an entry's UUID and stamp parse;
// vanilla drops one failing either at load.
// vanilla: CachedUserNameToIdResolver.readGameProfile
func userCacheReadable(entry userCacheEntry) bool {
	if _, err := uuid.Parse(entry.UUID); err != nil {
		return false
	}
	_, err := time.Parse(listTimeLayout, entry.ExpiresOn)
	return err == nil
}

// userCacheExpiry is a fresh entry's stamp: vanilla's Calendar.add(MONTH, 1),
// which clamps to the target month's last day, so a January 31 join lapses
// at February's end, not in March.
// vanilla: CachedUserNameToIdResolver.add
func userCacheExpiry(now time.Time) time.Time {
	first := time.Date(now.Year(), now.Month()+1, 1, now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), now.Location())
	day := min(now.Day(), first.AddDate(0, 1, -1).Day())
	return time.Date(now.Year(), now.Month()+1, day, now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), now.Location())
}

// defaultProfileLookupServer is Mojang's name-to-profile endpoint.
const defaultProfileLookupServer = "https://api.minecraftservices.com/minecraft/profile/lookup/name"

// lookupProfileByName asks Mojang for a name's profile and caches a found
// one. It blocks, as vanilla's lookup does on the server thread; a name vanilla
// rejects, a 404 or any failure is false.
// vanilla: CachedUserNameToIdResolver.lookupGameProfile
func (s *Server) lookupProfileByName(name string) (player.Profile, bool) {
	if len(name) > 16 || protocol.HasDisallowedNameChar(name) {
		return player.Profile{}, false
	}
	base := s.profileLookupServer
	if base == "" {
		base = defaultProfileLookupServer
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(base + "/" + url.PathEscape(name))
	if err != nil {
		return player.Profile{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return player.Profile{}, false
	}
	var body struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil || body.Name == "" {
		return player.Profile{}, false
	}
	id, err := uuid.Parse(body.ID)
	if err != nil {
		return player.Profile{}, false
	}
	profile := player.Profile{UUID: id, Name: body.Name}
	s.usercache.add(profile)
	return profile, true
}
