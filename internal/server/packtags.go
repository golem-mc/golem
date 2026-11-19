package server

import (
	"log/slog"
	"strings"

	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/registry"
)

// loadPackTags reads the pack tags of every registry a pack can change.
// vanilla: TagLoader.loadTagsForRegistry
func loadPackTags(dirs []string, skip func(file string, err error)) (registry.PackTags, error) {
	out := registry.PackTags{}
	for _, reg := range registry.PackTagRegistries() {
		raw, err := datapack.LoadTags(dirs, strings.TrimPrefix(reg, "minecraft:"), skip)
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			continue
		}
		tags := make(map[string]registry.PackTag, len(raw))
		for id, tag := range raw {
			entries := make([]registry.PackTagEntry, len(tag.Entries))
			for i, e := range tag.Entries {
				entries[i] = registry.PackTagEntry{ID: e.ID, Tag: e.Tag, Required: e.Required}
			}
			tags[id] = registry.PackTag{Replace: tag.Replace, Entries: entries}
		}
		out[reg] = tags
	}
	for _, reg := range unappliedTagRegistries {
		raw, err := datapack.LoadTags(dirs, strings.TrimPrefix(reg, "minecraft:"), skip)
		if err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			slog.Warn("Ignoring data pack tags of a registry whose tags are not loadable", "registry", reg, "tags", len(raw))
		}
	}
	return out, nil
}

// unappliedTagRegistries get built-in tags sent but pack tags ignored.
var unappliedTagRegistries = []string{"minecraft:game_event", "minecraft:potion", "minecraft:point_of_interest_type"}

// applyPackTags layers the tags over the built-ins for every lookup and the
// next Update Tags. The tables are process-wide, so a server without pack
// tags leaves them alone unless it changed them itself.
func (s *Server) applyPackTags(tags registry.PackTags) {
	if len(tags) == 0 && !s.packTagsApplied {
		return
	}
	s.packTagsApplied = len(tags) > 0
	for _, problem := range registry.ApplyPackTags(tags) {
		slog.Error("Couldn't load tag " + problem)
	}
}
