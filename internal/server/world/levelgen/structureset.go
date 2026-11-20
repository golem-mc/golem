package levelgen

import (
	"slices"
	"sync/atomic"
)

// SpreadType is how a random_spread placement draws a start's offset within
// its cell. vanilla: RandomSpreadType
type SpreadType uint8

const (
	SpreadLinear SpreadType = iota
	SpreadTriangular
)

// FrequencyReducer is the test that thins a placement's chunks when its
// frequency is below 1. vanilla: StructurePlacement.FrequencyReductionMethod
type FrequencyReducer uint8

const (
	ReduceDefault FrequencyReducer = iota
	// ReduceLegacyType1 is the pillager outposts' test, seeded per 16x16
	// chunks.
	ReduceLegacyType1
	// ReduceLegacyType2 is buried treasure's, with a fixed salt.
	ReduceLegacyType2
	// ReduceLegacyType3 is the mineshafts', a double against the frequency.
	ReduceLegacyType3
)

// StructurePlacement is where a structure set may start: a random_spread grid
// when Spacing is set, else concentric_rings, whose chunks
// ConcentricRingChunks lays out from the Ring fields.
// vanilla: StructurePlacement, RandomSpreadStructurePlacement, ConcentricRingsStructurePlacement
type StructurePlacement struct {
	Spacing, Separation int32
	Spread              SpreadType
	Salt                int32
	// LocateOffset is added to a start chunk's corner for /locate.
	LocateOffset [3]int32
	Frequency    float32
	Reducer      FrequencyReducer
	// A start is refused within ExclusionChunks chunks of a placement chunk
	// of the set named ExclusionSet.
	ExclusionSet    string
	ExclusionChunks int32
	RingDistance    int
	RingSpread      int
	RingCount       int
	// RingPreferred is the biome tag the ring chunks move towards.
	RingPreferred string
}

// StructureSetEntry is a structure a set chooses, with its weight.
type StructureSetEntry struct {
	Structure string
	Weight    int32
}

// StructureSet is a structure_set: the structures it chooses between and
// where they may start.
type StructureSet struct {
	Name      string
	Entries   []StructureSetEntry
	Placement StructurePlacement
}

func spread(spacing, separation int32, t SpreadType, salt int32) StructurePlacement {
	return StructurePlacement{Spacing: spacing, Separation: separation, Spread: t, Salt: salt, Frequency: 1}
}

func entries(names ...string) []StructureSetEntry {
	out := make([]StructureSetEntry, len(names))
	for i, n := range names {
		out[i] = StructureSetEntry{"minecraft:" + n, 1}
	}
	return out
}

// AbandonedCampBiomes are the abandoned camp variants, in identifier order;
// each is the structure minecraft:abandoned_camp_<variant>.
var AbandonedCampBiomes = []string{
	"bamboo_jungle", "birch_forest", "cherry_grove", "dappled_forest", "flower_forest", "forest", "meadow",
	"old_growth_birch_forest", "old_growth_pine_taiga", "old_growth_spruce_taiga", "pale_garden", "savanna",
	"snowy_taiga", "sparse_jungle", "swamp", "taiga", "windswept_forest", "wooded_badlands",
}

// abandonedCampEntries lists the camps in the set's own order, which has
// old_growth_pine_taiga ahead of old_growth_birch_forest.
// vanilla: data/minecraft/worldgen/structure_set/abandoned_camp.json
func abandonedCampEntries() []StructureSetEntry {
	out := make([]StructureSetEntry, 0, len(AbandonedCampBiomes))
	for _, biome := range AbandonedCampBiomes {
		out = append(out, StructureSetEntry{"minecraft:abandoned_camp_" + biome, 1})
	}
	out[7], out[8] = out[8], out[7]
	return out
}

// StructureSets are vanilla's structure sets, in registry (identifier) order.
// vanilla: StructureSets.bootstrap
var StructureSets = []StructureSet{
	{"minecraft:abandoned_camp", abandonedCampEntries(), spread(37, 8, SpreadLinear, 91231127)},
	{"minecraft:ancient_cities", entries("ancient_city"), spread(24, 8, SpreadLinear, 20083232)},
	{"minecraft:buried_treasures", entries("buried_treasure"), StructurePlacement{
		Spacing: 1, Salt: 0, LocateOffset: [3]int32{9, 0, 9}, Frequency: 0.01, Reducer: ReduceLegacyType2}},
	{"minecraft:desert_pyramids", entries("desert_pyramid"), spread(32, 8, SpreadLinear, 14357617)},
	{"minecraft:end_cities", entries("end_city"), spread(20, 11, SpreadTriangular, 10387313)},
	{"minecraft:igloos", entries("igloo"), spread(32, 8, SpreadLinear, 14357618)},
	{"minecraft:jungle_temples", entries("jungle_pyramid"), spread(32, 8, SpreadLinear, 14357619)},
	{"minecraft:mineshafts", entries("mineshaft", "mineshaft_mesa"), StructurePlacement{
		Spacing: 1, Salt: 0, Frequency: 0.004, Reducer: ReduceLegacyType3}},
	{"minecraft:nether_complexes", []StructureSetEntry{{"minecraft:fortress", 2}, {"minecraft:bastion_remnant", 3}},
		spread(NetherComplexSpacing, NetherComplexSeparation, SpreadLinear, NetherComplexSalt)},
	{"minecraft:nether_fossils", entries("nether_fossil"), spread(2, 1, SpreadLinear, 14357921)},
	{"minecraft:ocean_monuments", entries("monument"), spread(32, 5, SpreadTriangular, 10387313)},
	{"minecraft:ocean_ruins", entries("ocean_ruin_cold", "ocean_ruin_warm"), spread(20, 8, SpreadLinear, 14357621)},
	{"minecraft:pillager_outposts", entries("pillager_outpost"), StructurePlacement{
		Spacing: 32, Separation: 8, Salt: 165745296, Frequency: 0.2, Reducer: ReduceLegacyType1,
		ExclusionSet: "minecraft:villages", ExclusionChunks: 10}},
	{"minecraft:ruined_portals", entries("ruined_portal", "ruined_portal_desert", "ruined_portal_jungle", "ruined_portal_swamp",
		"ruined_portal_mountain", "ruined_portal_ocean", "ruined_portal_nether"), spread(40, 15, SpreadLinear, 34222645)},
	{"minecraft:shipwrecks", entries("shipwreck", "shipwreck_beached"), spread(24, 4, SpreadLinear, 165745295)},
	{"minecraft:strongholds", entries("stronghold"), StructurePlacement{
		Frequency: 1, RingDistance: 32, RingSpread: 3, RingCount: 128, RingPreferred: "minecraft:stronghold_biased_to"}},
	{"minecraft:swamp_huts", entries("swamp_hut"), spread(32, 8, SpreadLinear, 14357620)},
	{"minecraft:trail_ruins", entries("trail_ruins"), spread(34, 8, SpreadLinear, 83469867)},
	{"minecraft:trial_chambers", entries("trial_chambers"), spread(34, 12, SpreadLinear, 94251327)},
	{"minecraft:villages", entries("village_plains", "village_desert", "village_savanna", "village_snowy", "village_taiga"),
		spread(34, 8, SpreadLinear, 10387312)},
	{"minecraft:woodland_mansions", entries("mansion"), spread(80, 20, SpreadTriangular, 10387319)},
}

// packWorldgen is the structure sets and structures with the data packs'
// added: a pack's set replaces a vanilla set of its id, in the same place.
type packWorldgen struct {
	sets       []StructureSet
	structures []StructureDef
}

var packStructures atomic.Pointer[packWorldgen]

// SetPackStructures layers the data packs' structure sets and structures over
// vanilla's, which a pack cannot remove; both nil clears them. A structure's
// Index continues its step's count after vanilla's, in the order given.
// vanilla: RegistryDataLoader (a later pack's file replaces the earlier by id)
func SetPackStructures(sets []StructureSet, defs []StructureDef) {
	if len(sets) == 0 && len(defs) == 0 {
		packStructures.Store(nil)
		return
	}
	p := &packWorldgen{sets: slices.Clone(StructureSets), structures: slices.Clone(Structures)}
	for _, set := range sets {
		if i := slices.IndexFunc(p.sets, func(s StructureSet) bool { return s.Name == set.Name }); i >= 0 {
			p.sets[i] = set
		} else {
			p.sets = append(p.sets, set)
		}
	}
	var next [DecorationSteps]int32
	for _, d := range p.structures {
		next[d.Step] = d.Index + 1
	}
	for _, d := range defs {
		if slices.ContainsFunc(p.structures, func(o StructureDef) bool { return o.Name == d.Name }) {
			continue
		}
		d.Index = next[d.Step]
		next[d.Step]++
		p.structures = append(p.structures, d)
	}
	packStructures.Store(p)
}

// AllStructureSets are vanilla's structure sets and the data packs', in
// registry order. The result must not be modified.
func AllStructureSets() []StructureSet {
	if p := packStructures.Load(); p != nil {
		return p.sets
	}
	return StructureSets
}

// AllStructures are vanilla's structures and the data packs', in registry
// order. The result must not be modified.
func AllStructures() []StructureDef {
	if p := packStructures.Load(); p != nil {
		return p.structures
	}
	return Structures
}

// StructureSetNamed returns the set of the name, or nil.
func StructureSetNamed(name string) *StructureSet {
	sets := AllStructureSets()
	for i := range sets {
		if sets[i].Name == name {
			return &sets[i]
		}
	}
	return nil
}

// PotentialChunk is the chunk a random_spread placement puts a start in
// within the grid cell holding chunk (x, z).
// vanilla: RandomSpreadStructurePlacement.getPotentialStructureChunk
func (p *StructurePlacement) PotentialChunk(seed int64, x, z int32) (int32, int32) {
	gx, gz := floorDiv(x, p.Spacing), floorDiv(z, p.Spacing)
	r := NewLegacyRandom(0)
	r.SetLargeFeatureWithSalt(seed, gx, gz, p.Salt)
	limit := p.Spacing - p.Separation
	draw := func() int32 {
		if p.Spread == SpreadTriangular {
			return (r.NextIntN(limit) + r.NextIntN(limit)) / 2
		}
		return r.NextIntN(limit)
	}
	sx := draw()
	sz := draw()
	return gx*p.Spacing + sx, gz*p.Spacing + sz
}

// keeps reports whether the frequency reducer keeps chunk (x, z).
// vanilla: StructurePlacement.applyAdditionalChunkRestrictions
func (p *StructurePlacement) keeps(seed int64, x, z int32) bool {
	if p.Frequency >= 1 {
		return true
	}
	r := NewLegacyRandom(0)
	switch p.Reducer {
	case ReduceLegacyType1:
		r.SetSeed(int64((x>>4)^(z>>4)<<4) ^ seed)
		r.NextInt()
		return r.NextIntN(int32(1/p.Frequency)) == 0
	case ReduceLegacyType2:
		r.SetLargeFeatureWithSalt(seed, x, z, 10387320)
		return r.NextFloat() < p.Frequency
	case ReduceLegacyType3:
		r.SetLargeFeatureSeed(seed, x, z)
		return r.NextDouble() < float64(p.Frequency)
	default:
		// The salt and the chunk go in one place over: x takes the salt.
		r.SetLargeFeatureWithSalt(seed, p.Salt, x, z)
		return r.NextFloat() < p.Frequency
	}
}

// IsStructureChunk reports whether the set may start in chunk (x, z): its
// placement picks the chunk, the frequency keeps it, and no placement chunk of
// the excluded set is near. inRings answers for a concentric_rings placement.
// vanilla: StructurePlacement.isStructureChunk, ChunkGeneratorStructureState.hasStructureChunkInRange
func (s *StructureSet) IsStructureChunk(seed int64, x, z int32, inRings func(x, z int32) bool) bool {
	p := &s.Placement
	if p.Spacing == 0 {
		if !inRings(x, z) {
			return false
		}
	} else if px, pz := p.PotentialChunk(seed, x, z); px != x || pz != z {
		return false
	}
	if !p.keeps(seed, x, z) {
		return false
	}
	if p.ExclusionSet == "" {
		return true
	}
	other := StructureSetNamed(p.ExclusionSet)
	for tx := x - p.ExclusionChunks; tx <= x+p.ExclusionChunks; tx++ {
		for tz := z - p.ExclusionChunks; tz <= z+p.ExclusionChunks; tz++ {
			if other.IsStructureChunk(seed, tx, tz, inRings) {
				return false
			}
		}
	}
	return true
}

// TryEntries offers the set's entries to try in the order a start in chunk
// (x, z) picks them, by weight without replacement, until try accepts one; it
// reports whether one was accepted.
// vanilla: ChunkGenerator.createStructures
func (s *StructureSet) TryEntries(seed int64, x, z int32, try func(StructureSetEntry) bool) bool {
	if len(s.Entries) == 1 {
		return try(s.Entries[0])
	}
	options := append([]StructureSetEntry(nil), s.Entries...)
	r := NewLegacyRandom(0)
	r.SetLargeFeatureSeed(seed, x, z)
	var total int32
	for _, o := range options {
		total += o.Weight
	}
	for len(options) > 0 {
		choice := r.NextIntN(total)
		index := 0
		for _, o := range options {
			if choice -= o.Weight; choice < 0 {
				break
			}
			index++
		}
		selected := options[index]
		if try(selected) {
			return true
		}
		options = append(options[:index], options[index+1:]...)
		total -= selected.Weight
	}
	return false
}

// TerrainAdjustment is how a structure bends the terrain around its pieces.
// vanilla: TerrainAdjustment
type TerrainAdjustment uint8

const (
	AdjustNone TerrainAdjustment = iota
	AdjustBury
	AdjustBeardThin
	AdjustBeardBox
	AdjustEncapsulate
)

// Decoration steps a structure can place in. vanilla: GenerationStep.Decoration
const (
	UndergroundStructuresStep = 3
	SurfaceStructuresStep     = 4
	UndergroundDecorationStep = 7
	DecorationSteps           = 11
)

// StructureDef is a structure's settings: the biome tag it may start in, the
// decoration step it places in and its terrain adaptation.
// vanilla: Structure.StructureSettings
type StructureDef struct {
	Name       string
	Biomes     string
	Step       int32
	Adaptation TerrainAdjustment
	// BiomeList names the biomes it may start in when the pack gave biomes
	// rather than a tag; Biomes is then empty.
	BiomeList []string
	// Index is the structure's place among its step's structures, which
	// seeds its placement random.
	Index int32
}

func def(name, tag string, step int32, a TerrainAdjustment) StructureDef {
	return StructureDef{Name: "minecraft:" + name, Biomes: "minecraft:has_structure/" + tag, Step: step, Adaptation: a}
}

// Structures are vanilla's structures, in registry (identifier) order, which
// is the order applyBiomeDecoration places a step's structures in.
// vanilla: Structures.bootstrap
var Structures = func() []StructureDef {
	const under, surface, decoration = UndergroundStructuresStep, SurfaceStructuresStep, UndergroundDecorationStep
	var out []StructureDef
	for _, biome := range AbandonedCampBiomes {
		out = append(out, def("abandoned_camp_"+biome, "abandoned_camp_"+biome, surface, AdjustBeardThin))
	}
	out = append(out,
		def("ancient_city", "ancient_city", decoration, AdjustBeardBox),
		def("bastion_remnant", "bastion_remnant", surface, AdjustNone),
		def("buried_treasure", "buried_treasure", under, AdjustNone),
		def("desert_pyramid", "desert_pyramid", surface, AdjustNone),
		def("end_city", "end_city", surface, AdjustNone),
		def("fortress", "nether_fortress", decoration, AdjustNone),
		def("igloo", "igloo", surface, AdjustNone),
		def("jungle_pyramid", "jungle_temple", surface, AdjustNone),
		def("mansion", "woodland_mansion", surface, AdjustNone),
		def("mineshaft", "mineshaft", under, AdjustNone),
		def("mineshaft_mesa", "mineshaft_mesa", under, AdjustNone),
		def("monument", "ocean_monument", surface, AdjustNone),
		def("nether_fossil", "nether_fossil", decoration, AdjustBeardThin),
		def("ocean_ruin_cold", "ocean_ruin_cold", surface, AdjustNone),
		def("ocean_ruin_warm", "ocean_ruin_warm", surface, AdjustNone),
		def("pillager_outpost", "pillager_outpost", surface, AdjustBeardThin),
		def("ruined_portal", "ruined_portal_standard", surface, AdjustNone),
		def("ruined_portal_desert", "ruined_portal_desert", surface, AdjustNone),
		def("ruined_portal_jungle", "ruined_portal_jungle", surface, AdjustNone),
		def("ruined_portal_mountain", "ruined_portal_mountain", surface, AdjustNone),
		def("ruined_portal_nether", "ruined_portal_nether", surface, AdjustNone),
		def("ruined_portal_ocean", "ruined_portal_ocean", surface, AdjustNone),
		def("ruined_portal_swamp", "ruined_portal_swamp", surface, AdjustNone),
		def("shipwreck", "shipwreck", surface, AdjustNone),
		def("shipwreck_beached", "shipwreck_beached", surface, AdjustNone),
		def("stronghold", "stronghold", surface, AdjustBury),
		def("swamp_hut", "swamp_hut", surface, AdjustNone),
		def("trail_ruins", "trail_ruins", under, AdjustBury),
		def("trial_chambers", "trial_chambers", under, AdjustEncapsulate),
		def("village_desert", "village_desert", surface, AdjustBeardThin),
		def("village_plains", "village_plains", surface, AdjustBeardThin),
		def("village_savanna", "village_savanna", surface, AdjustBeardThin),
		def("village_snowy", "village_snowy", surface, AdjustBeardThin),
		def("village_taiga", "village_taiga", surface, AdjustBeardThin),
	)
	var next [DecorationSteps]int32
	for i := range out {
		out[i].Index = next[out[i].Step]
		next[out[i].Step]++
	}
	return out
}()

// StructureNamed returns the structure of the name, or nil.
func StructureNamed(name string) *StructureDef {
	all := AllStructures()
	for i := range all {
		if all[i].Name == name {
			return &all[i]
		}
	}
	return nil
}
