package server

import (
	"errors"
	"strings"

	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/registry"
)

// loadPackLoot reads the enabled packs' loot tables, predicates and item modifiers; a file
// that vanilla's codec refuses is added to bad, except a
// weighted list whose total weight overflows, which fails the whole load.
// vanilla: ReloadableServerRegistries.reload, LootDataType
func loadPackLoot(layers *registry.PackLayers, dirs []string, skip func(file string, err error), bad *packErrors) (map[string]registry.LootTable, map[string]registry.LootCondition, map[string]registry.LootFunction, error) {
	rawTables, err := datapack.LoadResources(dirs, "loot_table", skip)
	if err != nil {
		return nil, nil, nil, err
	}
	tables := make(map[string]registry.LootTable, len(rawTables))
	for id, raw := range rawTables {
		table, err := datapack.DecodeLootTable(layers, raw)
		if err != nil {
			if errors.Is(err, datapack.ErrWeightOverflow) {
				return nil, nil, nil, err
			}
			bad.add("loot_table", id, err)
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
		cond, err := datapack.DecodePredicate(layers, raw)
		if err != nil {
			if errors.Is(err, datapack.ErrWeightOverflow) {
				return nil, nil, nil, err
			}
			bad.add("predicate", id, err)
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
		fn, err := datapack.DecodeItemModifier(layers, raw)
		if err != nil {
			if errors.Is(err, datapack.ErrWeightOverflow) {
				return nil, nil, nil, err
			}
			bad.add("item_modifier", id, err)
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

// loadPackSlotSources stages the enabled packs' slot sources in layers; a
// file that fails to decode is added to bad. A source that names
// another resolves it whatever the files' order, so a pass repeats while it
// still lets one more decode.
// vanilla: RegistryDataLoader.RELOADABLE_REGISTRIES
func loadPackSlotSources(layers *registry.PackLayers, dirs []string, skip func(file string, err error), bad *packErrors) error {
	raws, err := datapack.LoadResources(dirs, "slot_source", skip)
	if err != nil {
		return err
	}
	sources := make(map[string]registry.SlotSource, len(raws))
	layers.SetSlotSources(sources)
	for progress := true; progress && len(sources) < len(raws); {
		progress = false
		for id, raw := range raws {
			if _, done := sources[id]; done {
				continue
			}
			if source, err := datapack.DecodeSlotSource(layers, raw); err == nil {
				sources[id] = source
				progress = true
			}
		}
	}
	for id, raw := range raws {
		if _, done := sources[id]; !done {
			_, err := datapack.DecodeSlotSource(layers, raw)
			bad.add("slot_source", id, err)
		}
	}
	return nil
}
