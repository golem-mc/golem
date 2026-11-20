package world

import (
	"crypto/rand"
	"encoding/binary"
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The overworld generator: seeded, deterministic terrain. Its shape is vanilla's
// density (terrain.NewRandomState); the surface, caves, ores, trees and
// snow are hand-made Perlin noise fields derived from the world seed.
//
// Every feature is a pure function of the seed and world coordinates, so chunks
// generate on demand, in any order, identically after a restart. No feature
// consults a neighbouring chunk: a canopy crossing a border is grown again by
// the neighbour from the same seeds.

// Salts keep the noise fields independent while all derive from the one seed.
const (
	saltForest     = 0x94D049BB
	saltOceanFloor = 0xC2B2AE3D
	saltSandstone  = 0x3C6EF372
)

// Terrain shape, in blocks. Sea level is the SeaLevel the login packet sends
// (63); water fills to the block below it, waterTop.
const (
	waterTop = SeaLevel - 1 // the top water block, at y=62
)

// No two fields of a generator may share an origin: a Perlin field collapses
// toward zero along the lattice diagonal in every seed, so fields sampled on a
// shared diagonal would all run quiet along one line. Each field uses its own
// offsets, different on every axis.
const (
	originForestX     = 909.63
	originForestZ     = 223.51
	originOceanFloorX = 557.61
	originOceanFloorZ = 883.09
	originSandstoneX  = 143.77
	originSandstoneZ  = 361.53
	originDeepslateX  = 317.93
	originBedrockZ    = 377.09
)

// OverworldGenerator generates the seeded overworld. Build it with NewGenerator
// (from configuration) or NewOverworldGenerator (from a seed, as tests do).
type OverworldGenerator struct {
	// seed is the generation seed; deferred marks a generator built without
	// one, whose seed NewWorld resolves (from level.dat, or minted at creation)
	// before any chunk is generated.
	seed     int64
	deferred bool
	// preset is the world preset whose noise settings shape the terrain.
	preset overworldPreset
	// single is the biome of a single-biome world when hasSingle; plains
	// otherwise.
	single    terrain.OverworldBiome
	hasSingle bool

	// terrain is the noise settings of a single-biome world built from a data
	// pack stem; the zero value is the normal ones.
	terrain overworldPreset
	// noStructures writes generate_structures=false into a new world's
	// world_gen_settings.dat.
	noStructures bool
	// bonusChest writes bonus_chest=true into a new world's
	// world_gen_settings.dat.
	bonusChest bool
	// flat, when set, makes this a flat world that places structures: its
	// layers instead of the noise terrain, its biome everywhere, its
	// structure sets and no placed features.
	// vanilla: FlatLevelSource
	flat *FlatGenerator
	// liveHeight, when set, answers height from a loaded world instead of the
	// noise; it is set on the throwaway generator /place structure builds with.
	liveHeight func(x, z int32) int32
	// mobFinalizer finalizes structure mobs at placement; nil defers it to
	// the load.
	mobFinalizer StructureMobFinalizer

	// Decoration: shaped neighbour terrain, the proto chunks decorations write
	// into and the placed features in FeatureSorter order.
	decorState
	// shapes counts shapeTerrain calls, for BenchmarkOverworldGenerateSpread.
	shapes atomic.Int64
	// biomeFills counts computeBiomes calls, for BenchmarkOverworldJoinGenerate.
	biomeFills atomic.Int64
	shapers    chan struct{}
	zoomSeed   int64
	placed     []overworldPlaced
	// masks holds each chunk's biomes as a bit set, under masksMu.
	masksMu sync.Mutex
	masks   map[ChunkPos]uint64

	forest     *perlin // where trees grow thick
	oceanFloor *perlin // gravel, sand and dirt patches under the sea
	sandstone  *perlin // how thick the sandstone under a beach's sand is

	// The surface rule's vertical gradients draw from these, as RandomState
	// seeds them.
	bedrockRandom levelgen.PositionalRandomFactory

	// The terrain's block states, resolved once so generation never touches the
	// registry again.
	grassState      uint16
	grassSnowyState uint16
	dirtState       uint16
	stoneFillState  uint16
	sandState       uint16
	gravelState     uint16
	clayState       uint16
	sandstoneState  uint16
	waterState      uint16
	lavaState       uint16
	bedrockState    uint16
	deepslateState  uint16
	coalOreState    uint16
	ironOreState    uint16
	graniteState    uint16
	dioriteState    uint16
	andesiteState   uint16
	tuffState       uint16
	shortGrassState uint16
	snowLayerState  uint16
	snowBlockState  uint16

	calciteState uint16

	// The ores' deepslate variants, written where a vein meets deepslate or
	// tuff (deepslate_ore_replaceables).
	deepslateCoalOreState uint16

	// The vein blocks the terrain fill writes (levelgen.BlockCopperOre and kin).
	copperOreState, rawCopperState      uint16
	deepslateIronOreState, rawIronState uint16

	// biomeSource is the climate-driven biome source of the seed; biomeIDs
	// maps its biomes to registry IDs for the wire.
	biomeSource *terrain.OverworldBiomeSource
	// The surface rule's system and what it reads; biome cells are bounded
	// and guarded by surfaceMu.
	surfaceMu      *sync.Mutex
	cellCache      map[[2]int32]*biomeCells
	biomeClimates  [terrain.OverworldBiomeCount]registry.BiomeClimate
	materialStates [256]uint16
	biomeIDs       [terrain.OverworldBiomeCount]int32

	// rings is the stronghold layout of the current seed, shared by every
	// generator of the seed.
	rings *strongholdRings

	// state is the seeded terrain density (terrain.NewRandomState);
	// heights caches each chunk's column heights from it.
	state     *terrain.RandomState
	heightsMu sync.Mutex
	heights   map[[2]int32]*columnHeights

	// structures holds the current seed's structure starts.
	structures *overworldStructures
}

// NewOverworldGenerator returns an overworld generator for seed, which decides
// every block: the same coordinates and seed generate the same terrain
// anywhere, anytime. It panics if a state or biome is missing from the
// generated registry tables, a data problem worth failing at startup.
func NewOverworldGenerator(seed int64) *OverworldGenerator {
	return NewOverworldGeneratorFor(levelTypeNormal, seed)
}

// NewOverworldGeneratorFor is NewOverworldGenerator for the world preset a
// level-type names; one other than minecraft:large_biomes,
// minecraft:amplified or minecraft:single_biome_surface is the normal
// overworld.
func NewOverworldGeneratorFor(preset string, seed int64) *OverworldGenerator {
	g := &OverworldGenerator{preset: presetOf(parseLevelType(preset))}
	g.setSeed(seed)
	return g
}

// newFlatOverworldGenerator is the flat world of flat, whose structure sets are
// not empty, for seed.
func newFlatOverworldGenerator(seed int64, flat *FlatGenerator) *OverworldGenerator {
	g := &OverworldGenerator{flat: flat}
	g.setSeed(seed)
	return g
}

// NewFlatOverworldGeneratorFor is the flat-mode overworld for seed of
// server.properties' generator-settings, empty for the default flat world,
// even when the preset names no structure sets.
func NewFlatOverworldGeneratorFor(seed int64, generatorSettings string) *OverworldGenerator {
	flat := NewVanillaFlatGenerator()
	if strings.TrimSpace(generatorSettings) != "" {
		flat = flatGeneratorFromSettings(generatorSettings)
	}
	return newFlatOverworldGenerator(seed, flat)
}

// Flat is the flat preset of a flat-mode generator, nil for the noise
// overworld.
func (g *OverworldGenerator) Flat() *FlatGenerator { return g.flat }

// SeaLevel returns the overworld's noise-settings sea level, or a flat world's.
// vanilla: NoiseBasedChunkGenerator.getSeaLevel, FlatLevelSource.getSeaLevel
func (g *OverworldGenerator) SeaLevel() int32 {
	if g.flat != nil {
		return flatSeaLevel
	}
	return SeaLevel
}

// minY is the generator's own minimum Y, which moveBelowSeaLevel keeps
// structures above: a flat world reports 0 whatever its dimension's bottom.
// vanilla: ChunkGenerator.getMinY, FlatLevelSource.getMinY
func (g *OverworldGenerator) minY() int32 {
	if g.flat != nil {
		return 0
	}
	return worldMinY
}

// setSeed builds the noise fields for a seed; called by the constructors and by
// NewWorld for a deferred seed, always before the first chunk.
func (g *OverworldGenerator) setSeed(seed int64) {
	g.seed, g.deferred = seed, false
	g.state = terrain.NewRandomState(g.terrainPreset().dimension(), seed)
	g.heights = map[[2]int32]*columnHeights{}
	g.rings = g.ringsFor(seed)
	g.structures = newOverworldStructures()
	g.setBiomeSource(seed)
	g.shapers = make(chan struct{}, max(1, runtime.GOMAXPROCS(0)-2))
	g.masks = map[ChunkPos]uint64{}
	g.setSurface(seed)
	g.forest = newPerlin(seed, saltForest)
	g.oceanFloor = newPerlin(seed, saltOceanFloor)
	g.sandstone = newPerlin(seed, saltSandstone)
	g.bedrockRandom = levelgen.NewXoroshiroNamedRandom(seed, "minecraft:bedrock_floor")

	mustState := func(name string, overrides map[string]string) uint16 {
		state, ok := registry.FindState(name, overrides)
		if !ok {
			panic("the block registry has no " + name)
		}
		return state
	}
	g.grassState = mustState("minecraft:grass_block", nil)
	g.grassSnowyState = mustState("minecraft:grass_block", map[string]string{"snowy": "true"})
	g.dirtState = mustState("minecraft:dirt", nil)
	g.stoneFillState = stoneState
	g.sandState = mustState("minecraft:sand", nil)
	g.gravelState = mustState("minecraft:gravel", nil)
	g.clayState = mustState("minecraft:clay", nil)
	g.sandstoneState = mustState("minecraft:sandstone", nil)
	g.waterState = mustState("minecraft:water", nil)
	g.lavaState = mustState("minecraft:lava", nil)
	g.bedrockState = mustState("minecraft:bedrock", nil)
	g.deepslateState = mustState("minecraft:deepslate", nil)
	g.coalOreState = mustState("minecraft:coal_ore", nil)
	g.ironOreState = mustState("minecraft:iron_ore", nil)
	g.graniteState = mustState("minecraft:granite", nil)
	g.dioriteState = mustState("minecraft:diorite", nil)
	g.andesiteState = mustState("minecraft:andesite", nil)
	g.tuffState = mustState("minecraft:tuff", nil)
	g.deepslateCoalOreState = mustState("minecraft:deepslate_coal_ore", nil)
	g.copperOreState = mustState("minecraft:copper_ore", nil)
	g.rawCopperState = mustState("minecraft:raw_copper_block", nil)
	g.deepslateIronOreState = mustState("minecraft:deepslate_iron_ore", nil)
	g.rawIronState = mustState("minecraft:raw_iron_block", nil)
	g.shortGrassState = mustState("minecraft:short_grass", nil)
	// The generated cover is snow's default single layer, what
	// SnowAndFreezeFeature places.
	// vanilla: SnowAndFreezeFeature.place
	g.snowLayerState = mustState("minecraft:snow", map[string]string{"layers": "1"})
	g.snowBlockState = mustState("minecraft:snow_block", nil)
	g.calciteState = mustState("minecraft:calcite", nil)

	g.initOverworldFeatures()
}

// NewGenerator returns the generator a world asks for: flat for
// "minecraft:flat", the seeded overworld of the preset parseLevelType reads
// otherwise. An existing world keeps the generator its files record.
// generatorSettings is a new flat world's layers JSON; other kinds ignore it. A level-seed settles either kind's seed; an empty
// one leaves it for NewWorld, where an existing level.dat's seed wins.
func NewGenerator(dir, levelType, levelSeed, generatorSettings string) Generator {
	spec := strings.TrimSpace(levelSeed)
	if stored, flat, ok := storedGenerator(dir); ok {
		levelType = stored
		if flat != nil {
			return flatGenerator(flat, spec)
		}
	} else {
		levelType = parseLevelType(levelType)
	}
	if levelType == levelTypeFlat {
		return flatGenerator(flatGeneratorFromSettings(generatorSettings), spec)
	}
	if levelType == levelTypeDebug {
		g := newDebugGenerator()
		if spec != "" {
			g.seed = ParseSeed(spec)
		} else {
			g.deferred = true
		}
		return g
	}
	if spec != "" {
		return NewOverworldGeneratorFor(levelType, ParseSeed(spec))
	}
	return &OverworldGenerator{deferred: true, preset: presetOf(levelType)}
}

// flatGenerator is the generator of a flat preset: the flat-mode overworld when
// it names structure sets, features or lakes, else the preset itself. A
// level-seed spec settles the seed; an empty one defers it to NewWorld.
func flatGenerator(flat *FlatGenerator, spec string) Generator {
	if flat.decorates() {
		if spec != "" {
			return newFlatOverworldGenerator(ParseSeed(spec), flat)
		}
		return &OverworldGenerator{deferred: true, flat: flat}
	}
	if spec != "" {
		flat.seed = ParseSeed(spec)
	} else {
		flat.deferred = true
	}
	return flat
}

// DisableStructures makes a new world's world_gen_settings.dat carry
// generate_structures=false; an existing file is kept.
func (g *OverworldGenerator) DisableStructures() { g.noStructures = true }

// EnableBonusChest makes a new world's world_gen_settings.dat carry
// bonus_chest=true; an existing file is kept.
func (g *OverworldGenerator) EnableBonusChest() { g.bonusChest = true }

// ParseSeed turns a level-seed into a 64-bit seed as the game does: a signed
// decimal is the seed itself, other text is hashed with Java's String.hashCode
// (UTF-16 code units, 32-bit wraparound). The empty string is not parsed; the
// caller treats it as "choose at creation".
func ParseSeed(spec string) int64 {
	trimmed := strings.TrimSpace(spec)
	if seed, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return seed
	}
	var h int32
	for _, unit := range utf16.Encode([]rune(trimmed)) {
		h = 31*h + int32(unit)
	}
	return int64(h)
}

// mintSeed picks a random seed for a world created without level-seed.
func mintSeed() int64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Only a broken entropy source gets here; a time-derived seed still
		// beats two worlds sharing one.
		return time.Now().UnixNano()
	}
	return int64(binary.LittleEndian.Uint64(b[:]))
}

// initOverworld gives a new world from an overworld generator its final
// level.dat: the overworld generator rather than the template's flat guess, and
// a spawn on dry land.
func (w *World) initOverworld(g *OverworldGenerator) {
	if !w.level.existed {
		if g.flat == nil {
			w.level.raw["Generator"] = nbt.Int(generatorDefault)
		}
		w.settleCreation(g.FindSpawn())
	}
}

// settleSeed settles a generator's seed at world open: a saved seed wins, a
// deferred generator mints one, and either is recorded for level.dat and
// world_gen_settings.dat.
func (w *World) settleSeed(g seedSource) {
	seed, deferred := g.generatorSeed()
	switch {
	case w.level.HasSeed && (deferred || w.level.Seed != seed):
		// The world's own seed wins, like the game, even when it equals a
		// deferred generator's zero placeholder.
		seed = w.level.Seed
		g.setGeneratorSeed(seed)
	case !w.level.HasSeed && deferred:
		// First creation with no level-seed.
		seed = mintSeed()
		g.setGeneratorSeed(seed)
	}
	w.level.putSeed(seed)
	w.zoomSeed = levelgen.BiomeZoomSeed(seed)
}

// settleCreation writes a fresh world's spawn into level.dat and saves it (so
// it sticks even if this process never saves), plus the world_gen_settings.dat
// telling vanilla how the world was made. It runs while the world is built,
// before another goroutine can see it, so it takes no timeMu.
func (w *World) settleCreation(spawn SpawnPos) {
	w.level.Spawn = spawn
	w.level.raw["SpawnX"] = nbt.Int(spawn.X)
	w.level.raw["SpawnY"] = nbt.Int(spawn.Y)
	w.level.raw["SpawnZ"] = nbt.Int(spawn.Z)
	if err := SaveLevel(w.dir, w.level); err != nil {
		slog.Warn("could not write level.dat at world creation", "dir", w.dir, "error", err)
	}
	// existed keeps meaning "found on disk at load": the creation write is not
	// an existing world, and SetDefaultDifficulty, which reads it afterwards,
	// must still apply the configured difficulty.
	if err := w.ensureWorldGen(); err != nil {
		slog.Warn("could not write world_gen_settings.dat at world creation", "dir", w.dir, "error", err)
	}
}

// --- Terrain shape ---

// SurfaceHeight is the Y of the topmost terrain block at the coordinates, for
// standing on the surface (a spawn search) without generating the chunk; trees,
// plants and snow are not counted.
func (g *OverworldGenerator) SurfaceHeight(x, z int32) int32 {
	return g.height(x, z)
}

// forestAt reports woods thick enough to fill with trees rather than dot.
func (g *OverworldGenerator) forestAt(x, z int32) bool {
	return 1.6*g.forest.fbm2(float64(x)+originForestX, float64(z)+originForestZ, 1/150.0, 3) > 0.12
}

// OverworldWoodsAt reports whether the overworld's tree pass plants the column
// as woods, false for any other generator.
func (w *World) OverworldWoodsAt(x, z int32) bool {
	g, ok := w.gen.(*OverworldGenerator)
	return ok && g.flat == nil && g.forestAt(x, z)
}

// Generate fills a chunk with the overworld at its coordinates.
func (g *OverworldGenerator) Generate(c *Chunk) {
	if g.deferred {
		// NewWorld resolves the seed before returning, so only a generator no
		// world was opened with gets here; fail rather than generate terrain
		// that would not regenerate the same.
		panic("overworld generator used before NewWorld resolved its seed")
	}
	g.shape(c)

	cells := g.biomeCellsOf(c.x, c.z)
	g.masksMu.Lock()
	g.storeMask(ChunkPos{X: c.x, Z: c.z}, cells.mask)
	g.masksMu.Unlock()

	g.decorateOverworld(c)
	g.assignBiomes(c, &cells.grid)
	g.saveStructures(c)
}

// shape gives c its undecorated terrain, taking the blocks from the terrain
// cache when a neighbour's decoration already shaped the chunk.
func (g *OverworldGenerator) shape(c *Chunk) {
	g.terrainOf(c.x, c.z, nil).restore(c)
}

// shapeTerrain fills the chunk with terrain and caves, undecorated.
func (g *OverworldGenerator) shapeTerrain(c *Chunk) {
	g.shapes.Add(1)
	g.fillTerrain(c)

}

// FindSpawn chooses the initial world spawn from climate, then nearby chunks; a
// flat world's is the flat spawn.
// vanilla: MinecraftServer.setInitialSpawn, PlayerSpawnFinder.getSpawnPosInChunk
func (g *OverworldGenerator) FindSpawn() SpawnPos {
	if g.flat != nil {
		return SpawnPos{X: 0, Y: g.flat.spawnY(), Z: 0}
	}
	x, z := g.biomeSource.FindSpawnPosition()
	return findSpawnNear(x>>4, z>>4, func(cx, cz int32) (SpawnPos, bool) {
		c := newChunk(cx, cz)
		g.Generate(c)
		for lx := range int32(16) {
			for lz := range int32(16) {
				if spawn, ok := respawnColumn(c, cx*16+lx, cz*16+lz, worldMinY, worldHeight); ok {
					return spawn, true
				}
			}
		}
		return SpawnPos{}, false
	})
}

// vanilla: MinecraftServer.setInitialSpawn
func findSpawnNear(cx, cz int32, scan func(cx, cz int32) (SpawnPos, bool)) SpawnPos {
	var ox, oz, dx int32
	dz := int32(-1)
	for range 11 * 11 {
		if ox >= -5 && ox <= 5 && oz >= -5 && oz <= 5 {
			if spawn, ok := scan(cx+ox, cz+oz); ok {
				return spawn
			}
		}
		if ox == oz || ox < 0 && ox == -oz || ox > 0 && ox == 1-oz {
			dx, dz = -dz, dx
		}
		ox, oz = ox+dx, oz+dz
	}
	// vanilla: ChunkGenerator.getSpawnHeight
	return SpawnPos{X: cx*16 + 8, Y: 64, Z: cz*16 + 8}
}
