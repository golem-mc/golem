package registry

import (
	"strings"
	"sync/atomic"

	"github.com/golem-mc/golem/nbt"
)

// SpawnConditionKind is the condition type of a variant spawn selector.
// vanilla: SpawnConditions
type SpawnConditionKind uint8

const (
	SpawnConditionAlways SpawnConditionKind = iota
	SpawnConditionBiome
	SpawnConditionMoonBrightness
	SpawnConditionStructure
)

// VariantSpawnSelector is one entry of a variant's spawn_conditions list.
// vanilla: PriorityProvider.Selector, SpawnCondition
type VariantSpawnSelector struct {
	Priority               int
	Kind                   SpawnConditionKind
	BiomeTag               string
	Biomes                 []Biome
	HasMinMoon, HasMaxMoon bool
	MinMoon, MaxMoon       float64
	StructureTag           string
	Structures             []string
}

// VariantSpawnEntry pairs a variant registry entry name with its selectors.
type VariantSpawnEntry struct {
	Name      string
	Selectors []VariantSpawnSelector
}

var variantSpawnsNow = func() *atomic.Pointer[map[string][]VariantSpawnEntry] {
	p := new(atomic.Pointer[map[string][]VariantSpawnEntry])
	m := buildVariantSpawns()
	p.Store(&m)
	return p
}()

// VariantSpawns returns the spawn entries of a variant registry in registry
// order, with data pack entries and overrides decoded.
func VariantSpawns(registryName string) []VariantSpawnEntry {
	return (*variantSpawnsNow.Load())[registryName]
}

func reindexVariantSpawns() {
	m := buildVariantSpawns()
	variantSpawnsNow.Store(&m)
}

func buildVariantSpawns() map[string][]VariantSpawnEntry {
	out := make(map[string][]VariantSpawnEntry, len(builtinVariantSpawns))
	for regName, builtins := range builtinVariantSpawns {
		r, ok := Get(regName)
		if !ok {
			continue
		}
		entries := make([]VariantSpawnEntry, 0, len(r.Entries))
		for _, name := range r.Entries {
			var selectors []VariantSpawnSelector
			if raw, hasPack := r.Data[name]; hasPack {
				selectors = decodeVariantSelectors(raw)
			} else {
				selectors = builtins[name]
			}
			entries = append(entries, VariantSpawnEntry{Name: name, Selectors: selectors})
		}
		out[regName] = entries
	}
	return out
}

func decodeVariantSelectors(raw nbt.Tag) []VariantSpawnSelector {
	doc, ok := nbt.CompoundView(raw)
	if !ok {
		return nil
	}
	list, ok := nbt.Get[nbt.List](doc, "spawn_conditions")
	if !ok {
		return nil
	}
	out := make([]VariantSpawnSelector, 0, len(list.Items))
	for _, item := range list.Items {
		if sel, ok := decodeVariantSelector(item); ok {
			out = append(out, sel)
		}
	}
	return out
}

func decodeVariantSelector(item nbt.Tag) (VariantSpawnSelector, bool) {
	c, ok := nbt.CompoundView(item)
	if !ok {
		return VariantSpawnSelector{}, false
	}
	prio, ok := nbt.IntValue(c["priority"])
	if !ok {
		return VariantSpawnSelector{}, false
	}
	sel := VariantSpawnSelector{Priority: int(prio), Kind: SpawnConditionAlways}
	condTag, hasCond := c["condition"]
	if !hasCond {
		return sel, true
	}
	cond, ok := nbt.CompoundView(condTag)
	if !ok {
		return VariantSpawnSelector{}, false
	}
	typ, ok := nbt.Get[nbt.String](cond, "type")
	if !ok {
		return VariantSpawnSelector{}, false
	}
	switch Qualify(string(typ)) {
	case "minecraft:biome":
		sel.Kind = SpawnConditionBiome
		tag, ids, ok := decodeHolderSet(cond["biomes"])
		if !ok {
			return VariantSpawnSelector{}, false
		}
		if tag != "" {
			sel.BiomeTag = tag
		} else {
			for _, id := range ids {
				if b, ok := BiomeByName(id); ok {
					sel.Biomes = append(sel.Biomes, b)
				}
			}
		}
		return sel, true
	case "minecraft:moon_brightness":
		sel.Kind = SpawnConditionMoonBrightness
		rngTag, hasRng := cond["range"]
		if !hasRng {
			return VariantSpawnSelector{}, false
		}
		if v, ok := nbtDouble(rngTag); ok {
			sel.HasMinMoon, sel.HasMaxMoon = true, true
			sel.MinMoon, sel.MaxMoon = v, v
			return sel, true
		}
		rng, ok := nbt.CompoundView(rngTag)
		if !ok {
			return VariantSpawnSelector{}, false
		}
		if minTag, hasMin := rng["min"]; hasMin {
			v, ok := nbtDouble(minTag)
			if !ok {
				return VariantSpawnSelector{}, false
			}
			sel.HasMinMoon, sel.MinMoon = true, v
		}
		if maxTag, hasMax := rng["max"]; hasMax {
			v, ok := nbtDouble(maxTag)
			if !ok {
				return VariantSpawnSelector{}, false
			}
			sel.HasMaxMoon, sel.MaxMoon = true, v
		}
		return sel, true
	case "minecraft:structure":
		sel.Kind = SpawnConditionStructure
		tag, ids, ok := decodeHolderSet(cond["structures"])
		if !ok {
			return VariantSpawnSelector{}, false
		}
		sel.StructureTag = tag
		sel.Structures = ids
		return sel, true
	default:
		return VariantSpawnSelector{}, false
	}
}

func decodeHolderSet(tag nbt.Tag) (tagName string, ids []string, ok bool) {
	switch v := tag.(type) {
	case nbt.String:
		s := string(v)
		if t, isTag := strings.CutPrefix(s, "#"); isTag {
			return Qualify(t), nil, true
		}
		return "", []string{Qualify(s)}, true
	case nbt.List:
		out := make([]string, 0, len(v.Items))
		for _, item := range v.Items {
			s, isStr := item.(nbt.String)
			if !isStr {
				return "", nil, false
			}
			out = append(out, Qualify(string(s)))
		}
		return "", out, true
	default:
		return "", nil, false
	}
}

func nbtDouble(tag nbt.Tag) (float64, bool) {
	switch v := tag.(type) {
	case nbt.Double:
		return float64(v), true
	case nbt.Float:
		return float64(v), true
	case nbt.Int:
		return float64(v), true
	case nbt.Long:
		return float64(v), true
	case nbt.Short:
		return float64(v), true
	case nbt.Byte:
		return float64(v), true
	default:
		return 0, false
	}
}

func mustBiome(name string) Biome {
	b, ok := BiomeByName(name)
	if !ok {
		panic("registry: no biome " + name)
	}
	return b
}
