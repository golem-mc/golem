package server

import (
	"log/slog"
	"strings"

	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/registry"
)

// loadPackLoot reads the enabled packs' loot tables, predicates and item modifiers; a file
// that fails to decode is logged and skipped, leaving the built-in one.
// vanilla: ReloadableServerRegistries.reload, LootDataType
func loadPackLoot(dirs []string, decoder datapack.ResourceDecoder, skip func(file string, err error)) (map[string]registry.LootTable, map[string]registry.LootCondition, map[string]registry.LootFunction, error) {
	rawTables, err := datapack.LoadResources(dirs, "loot_table", skip)
	if err != nil {
		return nil, nil, nil, err
	}
	tables := make(map[string]registry.LootTable, len(rawTables))
	for id, raw := range rawTables {
		table, err := decoder.DecodeLootTable(raw)
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
		cond, err := decoder.DecodePredicate(raw)
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
		fn, err := decoder.DecodeItemModifier(raw)
		if err != nil {
			slog.Error("Couldn't parse element item_modifier/" + id + ": " + err.Error())
			continue
		}
		modifiers[id] = fn
	}
	return tables, predicates, modifiers, nil
}

// hiddenLoot returns the built-in loot tables a pack's filter hides.
// vanilla: MultiPackResourceManager
func hiddenLoot(dirs []string, tables map[string]registry.LootTable) map[string]bool {
	filters := datapack.LoadFilters(dirs)
	if !filters.Any() {
		return nil
	}
	hidden := map[string]bool{}
	for _, name := range registry.BuiltinLootTableNames() {
		ns, path, _ := strings.Cut(name, ":")
		if _, packed := tables[name]; packed || !filters.Builtin(ns, "loot_table/"+path+".json") {
			continue
		}
		hidden[name] = true
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

// loadPackSlotSources fills the candidate layer. A source that names another
// resolves it whatever the files' order, so each pass that decodes one more
// lets a subsequent pass find it.
// vanilla: RegistryDataLoader.RELOADABLE_REGISTRIES
func loadPackSlotSources(dirs []string, decoder datapack.ResourceDecoder, sources map[string]registry.SlotSource, skip func(file string, err error)) error {
	raws, err := datapack.LoadResources(dirs, "slot_source", skip)
	if err != nil {
		return err
	}
	for progress := true; progress && len(sources) < len(raws); {
		progress = false
		for id, raw := range raws {
			if _, done := sources[id]; done {
				continue
			}
			if source, err := decoder.DecodeSlotSource(raw); err == nil {
				sources[id] = source
				progress = true
			}
		}
	}
	for id, raw := range raws {
		if _, done := sources[id]; !done {
			_, err := decoder.DecodeSlotSource(raw)
			slog.Error("Couldn't parse element slot_source/" + id + ": " + err.Error())
		}
	}
	return nil
}
