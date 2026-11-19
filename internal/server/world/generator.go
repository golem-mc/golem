package world

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Generator decides what the world is made of. Chunks are generated when a
// player first comes near, so a generator gets one empty chunk at a time and
// must produce any of them, in any order, from its coordinates alone.
//
// The same coordinates must always produce the same chunk: an unchanged chunk
// is discarded on unload and regenerated when a player returns.
type Generator interface {
	Generate(c *Chunk)
	// SeaLevel is the dimension's sea level, which the client uses for fog and
	// horizon height and Login (play) and Respawn carry.
	// vanilla: ChunkGenerator.getSeaLevel
	SeaLevel() int32
}

// EntityGenerator stands entities in a chunk on first generation (the end
// crystals), returned as entity-file compounds for the chunk holding their
// feature.
type EntityGenerator interface {
	GeneratedEntities(pos ChunkPos) []nbt.Compound
}

// flatLayer is one slice of a flat world's ground: a state and its height.
// Layers stack up from the world's bottom.
type flatLayer struct {
	state  uint16
	height int32
}

// FlatGenerator makes a flat world from its stacked layers under one biome. Its
// terrain ignores the seed, but the world's seed (read by slime chunks,
// structures and mob variants) is settled and persisted like the noise
// generators'. One whose structure sets are not empty generates through an
// OverworldGenerator in flat mode (newFlatOverworldGenerator).
type FlatGenerator struct {
	biome     int32
	biomeName string
	layers    []flatLayer
	// structureSets are the structure_overrides set ids; allStructureSets
	// means every set, for a preset without the key.
	// vanilla: FlatLevelSource.createState
	structureSets    []string
	allStructureSets bool
	// features places the biome's own decoration and lakes the two lava
	// lakes. vanilla: FlatLevelGeneratorSettings.decoration, addLakes
	features, lakes bool
	// bonusChest writes bonus_chest=true into a new world's
	// world_gen_settings.dat.
	bonusChest bool

	// seed is carried for the world's other readers; deferred marks a generator
	// built without one, resolved by NewWorld before any chunk is generated.
	seed     int64
	deferred bool
}

// seedSource is a generator whose seed the world settles at open (level-seed or
// a fresh roll, the world's saved seed winning), the one seed path the
// overworld and flat generators share.
type seedSource interface {
	// generatorSeed returns the built seed and whether it is still open (built
	// without one).
	generatorSeed() (seed int64, deferred bool)
	// setGeneratorSeed settles the generator's seed.
	setGeneratorSeed(seed int64)
}

func (g *OverworldGenerator) generatorSeed() (int64, bool) { return g.seed, g.deferred }
func (g *OverworldGenerator) setGeneratorSeed(seed int64)  { g.setSeed(seed) }
func (g *FlatGenerator) generatorSeed() (int64, bool)      { return g.seed, g.deferred }
func (g *FlatGenerator) setGeneratorSeed(seed int64)       { g.seed, g.deferred = seed, false }

// newFlatGenerator builds the flat generator for biome and layers. It panics on
// an unregistered biome: the names come from this file's presets, so a missing
// one is a registry data problem.
func newFlatGenerator(biome string, layers []flatLayer) *FlatGenerator {
	return &FlatGenerator{
		biome:     registry.MustID("minecraft:worldgen/biome", biome),
		biomeName: biome,
		layers:    layers,
	}
}

// flatBiome resolves a stored preset's biome, plains where the registries lack
// it.
// vanilla: FlatLevelGeneratorSettings
func flatBiome(name string) string {
	if r, ok := registry.Get("minecraft:worldgen/biome"); ok && r.ID(name) >= 0 {
		return name
	}
	return "minecraft:plains"
}

// NewFlatGenerator makes the stone ground the black-box tests build on.
func NewFlatGenerator() *FlatGenerator {
	return newFlatGenerator("minecraft:plains", []flatLayer{
		{state: stoneState, height: GroundLevel - worldMinY},
	})
}

// SetSeed gives the generator the world seed a level-seed named.
func (g *FlatGenerator) SetSeed(seed int64) { g.setGeneratorSeed(seed) }

// NewVanillaFlatGenerator makes vanilla's default flat preset,
// flat_world_preset
// "1;minecraft:bedrock,2*minecraft:dirt,minecraft:grass_block;minecraft:plains":
// bedrock, two dirt and grass_block from the bottom, with strongholds and
// villages.
// vanilla: FlatLevelGeneratorSettings.getDefault
func NewVanillaFlatGenerator() *FlatGenerator {
	g := newFlatGenerator("minecraft:plains", []flatLayer{
		{state: registry.BlockBedrock.DefaultState(), height: 1},
		{state: registry.BlockDirt.DefaultState(), height: 2},
		{state: registry.BlockGrassBlock.DefaultState(), height: 1},
	})
	g.structureSets = []string{"minecraft:strongholds", "minecraft:villages"}
	return g
}

// decorates reports whether the preset places anything after its layers: a
// structure set, features or lakes.
func (g *FlatGenerator) decorates() bool {
	return g.allStructureSets || len(g.structureSets) > 0 || g.features || g.lakes
}

// Generate fills a chunk with the layers and empty sky above.
func (g *FlatGenerator) Generate(c *Chunk) {
	c.SetBiome(g.biome)
	g.fillLayers(c, false)
}

// fillLayers lays the layers from the world's bottom; baseOnly leaves out
// those a flat-mode overworld fills after its features.
func (g *FlatGenerator) fillLayers(c *Chunk, baseOnly bool) {
	y := int32(worldMinY)
	for _, l := range g.layers {
		if !baseOnly || inFlatBase(l.state) {
			c.Fill(y, y+l.height, l.state)
		}
		y += l.height
	}
}

// inFlatBase reports whether a layer state is part of a flat-mode overworld's
// base terrain: one that is MOTION_BLOCKING. The others are written last by a
// TOP_LAYER_MODIFICATION fill (flatFills).
// vanilla: FlatLevelGeneratorSettings.adjustGenerationSettings
func inFlatBase(state uint16) bool {
	return motionBlockingByState[state]
}

// surfaceY is the Y of the topmost base layer block, worldMinY-1 when there
// is none.
// vanilla: FlatLevelSource.getBaseHeight (minus one)
func (g *FlatGenerator) surfaceY() int32 {
	y, top := int32(worldMinY), int32(worldMinY-1)
	for _, l := range g.layers {
		y += l.height
		if inFlatBase(l.state) {
			top = y - 1
		}
	}
	return min(top, worldMinY+worldHeight-1)
}

// baseLayerAt is the base layer state at y: air above or below the layers and
// for a layer outside the base.
// vanilla: FlatLevelSource.getBaseColumn
func (g *FlatGenerator) baseLayerAt(y int32) uint16 {
	bottom := int32(worldMinY)
	for _, l := range g.layers {
		if y >= bottom && y < bottom+l.height {
			if !inFlatBase(l.state) {
				return airState
			}
			return l.state
		}
		bottom += l.height
	}
	return airState
}

// SeaLevel is one above the world's floor.
// vanilla: FlatLevelSource.getSeaLevel
func (g *FlatGenerator) SeaLevel() int32 {
	return flatSeaLevel
}

// spawnY is the first air above the ground, where a fresh flat world's spawn is
// written: the bottom plus the layers' total height, at most the top of the
// world.
// vanilla: FlatLevelSource.getSpawnHeight
func (g *FlatGenerator) spawnY() int32 {
	y := int32(worldMinY)
	for _, l := range g.layers {
		y += l.height
	}
	return min(y, worldMinY+worldHeight)
}

// ProtoGenerator keeps unfinished chunks between Generate calls: decorations
// write into neighbouring chunks, so a chunk is final only once its neighbours
// are decorated.
type ProtoGenerator interface {
	// OpenProtos attaches the generator to the folder holding its unfinished
	// chunks.
	OpenProtos(dir string)
	// FlushProtos saves the unfinished chunks held in memory.
	FlushProtos()
}

// EnableBonusChest makes a new world's world_gen_settings.dat carry
// bonus_chest=true; an existing file is kept.
func (g *FlatGenerator) EnableBonusChest() { g.bonusChest = true }
