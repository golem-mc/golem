package management

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/player"
)

// decodeList decodes a JSON array, each element through one.
// vanilla: Codec.listOf
func decodeList[T any](raw json.RawMessage, one func(json.RawMessage) (T, error)) ([]T, error) {
	var elems []json.RawMessage
	if json.Unmarshal(raw, &elems) != nil || elems == nil {
		return nil, codecError("Not a json array: " + compactJSON(raw))
	}
	out := make([]T, 0, len(elems))
	for _, e := range elems {
		v, err := one(e)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// decodePlayer decodes PlayerDto.CODEC: both fields are optional, but a
// present one must be well-formed.
func decodePlayer(raw json.RawMessage) (playerIn, error) {
	var p playerIn
	obj, err := jsonObject(raw)
	if err != nil {
		return p, err
	}
	if v, ok := field(obj, "name"); ok {
		var name string
		if json.Unmarshal(v, &name) != nil {
			return p, codecError("Not a string: " + compactJSON(v))
		}
		p.Name = &name
	}
	if v, ok := field(obj, "id"); ok {
		var str string
		if json.Unmarshal(v, &str) != nil {
			return p, codecError("Not a string: " + compactJSON(v))
		}
		id, err := uuid.Parse(str)
		if err != nil {
			return p, codecError(fmt.Sprintf("Invalid UUID %s: %v", str, err))
		}
		p.ID = &id
	}
	return p, nil
}

// lookup resolves p through the host; false when it names nobody.
func (s *Server) lookup(p playerIn) (player.Profile, bool) {
	name := ""
	if p.Name != nil {
		name = *p.Name
	}
	if p.ID == nil && p.Name == nil {
		return player.Profile{}, false
	}
	return s.host.LookupProfile(p.ID, name)
}

// resolvePlayers decodes a [player] param and resolves each one, skipping
// those that name nobody. vanilla: MinecraftPlayerListService.getUser
func (s *Server) resolvePlayers(raw json.RawMessage) ([]player.Profile, error) {
	in, err := decodeList(raw, decodePlayer)
	if err != nil {
		return nil, errInvalidParams(err.Error())
	}
	out := make([]player.Profile, 0, len(in))
	for _, p := range in {
		if profile, ok := s.lookup(p); ok {
			out = append(out, profile)
		}
	}
	return out, nil
}
