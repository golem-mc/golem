package registry

import (
	"slices"
	"sync"
	"sync/atomic"
)

// lootState is the data packs' loot tables, predicates and item modifiers,
// replaced whole so a roll never sees half a reload.
type lootState struct {
	tables     map[string]LootTable
	predicates map[string]LootCondition
	modifiers  map[string]LootFunction
	// hidden holds the built-in table names a pack's filter hides.
	hidden map[string]bool
}

var packLoot atomic.Pointer[lootState]

// LootReferenceCondition is the condition that a loaded predicate holds.
// vanilla: ConditionReference
type LootReferenceCondition struct {
	Name string
}

func (LootReferenceCondition) isLootCondition() {}

// ApplyPackLoot makes the data packs' loot tables override the built-in ones
// by name and makes the predicates and item modifiers available to
// FindPredicate and FindItemModifier. Nil maps clear the layer.
func ApplyPackLoot(tables map[string]LootTable, predicates map[string]LootCondition, modifiers map[string]LootFunction) {
	ApplyPackLootHiding(tables, predicates, modifiers, nil)
}

// ApplyPackLootHiding is ApplyPackLoot with the built-in tables in hidden
// absent, as if their files did not exist.
func ApplyPackLootHiding(tables map[string]LootTable, predicates map[string]LootCondition, modifiers map[string]LootFunction, hidden map[string]bool) {
	if len(tables) == 0 && len(predicates) == 0 && len(modifiers) == 0 && len(hidden) == 0 {
		packLoot.Store(nil)
		return
	}
	packLoot.Store(&lootState{tables: tables, predicates: predicates, modifiers: modifiers, hidden: hidden})
}

// BuiltinLootTableNames lists the built-in loot tables, in no order.
func BuiltinLootTableNames() []string {
	names := make([]string, 0, len(LootTables))
	for name := range LootTables {
		names = append(names, name)
	}
	return names
}

// FindItemModifier returns the item modifier a data pack defines under the
// name.
func FindItemModifier(name string) (LootFunction, bool) {
	st := packLoot.Load()
	if st == nil {
		return nil, false
	}
	f, ok := st.modifiers[name]
	return f, ok
}

// PackLootTable returns the table a data pack defines under the name, not
// the built-in one.
func PackLootTable(name string) (LootTable, bool) {
	st := packLoot.Load()
	if st == nil {
		return LootTable{}, false
	}
	t, ok := st.tables[name]
	return t, ok
}

// DecodeBuiltinPredicate reads a built-in predicate's JSON; the data pack
// decoder installs it, since this package cannot import that one.
var DecodeBuiltinPredicate func(data []byte) (LootCondition, error)

// DecodeContextProvider reads a context int (or, when float, float)
// provider's JSON, answering the codec's message on failure; the data pack
// decoder installs it, since this package cannot import that one.
var DecodeContextProvider func(data []byte, float bool) (LootNumber, string)

var (
	builtinPredicateMu    sync.Mutex
	builtinPredicateCache = map[string]LootCondition{}
)

// FindPredicate returns the predicate a data pack defines under the name, or
// the built-in one.
// vanilla: Registries.PREDICATE
func FindPredicate(name string) (LootCondition, bool) {
	if st := packLoot.Load(); st != nil {
		if c, ok := st.predicates[name]; ok {
			return c, true
		}
	}
	src, ok := predicateJSON[name]
	if !ok || DecodeBuiltinPredicate == nil {
		return nil, false
	}
	builtinPredicateMu.Lock()
	defer builtinPredicateMu.Unlock()
	if c, ok := builtinPredicateCache[name]; ok {
		return c, true
	}
	c, err := DecodeBuiltinPredicate([]byte(src))
	if err != nil {
		return nil, false
	}
	builtinPredicateCache[name] = c
	return c, true
}

// PredicateNames lists the loaded predicates, sorted.
func PredicateNames() []string {
	st := packLoot.Load()
	if st == nil {
		return nil
	}
	names := make([]string, 0, len(st.predicates))
	for name := range st.predicates {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// LootTableNames lists every loot table by name, the data packs' among the
// built-in ones, sorted.
func LootTableNames() []string {
	st := packLoot.Load()
	names := make([]string, 0, len(LootTables))
	for name := range LootTables {
		if st == nil || !st.hidden[name] {
			names = append(names, name)
		}
	}
	if st != nil {
		for name := range st.tables {
			if _, builtin := LootTables[name]; !builtin {
				names = append(names, name)
			}
		}
	}
	slices.Sort(names)
	return names
}

// ItemTagMembers returns the names of the items a tag groups, or false for a
// tag this version does not define.
func ItemTagMembers(tag string) ([]string, bool) {
	for _, r := range tagsNow.Load().builtin {
		if r.Registry != "minecraft:item" {
			continue
		}
		for _, t := range r.Tags {
			if t.Name != tag {
				continue
			}
			names := make([]string, 0, len(t.IDs))
			for _, id := range t.IDs {
				if name, ok := ItemName(id); ok {
					names = append(names, name)
				}
			}
			return names, true
		}
	}
	return nil, false
}

// EnchantmentTagMembers returns the names of the enchantments a tag groups,
// or false for a tag this version does not define.
func EnchantmentTagMembers(tag string) ([]string, bool) {
	return SyncTagMembers("minecraft:enchantment")(tag)
}

// BiomeTagMembers returns the names of the biomes a tag groups, or false for
// a tag this version does not define.
func BiomeTagMembers(tag string) ([]string, bool) {
	return SyncTagMembers("minecraft:worldgen/biome")(tag)
}

// InstrumentTagMembers returns the names of the instruments a tag groups, or
// false for a tag this version does not define.
func InstrumentTagMembers(tag string) ([]string, bool) {
	return SyncTagMembers("minecraft:instrument")(tag)
}

// SyncTagMembers returns a resolver for the tags of one synchronized
// registry; it reports false for a tag no data pack defines, while a defined
// empty tag is an empty set.
// vanilla: MappedRegistry.get(TagKey) returns empty for an unbound tag, and
// HolderSetCodec reports the missing tag
func SyncTagMembers(registryName string) func(string) ([]string, bool) {
	return func(tag string) ([]string, bool) {
		for _, r := range tagsNow.Load().sync {
			if r.Registry != registryName {
				continue
			}
			for _, t := range r.Tags {
				if t.Name == tag {
					return slices.Clone(t.Entries), true
				}
			}
		}
		return nil, false
	}
}

// EmptyTagMembers resolves a tag of a registry that defines none (the
// client's built-in attribute registry): every tag is missing.
func EmptyTagMembers(string) ([]string, bool) { return nil, false }
