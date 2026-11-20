package world

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// PackWorldgen is the enabled data packs' structure generation files, by
// resource id (later packs already replacing earlier ones).
type PackWorldgen struct {
	Structures, StructureSets, TemplatePools, ProcessorLists map[string][]byte
	// Template returns a structure template file (data/<ns>/structure/*.nbt).
	Template func(id string) ([]byte, bool)
}

// jigsawPackData is the packs' files the jigsaw loader reads before the
// embedded archive.
type jigsawPackData struct {
	// files are the JSON files, by the archive's path.
	files    map[string][]byte
	template func(id string) ([]byte, bool)
	// types are the pack structures the overworld grows.
	types map[string]structureType
}

var packJigsaw atomic.Pointer[jigsawPackData]

// packGeneration counts the ApplyPackWorldgen calls, so a generator that read
// the sets before them reads them again.
var packGeneration atomic.Uint64

// read returns the pack's file under the archive path name.
func (p *jigsawPackData) read(name string) ([]byte, bool) {
	if b, ok := p.files[name]; ok {
		return b, true
	}
	if rest, ok := strings.CutSuffix(name, ".nbt"); ok && p.template != nil {
		if ns, path, ok := strings.Cut(rest, "/structure/"); ok {
			return p.template(ns + ":" + path)
		}
	}
	return nil, false
}

// structureAllowsBiome reports whether the structure may start in the biome.
// vanilla: Structure.isValidBiome
func structureAllowsBiome(def *levelgen.StructureDef, b registry.Biome) bool {
	if def.BiomeList != nil {
		return slices.Contains(def.BiomeList, b.Name())
	}
	return b.InTag(registry.BiomeTagOf(def.Biomes))
}

// ApplyPackWorldgen makes the packs' jigsaw structures, structure sets,
// template pools, processor lists and templates part of generation, over the
// embedded vanilla data; an empty value removes them. Pack structures that are
// not jigsaw or that the generator cannot read are logged and skipped, and so
// are structure sets that are not random_spread. It runs once at start.
// vanilla: RegistryDataLoader.WORLD_REGISTRIES
func ApplyPackWorldgen(p PackWorldgen) {
	defer packGeneration.Add(1)
	empty := len(p.Structures)+len(p.StructureSets)+len(p.TemplatePools)+len(p.ProcessorLists) == 0
	if empty {
		if packJigsaw.Swap(nil) != nil {
			resetJigsawCaches()
		}
		levelgen.SetPackStructures(nil, nil)
		registry.ApplyPackStructures(nil)
		return
	}
	data := &jigsawPackData{files: map[string][]byte{}, template: p.Template, types: map[string]structureType{}}
	for kind, files := range map[string]map[string][]byte{
		"worldgen/structure": p.Structures, "worldgen/template_pool": p.TemplatePools,
		"worldgen/processor_list": p.ProcessorLists,
	} {
		for id, b := range files {
			data.files[jigsawDataPath(kind, id, ".json")] = b
		}
	}
	packJigsaw.Store(data)
	resetJigsawCaches()

	var defs []levelgen.StructureDef
	var ids []string
	for _, id := range slices.Sorted(maps.Keys(p.Structures)) {
		if levelgen.StructureNamed(id) != nil {
			// A vanilla id: the pack's file replaces the jigsaw data only.
			continue
		}
		def, err := packStructure(id)
		if err != nil {
			slog.Warn("Data pack structure " + id + " skipped: " + err.Error())
			continue
		}
		defs = append(defs, def)
		ids = append(ids, id)
		data.types[id] = jigsawStructureType{id}
	}
	var sets []levelgen.StructureSet
	for _, id := range slices.Sorted(maps.Keys(p.StructureSets)) {
		set, err := packStructureSet(id, p.StructureSets[id], func(structure string) bool {
			return levelgen.StructureNamed(structure) != nil || slices.Contains(ids, structure)
		})
		if err != nil {
			slog.Warn("Data pack structure set " + id + " skipped: " + err.Error())
			continue
		}
		sets = append(sets, set)
	}
	// The loaders were reset before the structures were read; the pack ones
	// are now known, so the registry follows.
	registry.ApplyPackStructures(ids)
	levelgen.SetPackStructures(sets, defs)
}

var decorationSteps = []string{
	"raw_generation", "lakes", "local_modifications", "underground_structures", "surface_structures",
	"strongholds", "underground_ores", "underground_decoration", "fluid_springs", "vegetal_decoration",
	"top_layer_modification",
}

var terrainAdaptations = []string{"none", "bury", "beard_thin", "beard_box", "encapsulate"}

// packStructure reads a pack structure as a jigsaw structure, loading its
// pools and templates, and returns its settings.
// vanilla: Structure.DIRECT_CODEC, JigsawStructure.CODEC
func packStructure(id string) (levelgen.StructureDef, error) {
	l := newJigsawLoader()
	file := jigsawDataPath("worldgen/structure", id, ".json")
	var head struct {
		Type string `json:"type"`
	}
	b, err := l.read(file)
	if err != nil {
		return levelgen.StructureDef{}, err
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return levelgen.StructureDef{}, err
	}
	if head.Type != "minecraft:jigsaw" {
		return levelgen.StructureDef{}, fmt.Errorf("type %s is not minecraft:jigsaw", head.Type)
	}
	s, err := l.structure(file)
	if err == nil {
		err = l.drain(false)
	}
	if err != nil {
		return levelgen.StructureDef{}, err
	}
	def := levelgen.StructureDef{Name: id}
	step := slices.Index(decorationSteps, strings.TrimPrefix(s.step, "minecraft:"))
	if step < 0 {
		return def, fmt.Errorf("unknown step %q", s.step)
	}
	def.Step = int32(step)
	adapt := slices.Index(terrainAdaptations, strings.TrimPrefix(s.adaptation, "minecraft:"))
	switch {
	case s.adaptation == "":
		adapt = 0
	case adapt < 0:
		return def, fmt.Errorf("unknown terrain adaptation %q", s.adaptation)
	}
	def.Adaptation = levelgen.TerrainAdjustment(adapt)
	var tag string
	var list []string
	switch {
	case json.Unmarshal(s.biomes, &tag) == nil:
		if t, ok := strings.CutPrefix(tag, "#"); ok {
			def.Biomes = registry.Qualify(t)
		} else {
			def.BiomeList = []string{registry.Qualify(tag)}
		}
	case json.Unmarshal(s.biomes, &list) == nil:
		def.BiomeList = make([]string, len(list))
		for i, b := range list {
			def.BiomeList[i] = registry.Qualify(b)
		}
	default:
		return def, fmt.Errorf("biomes is neither a tag, a biome nor a list")
	}
	// Load the structure under its own id, so the first generation need not.
	jigsawStructureLoaders[id] = func() *jigsawStructure { return s }
	return def, nil
}

// packStructureSet reads a random_spread structure_set. known says whether a
// structure id exists; entries of unknown structures are dropped and a set
// without entries is an error.
// vanilla: StructureSet.DIRECT_CODEC, RandomSpreadStructurePlacement.CODEC
func packStructureSet(id string, raw []byte, known func(string) bool) (levelgen.StructureSet, error) {
	var j struct {
		Structures []struct {
			Structure string `json:"structure"`
			Weight    int32  `json:"weight"`
		} `json:"structures"`
		Placement struct {
			Type       string   `json:"type"`
			Spacing    int32    `json:"spacing"`
			Separation int32    `json:"separation"`
			SpreadType string   `json:"spread_type"`
			Salt       int32    `json:"salt"`
			Frequency  *float32 `json:"frequency"`
			Reduction  string   `json:"frequency_reduction_method"`
			Exclusion  *struct {
				OtherSet   string `json:"other_set"`
				ChunkCount int32  `json:"chunk_count"`
			} `json:"exclusion_zone"`
			LocateOffset []int32 `json:"locate_offset"`
		} `json:"placement"`
	}
	set := levelgen.StructureSet{Name: id}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&j); err != nil {
		return set, err
	}
	p := &j.Placement
	if p.Type != "minecraft:random_spread" {
		return set, fmt.Errorf("placement %s is not supported, only minecraft:random_spread", p.Type)
	}
	if p.Spacing < 1 || p.Spacing > 4096 || p.Separation < 0 || p.Separation >= p.Spacing {
		return set, fmt.Errorf("spacing %d and separation %d: want 1 <= spacing <= 4096 and 0 <= separation < spacing", p.Spacing, p.Separation)
	}
	pl := levelgen.StructurePlacement{Spacing: p.Spacing, Separation: p.Separation, Salt: p.Salt, Frequency: 1}
	switch strings.TrimPrefix(p.SpreadType, "minecraft:") {
	case "", "linear":
	case "triangular":
		pl.Spread = levelgen.SpreadTriangular
	default:
		return set, fmt.Errorf("unknown spread_type %q", p.SpreadType)
	}
	if p.Frequency != nil {
		if *p.Frequency < 0 || *p.Frequency > 1 {
			return set, fmt.Errorf("frequency %v is outside 0..1", *p.Frequency)
		}
		pl.Frequency = *p.Frequency
	}
	switch strings.TrimPrefix(p.Reduction, "minecraft:") {
	case "", "default":
	case "legacy_type_1":
		pl.Reducer = levelgen.ReduceLegacyType1
	case "legacy_type_2":
		pl.Reducer = levelgen.ReduceLegacyType2
	case "legacy_type_3":
		pl.Reducer = levelgen.ReduceLegacyType3
	default:
		return set, fmt.Errorf("unknown frequency_reduction_method %q", p.Reduction)
	}
	if e := p.Exclusion; e != nil {
		if e.ChunkCount < 1 || e.ChunkCount > 16 {
			return set, fmt.Errorf("exclusion_zone chunk_count %d is outside 1..16", e.ChunkCount)
		}
		pl.ExclusionSet, pl.ExclusionChunks = registry.Qualify(e.OtherSet), e.ChunkCount
	}
	if len(p.LocateOffset) != 0 {
		if len(p.LocateOffset) != 3 {
			return set, fmt.Errorf("locate_offset has %d numbers, want 3", len(p.LocateOffset))
		}
		copy(pl.LocateOffset[:], p.LocateOffset)
	}
	set.Placement = pl
	for _, e := range j.Structures {
		sid := registry.Qualify(e.Structure)
		if e.Weight < 1 {
			return set, fmt.Errorf("structure %s has weight %d", sid, e.Weight)
		}
		if known(sid) {
			set.Entries = append(set.Entries, levelgen.StructureSetEntry{Structure: sid, Weight: e.Weight})
		}
	}
	if len(set.Entries) == 0 {
		return set, fmt.Errorf("no structure it names is known")
	}
	return set, nil
}

// PackWorldgenActive reports whether data packs changed structure generation.
func PackWorldgenActive() bool { return packJigsaw.Load() != nil }

// ForgetGenerated drops the decorated and shaped chunks the generator kept
// from before the data packs were read (the spawn search generates chunks
// while the world opens), so they generate again with the packs' structures.
func (g *OverworldGenerator) ForgetGenerated() {
	d := &g.decorState
	d.claimAll()
	defer d.releaseAll()
	d.protos.mu.Lock()
	d.protos.mem, d.protos.spilling, d.protos.spilled = nil, nil, nil
	d.protos.mu.Unlock()
	d.cache.mu.Lock()
	d.cache.byPos = nil
	d.cache.lru.Init()
	d.cache.mu.Unlock()
}
