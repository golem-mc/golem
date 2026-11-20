package server

import (
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// loadPackLoot reads the enabled packs' loot tables, predicates and item modifiers; a file
// that fails to decode is logged and skipped, leaving the built-in one.
// vanilla: ReloadableServerRegistries.reload, LootDataType
func loadPackLoot(dirs []string, skip func(file string, err error)) (map[string]registry.LootTable, map[string]registry.LootCondition, map[string]registry.LootFunction, error) {
	rawTables, err := datapack.LoadResources(dirs, "loot_table", skip)
	if err != nil {
		return nil, nil, nil, err
	}
	tables := make(map[string]registry.LootTable, len(rawTables))
	for id, raw := range rawTables {
		table, err := datapack.DecodeLootTable(raw)
		if err != nil {
			slog.Error("Couldn't parse element loot_table/" + id + ": " + err.Error())
			continue
		}
		tables[id] = table
	}
	rawPredicates, err := datapack.LoadResources(dirs, "predicate", skip)
	if err != nil {
		return nil, nil, nil, err
	}
	predicates := make(map[string]registry.LootCondition, len(rawPredicates))
	for id, raw := range rawPredicates {
		cond, err := datapack.DecodePredicate(raw)
		if err != nil {
			slog.Error("Couldn't parse element predicate/" + id + ": " + err.Error())
			continue
		}
		predicates[id] = cond
	}
	rawModifiers, err := datapack.LoadResources(dirs, "item_modifier", skip)
	if err != nil {
		return nil, nil, nil, err
	}
	modifiers := make(map[string]registry.LootFunction, len(rawModifiers))
	for id, raw := range rawModifiers {
		fn, err := datapack.DecodeItemModifier(raw)
		if err != nil {
			slog.Error("Couldn't parse element item_modifier/" + id + ": " + err.Error())
			continue
		}
		modifiers[id] = fn
	}
	return tables, predicates, modifiers, nil
}

// hiddenLoot returns the built-in loot tables a pack's filter hides. A
// hand-written fishing table cannot be absent, so a hidden one is replaced by
// a table that rolls nothing in tables.
// vanilla: MultiPackResourceManager
func hiddenLoot(dirs []string, tables map[string]registry.LootTable) map[string]bool {
	filters := datapack.LoadFilters(dirs)
	if !filters.Any() {
		return nil
	}
	hidden := map[string]bool{}
	for _, name := range append(registry.BuiltinLootTableNames(), rules.FishingTableNames...) {
		ns, path, _ := strings.Cut(name, ":")
		if _, packed := tables[name]; packed || !filters.Builtin(ns, "loot_table/"+path+".json") {
			continue
		}
		if slices.Contains(rules.FishingTableNames, name) {
			tables[name] = registry.LootTable{}
		} else {
			hidden[name] = true
		}
	}
	return hidden
}

// applyPackLoot layers pack loot over the built-ins. The layer is
// process-wide, so a server without pack loot leaves it alone unless it
// changed it itself.
func (s *Server) applyPackLoot(tables map[string]registry.LootTable, predicates map[string]registry.LootCondition, modifiers map[string]registry.LootFunction, hidden map[string]bool) {
	layered := len(tables) > 0 || len(predicates) > 0 || len(modifiers) > 0 || len(hidden) > 0
	if !layered && !s.packLootApplied {
		return
	}
	s.packLootApplied = layered
	registry.ApplyPackLootHiding(tables, predicates, modifiers, hidden)
}

// loadPackSlotSources reads the enabled packs' slot sources into the
// registry; a file that fails to decode is logged and skipped. A source that
// names another resolves it whatever the files' order, so a pass repeats
// while it still lets one more decode.
// vanilla: RegistryDataLoader.RELOADABLE_REGISTRIES
func loadPackSlotSources(dirs []string, skip func(file string, err error)) error {
	raws, err := datapack.LoadResources(dirs, "slot_source", skip)
	if err != nil {
		return err
	}
	registry.ApplyPackSlotSources(nil)
	sources := make(map[string]registry.SlotSource, len(raws))
	for progress := true; progress && len(sources) < len(raws); {
		progress = false
		for id, raw := range raws {
			if _, done := sources[id]; done {
				continue
			}
			if source, err := datapack.DecodeSlotSource(raw); err == nil {
				sources[id] = source
				registry.ApplyPackSlotSources(maps.Clone(sources))
				progress = true
			}
		}
	}
	for id, raw := range raws {
		if _, done := sources[id]; !done {
			_, err := datapack.DecodeSlotSource(raw)
			slog.Error("Couldn't parse element slot_source/" + id + ": " + err.Error())
		}
	}
	return nil
}
