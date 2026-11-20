package server

import (
	"log/slog"
	"maps"
	"slices"

	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/registry"
)

// loadPackAdvancements reads the enabled packs' advancements; a file that
// fails to decode is logged and skipped, leaving the built-in one.
// vanilla: ServerAdvancementManager.apply
func loadPackAdvancements(dirs []string, skip func(file string, err error)) ([]registry.Advancement, error) {
	raw, err := datapack.LoadResources(dirs, "advancement", skip)
	if err != nil {
		return nil, err
	}
	var out []registry.Advancement
	for _, id := range slices.Sorted(maps.Keys(raw)) {
		adv, err := datapack.DecodeAdvancement(id, raw[id])
		if err != nil {
			slog.Error("Couldn't parse element advancement/" + id + ": " + err.Error())
			continue
		}
		out = append(out, adv)
	}
	return out, nil
}

// applyPackAdvancements layers the packs' advancements over the vanilla
// tree and puts every online player's progress over the result.
// vanilla: PlayerList.reloadResources, PlayerAdvancements.reload
func (s *Server) applyPackAdvancements(pack []registry.Advancement) {
	tree := advancement.ApplyPack(pack)
	s.mu.RLock()
	players := s.clientsLocked()
	s.mu.RUnlock()
	for _, c := range players {
		if progress := c.advancements.Load(); progress != nil && progress.Tree() != tree {
			c.AnnounceAdvancements()
			progress.Reload(tree)
		}
	}
}
