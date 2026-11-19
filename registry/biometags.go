package registry

import "sort"

// BiomeTagID indexes a biome's tag membership bitset. Built-in tags keep
// their sorted positions across reloads; pack-only IDs must be resolved
// again. noBiomeTag represents an unknown name.
type BiomeTagID uint16

const noBiomeTag BiomeTagID = 0xffff

var baseBiomeTagNames = func() []string {
	var names []string
	for _, r := range Tags {
		if r.Registry == "minecraft:worldgen/biome" {
			for _, t := range r.Tags {
				names = append(names, t.Name)
			}
		}
	}
	sort.Strings(names)
	return names
}()

// newBiomeTagTable assigns built-in names first, then sorted pack-only names.
func newBiomeTagTable(tags map[string]map[string]bool) BiomeTagLookup {
	names := append([]string(nil), baseBiomeTagNames...)
	builtin := make(map[string]bool, len(names))
	for _, name := range names {
		builtin[name] = true
	}
	var extra []string
	for name := range tags {
		if !builtin[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	names = append(names, extra...)
	t := BiomeTagLookup{ids: make(map[string]BiomeTagID, len(names)), words: (len(names) + 63) / 64}
	t.bits = make([]uint64, t.words*BiomeCount)
	for i, name := range names {
		id := BiomeTagID(i)
		t.ids[name] = id
		for member := range tags[name] {
			if b, ok := BiomeByName(member); ok {
				t.bits[int(b)*t.words+int(id)/64] |= 1 << (id % 64)
			}
		}
	}
	return t
}

// BiomeTagLookup is an immutable snapshot of biome tag membership.
type BiomeTagLookup struct {
	ids   map[string]BiomeTagID
	words int      // bitset width per biome
	bits  []uint64 // words per biome, indexed by Biome
}

// BiomeTagOf returns the ID of a named biome tag, or an ID with no members
// for an unknown name.
func BiomeTagOf(tag string) BiomeTagID {
	if id, ok := tagsNow.Load().biomeTable.ids[tag]; ok {
		return id
	}
	return noBiomeTag
}

// InTag reports whether the biome belongs to the tag.
func (b Biome) InTag(tag BiomeTagID) bool {
	tbl := &tagsNow.Load().biomeTable
	if b < 0 || b >= BiomeCount || tag == noBiomeTag || int(tag)/64 >= tbl.words {
		return false
	}
	return tbl.bits[int(b)*tbl.words+int(tag)/64]&(1<<(tag%64)) != 0
}

// Tag membership for the worldgen biomes the spawn rules ask after. The
// vanilla spawn rules pick a mob's variant by the biome it was born in, and
// the palettes they choose between are named by biome tags - the sheep's
// three palettes by spawns_warm_variant_farm_animals and
// spawns_cold_variant_farm_animals, everything else being the temperate
// one. The tags travel by name, so the lookup reads the same generated tag
// tables the configuration phase sends the client and the answers agree.

// BiomeInTag reports whether a biome belongs to a tag, by the names both go
// by in the minecraft:worldgen/biome tag registry.
func BiomeInTag(biome, tag string) bool {
	return tagsNow.Load().biome[tag][biome]
}

// BiomeTagExists reports whether the tag registry holds a tag of the name
// given, which is what an argument that may name a tag refuses a word for.
func BiomeTagExists(tag string) bool {
	_, ok := tagsNow.Load().biome[tag]
	return ok
}
