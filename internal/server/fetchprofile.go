package server

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// FetchProfileByName is the profile of an online player, then of a user cache
// entry, with that name in any case. This server has no Mojang lookup.
// vanilla: ProfileResolver.fetchByName
func (ws worldScope) FetchProfileByName(name string) (player.Profile, bool) {
	ws.srv.mu.RLock()
	for _, c := range ws.srv.clients {
		if strings.EqualFold(c.player.Name, name) {
			profile := c.player.Profile
			ws.srv.mu.RUnlock()
			return profile, true
		}
	}
	ws.srv.mu.RUnlock()
	for _, profile := range ws.srv.usercache.profiles() {
		if strings.EqualFold(profile.Name, name) {
			return profile, true
		}
	}
	return player.Profile{}, false
}

// FetchProfileByID is FetchProfileByName for a UUID.
// vanilla: ProfileResolver.fetchById
func (ws worldScope) FetchProfileByID(id uuid.UUID) (player.Profile, bool) {
	if c := ws.srv.clientByUUID(id); c != nil {
		return c.player.Profile, true
	}
	for _, profile := range ws.srv.usercache.profiles() {
		if profile.UUID == id {
			return profile, true
		}
	}
	return player.Profile{}, false
}

// AvatarProfile is the profile of a player or a mannequin.
// vanilla: Avatar.getProfile
func (ws worldScope) AvatarProfile(e command.Entity) (nbt.Compound, bool) {
	return avatarProfile(ws.srv, e)
}

func avatarProfile(s *Server, e command.Entity) (nbt.Compound, bool) {
	switch body := e.(type) {
	case *client:
		return command.ProfileTag(body.player.Profile), true
	case *entitySnapshot:
		if ent, ok := body.ref.(*mannequinEntity); ok {
			return mannequinProfileTag(s, ent), true
		}
	}
	return nil, false
}

func mannequinProfileTag(s *Server, ent *mannequinEntity) nbt.Compound {
	s.mannequinsMu.Lock()
	wire := mannequinProfileWire(ent.mannequin)
	s.mannequinsMu.Unlock()
	tag, _ := protocol.ProfileToTag(wire)
	if tag == nil {
		tag = nbt.Compound{}
	}
	return tag
}
