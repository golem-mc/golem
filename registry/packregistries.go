package registry

import (
	"maps"
	"slices"
	"sync/atomic"

	"github.com/golem-mc/golem/nbt"
)

// synchronizedNow is Synchronized with the data packs' entries applied. It is
// built by variable initialization, not init(), so the package-level IDs
// resolved through Get at init can read it.
var synchronizedNow = func() *atomic.Pointer[[]Registry] {
	p := new(atomic.Pointer[[]Registry])
	p.Store(&Synchronized)
	return p
}()

// Current returns the synchronized registries as Registry Data sends them:
// the built-in entries in their order, then the data packs' new ones.
func Current() []Registry { return *synchronizedNow.Load() }

// ApplyPackRegistries makes the data packs' entries of the synchronized
// registries, by registry name then entry name, what Get and Registry Data
// see. An entry overriding a built-in keeps its ID and gains its data; a new
// one is appended after the built-ins, the new names sorted. Vanilla sorts all
// names together, so its IDs differ; the client takes the order from the
// packet, and every ID resolved from the built-in list stays valid. Entries
// the server cannot model are left out and described in the returned
// messages. vanilla: RegistryDataLoader.load
func ApplyPackRegistries(packs map[string]map[string]nbt.Tag) []string {
	var problems []string
	out := make([]Registry, len(Synchronized))
	for i, r := range Synchronized {
		out[i] = r
		entries := packs[r.Name]
		if len(entries) == 0 {
			continue
		}
		data := make(map[string]nbt.Tag, len(entries))
		var added []string
		for _, name := range slices.Sorted(maps.Keys(entries)) {
			builtin := r.ID(name) >= 0
			switch {
			// World generation cannot place a new biome, and the biome
			// container's bit width follows the built-in count.
			case !builtin && r.Name == "minecraft:worldgen/biome":
				problems = append(problems, "new biome "+name+" is not loaded: world generation cannot place it")
				continue
			// Chunk height and sky light are fixed per dimension.
			case builtin && r.Name == "minecraft:dimension_type":
				problems = append(problems, "override of dimension type "+name+" is not loaded")
				continue
			case !builtin:
				added = append(added, name)
			}
			data[name] = entries[name]
		}
		if len(data) == 0 {
			continue
		}
		out[i].Entries = append(slices.Clip(r.Entries), added...)
		out[i].Data = data
	}
	synchronizedNow.Store(&out)
	damageTypesNow.Store(newDamageTypeState())
	reindexEnchantments()
	return problems
}
