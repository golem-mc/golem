package server

import (
	"log/slog"
	"slices"

	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// Scoreboard tags: each live body's set and every entity file's Tags
// string list. Players keep theirs on player.Player; the rest share these
// helpers.
// vanilla: Entity's tags field, its save fields and its addTag, removeTag

// tagsOfTag reads a compound's Tags list; empty or absent is none.
// Duplicates read as one (vanilla keeps a set), and over the 1024 cap reads
// as none. vanilla: Entity's read of Tags, Entity.addTag
func tagsOfTag(compound nbt.Compound) []string {
	list, ok := nbt.Get[nbt.List](compound, "Tags")
	if !ok || list.Elem != nbt.TypeString {
		return nil
	}
	if len(list.Items) > player.TagCap {
		return nil
	}
	var tags []string
	for _, item := range list.Items {
		s, ok := item.(nbt.String)
		if !ok {
			continue
		}
		if tag := string(s); !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	return tags
}

// putTags writes Tags, omitted when empty as Entity's save does.
// vanilla: Entity's store of Tags
func putTags(tag nbt.CompoundTag, tags []string) {
	if len(tags) == 0 {
		return
	}
	items := make([]nbt.Tag, len(tags))
	for i, t := range tags {
		items[i] = nbt.String(t)
	}
	tag.Set("Tags", nbt.List{Elem: nbt.TypeString, Items: items})
}

// tagTeam is a loaded body's UUID and the tag whose Team it joins.
type tagTeam struct {
	identity uuid.UUID
	tag      nbt.Compound
}

// joinTagTeam puts a loaded body on its tag's team, warning when there is
// no such team. It broadcasts, so it runs with no entity lock held.
// vanilla: Entity.load
func (s *Server) joinTagTeam(t tagTeam) {
	if team, joined := command.JoinTagTeam(s.scoreboard, t.identity, t.tag); team != "" && !joined {
		slog.Warn("unable to add entity to team (that team probably doesn't exist)", "team", team, "uuid", t.identity)
	}
}

// collectTeams adds compound's Team, and that of every rider nested in its
// Passengers, to r.teams; a rider already live is left to its own copy.
// vanilla: EntityType.loadEntityRecursive, Entity.load
func (r *wakeResult) collectTeams(compound nbt.Compound, known map[uuid.UUID]bool) {
	r.collectTeam(compound)
	list, _ := nbt.Get[nbt.List](compound, "Passengers")
	for _, item := range list.Items {
		if c, ok := nbt.CompoundView(item); ok {
			if identity, ok := entityUUIDOfTag(c); !ok || !known[identity] {
				r.collectTeams(c, known)
			}
		}
	}
}

// collectTeam adds compound's own Team, not its riders', to r.teams.
func (r *wakeResult) collectTeam(compound nbt.Compound) {
	if team, ok := compound["Team"]; ok {
		// A copy of the key: a carried compound loses it before the join.
		if identity, ok := entityUUIDOfTag(compound); ok {
			r.teams = append(r.teams, tagTeam{identity: identity, tag: nbt.Compound{"Team": team}})
		}
	}
}

// withoutTeam drops Team from a compound carried for write-back, and from
// its nested riders: it was joined once at the wake, and a save never
// writes it. vanilla: Entity.saveWithoutId
func withoutTeam(compound nbt.CompoundTag) nbt.CompoundTag {
	compound.Delete("Team")
	list, _ := nbt.Get[nbt.List](compound.Entries(), "Passengers")
	for _, item := range list.Items {
		if c, ok := item.(nbt.CompoundTag); ok {
			withoutTeam(c)
		}
	}
	return compound
}

// Reject candidates before copying the live set's iteration order.
func rejectsEntityTags(q *command.EntityQuery, tags nbt.StringSet) bool {
	return q.RejectsTags(tags.Len(), tags.Contains)
}
