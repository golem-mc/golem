package world

import (
	"math"
	"runtime"
	"strconv"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/registry"
)

// The nether generator: the noise router's density as netherrack with the lava
// sea below y 32, the five multi-noise biomes, the surface rule's bedrock floor
// and roof, soul sand valleys, basalt deltas and nylium, and the nether_cave
// carver, with vanilla's placed features on top (nether_features.go).
//
// The terrain fills y 0..127 of the 256-block dimension, like vanilla's; the
// rest stays air.
//
// Every feature is a pure function of the seed and world coordinates, so chunks
// generate on demand, in any order, identically after a restart.

// The nether's shape in blocks: the noise volume spans netherBottom to
// netherTop-1, and the lava sea fills every open block at or below
// netherLavaLevel, under the noise settings' sea level.
const (
	netherBottom    = 0
	netherTop       = 128
	netherLavaLevel = netherSeaLevel - 1

	// netherSeaLevel is the nether noise settings' sea level, one above the
	// lava ocean.
	// vanilla: NoiseGeneratorSettings.nether
	netherSeaLevel = 32
)

// netherVineAgeMin and netherVineHeads are the head ages 17..25, one state
// each.
// vanilla: WeepingVinesFeature.placeWeepingVinesColumn
const (
	netherVineAgeMin = 17
	netherVineHeads  = 9
)

// NetherGenerator generates the seeded nether. It is not yet wired into the
// server's dimension handling; construct one directly.
type NetherGenerator struct {
	seed     int64
	state    *terrain.RandomState
	zoomSeed int64

	// shapers bounds the goroutines shaping neighbour terrain at once.
	shapers  chan struct{}
	placed   []netherPlaced
	fortress netherFortress
	bastion  netherBastion
	fossil   netherFossil
	portals  netherRuinedPortals
	// noStructures is generate-structures=false.
	noStructures bool
	mobFinalizer StructureMobFinalizer
	decorState

	// The terrain's block states, resolved once so generation never touches the
	// registry again.
	bedrockState       uint16
	netherrackState    uint16
	lavaState          uint16
	soulSandState      uint16
	soulSoilState      uint16
	gravelState        uint16
	glowstoneState     uint16
	quartzOreState     uint16
	goldOreState       uint16
	ancientDebrisState uint16
	crimsonNyliumState uint16
	warpedNyliumState  uint16
	magmaState         uint16
	blackstoneState    uint16
	basaltState        uint16
	fireState          uint16
	soulFireState      uint16
	caveAirState       uint16
	brownMushroomState uint16
	redMushroomState   uint16
	netherWartState    uint16
	warpedWartState    uint16

	// The forests' cover: small plants, vine bodies, and one head state per age
	// 17..25.
	crimsonRootsState       uint16
	warpedRootsState        uint16
	crimsonFungusState      uint16
	warpedFungusState       uint16
	netherSproutsState      uint16
	shroomlightState        uint16
	crimsonStemState        uint16
	warpedStemState         uint16
	weepingVinesPlantState  uint16
	twistingVinesPlantState uint16
	weepingVineHeads        [netherVineHeads]uint16
	twistingVineHeads       [netherVineHeads]uint16

	// The assigned biomes' registry IDs, indexed by levelgen.NetherBiome.
	biomes         [levelgen.BasaltDeltas + 1]int32
	netherWastes   int32
	soulSandValley int32
	crimsonForest  int32
	warpedForest   int32
}

// NewNetherGenerator returns a nether generator for seed, which decides every
// block: the same coordinates and seed generate the same terrain anywhere,
// anytime. It panics if a state or biome is missing from the generated registry
// tables, a data problem worth failing at startup.
func NewNetherGenerator(seed int64) *NetherGenerator {
	g := &NetherGenerator{
		seed:     seed,
		state:    terrain.NewRandomState(terrain.Nether, seed),
		zoomSeed: levelgen.BiomeZoomSeed(seed),
		shapers:  make(chan struct{}, max(1, runtime.GOMAXPROCS(0)-2)),
	}

	mustState := func(name string, overrides map[string]string) uint16 {
		state, ok := registry.FindState(name, overrides)
		if !ok {
			panic("the block registry has no " + name)
		}
		return state
	}
	g.bedrockState = mustState("minecraft:bedrock", nil)
	g.netherrackState = mustState("minecraft:netherrack", nil)
	g.lavaState = mustState("minecraft:lava", map[string]string{"level": "0"})
	g.soulSandState = mustState("minecraft:soul_sand", nil)
	g.soulSoilState = mustState("minecraft:soul_soil", nil)
	g.gravelState = mustState("minecraft:gravel", nil)
	g.glowstoneState = mustState("minecraft:glowstone", nil)
	g.quartzOreState = mustState("minecraft:nether_quartz_ore", nil)
	g.goldOreState = mustState("minecraft:nether_gold_ore", nil)
	g.ancientDebrisState = mustState("minecraft:ancient_debris", nil)
	g.crimsonNyliumState = mustState("minecraft:crimson_nylium", nil)
	g.warpedNyliumState = mustState("minecraft:warped_nylium", nil)
	g.magmaState = mustState("minecraft:magma_block", nil)
	g.blackstoneState = mustState("minecraft:blackstone", nil)
	g.basaltState = mustState("minecraft:basalt", map[string]string{"axis": "y"})
	g.fireState = mustState("minecraft:fire", nil)
	g.soulFireState = mustState("minecraft:soul_fire", nil)
	g.caveAirState = mustState("minecraft:cave_air", nil)
	g.brownMushroomState = mustState("minecraft:brown_mushroom", nil)
	g.redMushroomState = mustState("minecraft:red_mushroom", nil)
	g.netherWartState = mustState("minecraft:nether_wart_block", nil)
	g.warpedWartState = mustState("minecraft:warped_wart_block", nil)
	g.crimsonRootsState = mustState("minecraft:crimson_roots", nil)
	g.warpedRootsState = mustState("minecraft:warped_roots", nil)
	g.crimsonFungusState = mustState("minecraft:crimson_fungus", nil)
	g.warpedFungusState = mustState("minecraft:warped_fungus", nil)
	g.netherSproutsState = mustState("minecraft:nether_sprouts", nil)
	g.shroomlightState = mustState("minecraft:shroomlight", nil)
	g.crimsonStemState = mustState("minecraft:crimson_stem", nil)
	g.warpedStemState = mustState("minecraft:warped_stem", nil)
	g.weepingVinesPlantState = mustState("minecraft:weeping_vines_plant", nil)
	g.twistingVinesPlantState = mustState("minecraft:twisting_vines_plant", nil)
	for i := range g.weepingVineHeads {
		age := map[string]string{"age": strconv.Itoa(netherVineAgeMin + i)}
		g.weepingVineHeads[i] = mustState("minecraft:weeping_vines", age)
		g.twistingVineHeads[i] = mustState("minecraft:twisting_vines", age)
	}

	mustBiome := func(name string) int32 {
		return registry.MustID("minecraft:worldgen/biome", name)
	}
	g.biomes = [...]int32{
		levelgen.NetherWastes:   mustBiome("minecraft:nether_wastes"),
		levelgen.SoulSandValley: mustBiome("minecraft:soul_sand_valley"),
		levelgen.CrimsonForest:  mustBiome("minecraft:crimson_forest"),
		levelgen.WarpedForest:   mustBiome("minecraft:warped_forest"),
		levelgen.BasaltDeltas:   mustBiome("minecraft:basalt_deltas"),
	}
	g.netherWastes = g.biomes[levelgen.NetherWastes]
	g.soulSandValley = g.biomes[levelgen.SoulSandValley]
	g.crimsonForest = g.biomes[levelgen.CrimsonForest]
	g.warpedForest = g.biomes[levelgen.WarpedForest]
	g.initNetherFeatures()
	g.initFortress()
	g.initBastion()
	g.initFossil()
	return g
}

// SeaLevel returns the nether's noise-settings sea level.
func (g *NetherGenerator) SeaLevel() int32 {
	return netherSeaLevel
}

// Generate fills a chunk with the nether: noise terrain, biomes, surface,
// carvers, features. The rest stays air.
func (g *NetherGenerator) Generate(c *Chunk) {
	g.buildTerrain(c, true)
	g.saveStructures(c)
	g.saveFossils(c)
	g.decorateNether(c)
}

// shapeTerrain builds the chunk up to its features: noise fill, biomes,
// surface.
// vanilla: NoiseBasedChunkGenerator.buildTerrain
func (g *NetherGenerator) shapeTerrain(c *Chunk) {
	g.buildTerrain(c, false)
}

// isAir reports whether a state is air or the cave air the carver leaves.
// vanilla: BlockBehaviour.BlockStateBase.isAir
func (g *NetherGenerator) isAir(state uint16) bool {
	return state == airState || state == g.caveAirState
}

// terrain fills the noise volume: netherrack where final density is above 0,
// else lava below netherSeaLevel and air above; the nether has no aquifer.
// beard adds the fossil beardifier; a structure's base column leaves it out.
// vanilla: NoiseBasedChunkGenerator.doFill, NoiseBasedChunkGenerator.createFluidPicker
func (g *NetherGenerator) terrain(c *Chunk, beard bool) {
	var b *terrain.Beardifier
	if beard {
		b = g.fossilBeardifier(c.x, c.z)
	}
	nc := terrain.NewNoiseChunk(g.state, b, g.state.ChunkVolume(c.x, c.z))
	defer nc.Recycle()
	density := nc.SampleDensity()
	defer nc.ReleaseDensity(density)
	nc.DoFill(density, func(x, y, z int32, s terrain.Substance) {
		switch s {
		case terrain.SubstanceDefault:
			c.SetBlock(x&15, y, z&15, g.netherrackState)
		case terrain.SubstanceLava:
			c.SetBlock(x&15, y, z&15, g.lavaState)
		}
	})
}

// netherQuarts holds the noise biomes of the quart columns a chunk's blocks can
// zoom into: its 4x4 plus a ring of one, x-major from the first quart minus
// one. The nether's biome source reads no y.
type netherQuarts [6][6]levelgen.NetherBiome

// quartBiomes samples the biome source over a chunk's quarts and ring.
// vanilla: MultiNoiseBiomeSource.getNoiseBiome
func (g *NetherGenerator) quartBiomes(cx, cz int32) netherQuarts {
	var q netherQuarts
	// Keep the search candidate across the x-major, z-minor biome fill.
	// vanilla: LevelChunkSection.fillBiomesFromNoise, Climate.RTree.lastResult
	last := levelgen.NetherWastes
	for i := range q {
		for j := range q[i] {
			q[i][j] = terrain.NetherBiomeAt(g.state.Router, cx*4-1+int32(i), 0, cz*4-1+int32(j), last)
			last = q[i][j]
		}
	}
	return q
}

// fillBiomes stores the noise biomes in every cell of the chunk.
// vanilla: ChunkAccess.fillBiomesFromNoise
func (g *NetherGenerator) fillBiomes(c *Chunk, q *netherQuarts) {
	c.SetBiome(g.biomes[q[1][1]])
	for i := range int32(4) {
		for j := range int32(4) {
			b := g.biomes[q[i+1][j+1]]
			if b == c.biome {
				continue
			}
			for y := int32(worldMinY); y < worldMinY+worldHeight; y += 4 {
				c.SetCellBiome(i*4, y, j*4, b)
			}
		}
	}
}

// biomeAt is the biome BiomeManager.getBiome reads at the block holding pos.
func (g *NetherGenerator) biomeAt(pos [3]float64) registry.Biome {
	qx, qy, qz := levelgen.ZoomedQuart(g.zoomSeed, int32(math.Floor(pos[0])), int32(math.Floor(pos[1])), int32(math.Floor(pos[2])))
	return registry.Biome(g.biomes[terrain.NetherBiomeAt(g.state.Router, qx, qy, qz)])
}

// regionAt is the column's quart noise biome, for the features' biome filters.
func (g *NetherGenerator) regionAt(x, z int32) int32 {
	return g.biomes[terrain.NetherBiomeAt(g.state.Router, x>>2, 0, z>>2)]
}
