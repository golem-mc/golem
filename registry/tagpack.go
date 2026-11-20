package registry

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
)

// tagState is the tag data every lookup reads: the built-in tables with the
// data packs' tags layered over them, and the indexes derived from them. It is
// replaced whole, so a reader never sees a half-applied reload.
type tagState struct {
	sync    []TaggedRegistry
	builtin []BuiltinTaggedRegistry

	blockTags  map[string][]string
	blockTable BlockTagLookup

	bannerPatternIDs   map[string][]int32
	paintingVariantIDs map[string][]int32
	biome              map[string]map[string]bool
	biomeTable         BiomeTagLookup
	damageTable        damageTypeTagLookup
	enchantment        map[string]map[string]bool
	enchantmentIDs     map[string][]int32
	entityType         map[string]map[int32]bool
	item               map[string]map[int32]bool
	itemTable          ItemTagLookup
	entityTable        EntityTypeTagLookup
}

func newTagState(sync []TaggedRegistry, builtin []BuiltinTaggedRegistry, blocks map[string][]string) *tagState {
	return &tagState{
		sync:               sync,
		builtin:            builtin,
		blockTags:          blocks,
		blockTable:         newBlockTagTable(blocks),
		bannerPatternIDs:   syncTagIDs(sync, "minecraft:banner_pattern"),
		paintingVariantIDs: syncTagIDs(sync, "minecraft:painting_variant"),
		biome:              syncTagIndex(sync, "minecraft:worldgen/biome"),
		biomeTable:         newBiomeTagTable(syncTagIndex(sync, "minecraft:worldgen/biome")),
		damageTable:        newDamageTypeTagTable(syncTagIndex(sync, "minecraft:damage_type")),
		enchantment:        syncTagIndex(sync, "minecraft:enchantment"),
		enchantmentIDs:     enchantmentTagIDs(sync),
		entityType:         builtinTagIndex(builtin, "minecraft:entity_type"),
		item:               builtinTagIndex(builtin, "minecraft:item"),
		itemTable:          newItemTagTable(itemTagIDs(builtin)),
		entityTable:        newEntityTypeTagTable(entityTypeTagIDs(builtin)),
	}
}

// tagsNow is built by variable initialization, not init(), so the package
// level tag IDs can resolve against it.
var tagsNow = func() *atomic.Pointer[tagState] {
	p := new(atomic.Pointer[tagState])
	p.Store(newTagState(Tags, BuiltinTags, BlockTags))
	return p
}()

// CurrentTags returns the tags of the synchronized registries and of the built-in
// registries as Update Tags sends them: the built-in tables with the data
// packs' tags layered over them.
func CurrentTags() ([]TaggedRegistry, []BuiltinTaggedRegistry) {
	st := tagsNow.Load()
	return st.sync, st.builtin
}

// PackTagEntry is one value of a data pack's tag file.
// vanilla: TagEntry
type PackTagEntry struct {
	ID       string // registry name, or tag name with Tag set
	Tag      bool
	Required bool // a missing required entry drops the whole tag
}

// PackTag is a tag as the packs define it, layered in pack order.
type PackTag struct {
	// Replace discards the built-in entries too: the vanilla tags are the
	// first pack in the stack.
	Replace bool
	Entries []PackTagEntry
}

// PackTags holds the data packs' tags by registry name, then tag name.
type PackTags map[string]map[string]PackTag

// PackTagRegistries lists the registries whose tags a data pack can change:
// the block, item, entity type and fluid registries and every synchronized
// registry the server sends tags for.
func PackTagRegistries() []string {
	out := []string{"minecraft:block", "minecraft:item", "minecraft:entity_type", "minecraft:fluid"}
	for _, r := range Tags {
		out = append(out, r.Registry)
	}
	for _, r := range Current() {
		if !slices.Contains(out, r.Name) {
			out = append(out, r.Name)
		}
	}
	return out
}

// ApplyPackTags layers the data packs' tags over the built-in tags and makes
// the result what every tag lookup and Update Tags see. Tags that do not load
// are left out and described in the returned messages.
// vanilla: TagLoader.build
func ApplyPackTags(packs PackTags) []string {
	var problems []string
	// Registries without vanilla tags start empty; vanilla loads tags for
	// every registry.
	var extra []string
	for name := range packs {
		if _, ok := Get(name); !ok || len(packs[name]) == 0 {
			continue
		}
		if !slices.ContainsFunc(Tags, func(r TaggedRegistry) bool { return r.Registry == name }) && !slices.ContainsFunc(BuiltinTags, func(r BuiltinTaggedRegistry) bool { return r.Registry == name }) {
			extra = append(extra, name)
		}
	}
	slices.Sort(extra)
	all := slices.Clone(Tags)
	for _, name := range extra {
		all = append(all, TaggedRegistry{Registry: name})
	}
	sync := make([]TaggedRegistry, len(all))
	for i, r := range all {
		sync[i] = r
		pack := packs[r.Registry]
		reg, ok := Get(r.Registry)
		if len(pack) == 0 || !ok {
			continue
		}
		flat := make(map[string][]string, len(r.Tags))
		order := make([]string, len(r.Tags))
		for j, t := range r.Tags {
			flat[t.Name] = t.Entries
			order[j] = t.Name
		}
		res, probs := resolveTags(baseEntries(r.Registry, flat), order, pack, func(n string) bool { return reg.ID(n) >= 0 })
		problems = append(problems, probs...)
		sync[i].Tags = nil
		for _, name := range res.order {
			sync[i].Tags = append(sync[i].Tags, Tag{Name: name, Entries: res.tags[name]})
		}
	}

	builtin := make([]BuiltinTaggedRegistry, len(BuiltinTags))
	blocks := BlockTags
	for i, r := range BuiltinTags {
		builtin[i] = r
		pack := packs[r.Registry]
		if len(pack) == 0 {
			continue
		}
		var names []string
		var idOf func(string) (int32, bool)
		switch r.Registry {
		case "minecraft:block":
			idOf = func(n string) (int32, bool) { b, ok := blockByName[n]; return int32(b), ok }
		case "minecraft:item":
			idOf = ItemID
		case "minecraft:entity_type":
			idOf = EntityTypeID
		case "minecraft:fluid":
			idOf = func(n string) (int32, bool) {
				i := slices.Index(fluidNames, n)
				return int32(i), i >= 0
			}
		default:
			continue
		}
		nameOf := func(id int32) string { return builtinName(r.Registry, id) }
		flat := make(map[string][]string, len(r.Tags))
		order := make([]string, len(r.Tags))
		for j, t := range r.Tags {
			names = names[:0:0]
			for _, id := range t.IDs {
				names = append(names, nameOf(id))
			}
			flat[t.Name] = names
			order[j] = t.Name
		}
		res, probs := resolveTags(baseEntries(r.Registry, flat), order, pack, func(n string) bool { _, ok := idOf(n); return ok })
		problems = append(problems, probs...)
		builtin[i].Tags = nil
		for _, name := range res.order {
			ids := make([]int32, 0, len(res.tags[name]))
			for _, n := range res.tags[name] {
				id, _ := idOf(n)
				ids = append(ids, id)
			}
			builtin[i].Tags = append(builtin[i].Tags, BuiltinTag{Name: name, IDs: ids})
		}
		if r.Registry == "minecraft:block" {
			blocks = res.tags
		}
	}
	itemTagsPacked.Store(len(packs["minecraft:item"]) > 0)
	tagsNow.Store(newTagState(sync, builtin, blocks))
	tagsGeneration.Add(1)
	return problems
}

// tagsGeneration counts the tag states ApplyPackTags has installed.
var tagsGeneration atomic.Uint64

// TagsGeneration changes whenever the tags do, so a caller may cache a tag
// lookup for as long as it reads the same value.
func TagsGeneration() uint64 { return tagsGeneration.Load() }

// builtinName names an entry of a built-in registry by its numeric ID.
func builtinName(registry string, id int32) string {
	if id < 0 {
		return ""
	}
	switch registry {
	case "minecraft:block":
		if Block(id) < BlockCount {
			return Block(id).Name()
		}
	case "minecraft:item":
		if int(id) < len(Items) {
			return Items[id]
		}
	case "minecraft:entity_type":
		if int(id) < len(EntityTypes) {
			return EntityTypes[id]
		}
	case "minecraft:fluid":
		if int(id) < len(fluidNames) {
			return fluidNames[id]
		}
	}
	return ""
}

// baseEntries returns the built-in tags of a registry with their references
// to other tags kept, so a pack's change to one tag reaches the tags that
// include it. A registry without definitions falls back to the flattened tags.
func baseEntries(registry string, flat map[string][]string) map[string][]PackTagEntry {
	defs, ok := tagDefs[registry]
	out := make(map[string][]PackTagEntry, len(flat))
	if !ok {
		for name, members := range flat {
			for _, n := range members {
				out[name] = append(out[name], PackTagEntry{ID: n, Required: true})
			}
			if _, has := out[name]; !has {
				out[name] = []PackTagEntry{}
			}
		}
		return out
	}
	for name, entries := range defs {
		list := make([]PackTagEntry, len(entries))
		for i, e := range entries {
			tag := strings.HasPrefix(e, "#")
			list[i] = PackTagEntry{ID: strings.TrimPrefix(e, "#"), Tag: tag, Required: true}
		}
		out[name] = list
	}
	return out
}

type resolvedTags struct {
	tags  map[string][]string
	order []string // base tags in their own order, then the others sorted
}

// resolveTags builds every tag of one registry from the built-in entries and
// the packs'. A tag with a missing required entry or required tag reference is
// left out, as are tags caught in a reference cycle.
// vanilla: TagLoader.build, TagEntry.build
func resolveTags(base map[string][]PackTagEntry, baseOrder []string, pack map[string]PackTag, exists func(string) bool) (resolvedTags, []string) {
	var problems []string
	done := make(map[string][]string, len(base)+len(pack))
	const visiting = 1
	state := map[string]int{}
	var build func(name string) ([]string, bool)
	build = func(name string) ([]string, bool) {
		if v, ok := done[name]; ok {
			return v, v != nil
		}
		p, packed := pack[name]
		b, builtin := base[name]
		if !packed && !builtin {
			return nil, false
		}
		if state[name] == visiting {
			return nil, false
		}
		state[name] = visiting
		defer delete(state, name)
		var values []string
		seen := map[string]bool{}
		add := func(n string) {
			if !seen[n] {
				seen[n] = true
				values = append(values, n)
			}
		}
		entries := p.Entries
		if !p.Replace {
			entries = append(slices.Clone(b), entries...)
		}
		var missing []string
		for _, e := range entries {
			if e.Tag {
				members, ok := build(e.ID)
				if !ok {
					if e.Required {
						missing = append(missing, "#"+e.ID)
					}
					continue
				}
				for _, n := range members {
					add(n)
				}
				continue
			}
			if !exists(e.ID) {
				if e.Required {
					missing = append(missing, e.ID)
				}
				continue
			}
			add(e.ID)
		}
		if len(missing) > 0 {
			problems = append(problems, fmt.Sprintf("%s: %v", name, missing))
			done[name] = nil
			return nil, false
		}
		done[name] = nonNil(values)
		return done[name], true
	}

	names := make([]string, 0, len(base)+len(pack))
	names = append(names, baseOrder...)
	var extra []string
	for name := range pack {
		if _, ok := base[name]; !ok {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	names = append(names, extra...)
	res := resolvedTags{tags: make(map[string][]string, len(names))}
	for _, name := range names {
		if members, ok := build(name); ok {
			res.tags[name] = members
			res.order = append(res.order, name)
		}
	}
	return res, problems
}

// nonNil keeps an empty tag distinct from a tag that failed to load.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
