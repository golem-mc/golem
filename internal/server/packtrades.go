package server

import (
	"log/slog"

	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/registry"
)

// loadPackTrades reads the packs' villager_trade files and tags and trade_set
// files and installs the resolved trade sets over the built-in ones. Vanilla
// loads these world registries once per start, so /reload leaves them.
// vanilla: RegistryDataLoader.WORLD_REGISTRIES
func loadPackTrades(dirs []string, skip func(file string, err error)) error {
	files, err := datapack.LoadResources(dirs, "villager_trade", skip)
	if err != nil {
		return err
	}
	setFiles, err := datapack.LoadResources(dirs, "trade_set", skip)
	if err != nil {
		return err
	}
	rawTags, err := datapack.LoadTags(dirs, "villager_trade", skip)
	if err != nil {
		return err
	}
	if len(files)+len(setFiles)+len(rawTags) == 0 {
		registry.ApplyPackTradeSets(nil)
		return nil
	}
	trades := make(map[string]registry.VillagerTrade, len(files))
	for id, data := range files {
		trade, err := datapack.DecodeVillagerTrade(data)
		if err != nil {
			skip("villager_trade "+id, err)
			continue
		}
		trades[id] = trade
	}
	sets := make(map[string]registry.PackTradeSet, len(setFiles))
	for id, data := range setFiles {
		set, err := datapack.DecodeTradeSet(data)
		if err != nil {
			skip("trade_set "+id, err)
			continue
		}
		sets[id] = set
	}
	tags := make(map[string]registry.PackTag, len(rawTags))
	for id, tag := range rawTags {
		entries := make([]registry.PackTagEntry, len(tag.Entries))
		for i, e := range tag.Entries {
			entries[i] = registry.PackTagEntry{ID: e.ID, Tag: e.Tag, Required: e.Required}
		}
		tags[id] = registry.PackTag{Replace: tag.Replace, Entries: entries}
	}
	resolved, problems := registry.BuildTradeSets(trades, tags, sets)
	for _, problem := range problems {
		slog.Warn("Data pack trade skipped: " + problem)
	}
	registry.ApplyPackTradeSets(resolved)
	return nil
}
