package world

import (
	"math"

	"github.com/golem-mc/golem/registry"
)

const (
	// levelTypeDebug is the debug world preset's name.
	// vanilla: WorldPresets.DEBUG
	levelTypeDebug = "minecraft:debug_all_block_states"
	// debugGeneratorType is the generator's type in world_gen_settings.dat.
	debugGeneratorType = "minecraft:debug"

	debugBarrierY = 60 // vanilla: DebugLevelSource.BARRIER_HEIGHT
	debugGridY    = 70 // vanilla: DebugLevelSource.HEIGHT
	debugSeaLevel = 63
)

// DebugSpawn is a new debug world's spawn.
// vanilla: MinecraftServer.setInitialSpawn
var DebugSpawn = SpawnPos{X: 0, Y: 80, Z: 0}

// DebugGenerator makes the debug world: a barrier floor at y=60 and, at y=70, a
// grid showing every block state in registry order. Nothing else generates.
// vanilla: DebugLevelSource
type DebugGenerator struct {
	biome    int32
	seed     int64
	deferred bool
}

// debugGridWidth and debugGridHeight are the grid's extent in states.
// vanilla: DebugLevelSource.GRID_WIDTH, GRID_HEIGHT
var (
	debugGridWidth  = int32(math.Ceil(math.Sqrt(float64(registry.StateCount))))
	debugGridHeight = int32(math.Ceil(float64(registry.StateCount) / float64(debugGridWidth)))
)

// NewDebugGenerator makes the debug world generator for the given seed.
func NewDebugGenerator(seed int64) *DebugGenerator {
	g := newDebugGenerator()
	g.seed = seed
	return g
}

func newDebugGenerator() *DebugGenerator {
	return &DebugGenerator{biome: int32(registry.BiomePlains)}
}

func (g *DebugGenerator) generatorSeed() (int64, bool) { return g.seed, g.deferred }
func (g *DebugGenerator) setGeneratorSeed(seed int64)  { g.seed, g.deferred = seed, false }

// SeaLevel is the debug world's sea level.
// vanilla: DebugLevelSource.getSeaLevel
func (g *DebugGenerator) SeaLevel() int32 { return debugSeaLevel }

// Generate fills a chunk's barrier row and state grid.
// vanilla: DebugLevelSource.applyBiomeDecoration
func (g *DebugGenerator) Generate(c *Chunk) {
	c.SetBiome(g.biome)
	for lx := range int32(16) {
		for lz := range int32(16) {
			x, z := c.x*16+lx, c.z*16+lz
			c.SetBlock(lx, debugBarrierY, lz, registry.BlockBarrier.DefaultState())
			c.SetBlock(lx, debugGridY, lz, DebugStateAt(x, z))
		}
	}
}

// DebugStateAt is the state the debug grid shows at a column: air except on odd
// positive x and z inside the grid, where state i of the registry stands.
// vanilla: DebugLevelSource.getBlockStateFor
func DebugStateAt(x, z int32) uint16 {
	if x <= 0 || z <= 0 || x%2 == 0 || z%2 == 0 {
		return airState
	}
	x /= 2
	if x > debugGridWidth {
		return airState
	}
	z /= 2
	if z > debugGridHeight {
		return airState
	}
	index := x*debugGridWidth + z
	if index < 0 {
		index = -index
	}
	if int(index) >= registry.StateCount {
		return airState
	}
	return uint16(index)
}
