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
	filters := datapack.LoadFilters(dirs)
	for _, reg := range registry.PackTagRegistries() {
		dir := strings.TrimPrefix(reg, "minecraft:")
		raw, err := datapack.LoadTags(dirs, dir, skip)
		if err != nil {
			return nil, err
		}
		// A built-in tag file a filter hides is gone: the tag starts empty.
		if filters.Any() {
			for _, name := range builtinTagNames(reg) {
				ns, path, _ := strings.Cut(name, ":")
				if filters.Builtin(ns, "tags/"+dir+"/"+path+".json") {
					tag := raw[name]
					tag.Replace = true
					raw[name] = tag
				}
			}
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

// preparePackTags builds a lookup for decoding and reports whether to publish
// it. A server without pack tags reuses the process-wide tags unless it had
// tags of its own to clear.
func (s *Server) preparePackTags(tags registry.PackTags) (*registry.TagLookup, bool) {
	if len(tags) == 0 && !s.packTagsApplied {
		return registry.CurrentTagLookup(), false
	}
	lookup, problems := registry.BuildPackTags(tags)
	for _, problem := range problems {
		slog.Error("Couldn't load tag " + problem)
	}
	return lookup, true
}

// builtinTagNames lists the built-in tags of a registry.
func builtinTagNames(reg string) []string {
	var names []string
	for _, r := range registry.Tags {
		if r.Registry == reg {
			for _, t := range r.Tags {
				names = append(names, t.Name)
			}
		}
	}
	for _, r := range registry.BuiltinTags {
		if r.Registry == reg {
			for _, t := range r.Tags {
				names = append(names, t.Name)
			}
		}
	}
	return names
}
