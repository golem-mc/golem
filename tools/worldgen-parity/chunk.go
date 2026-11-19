package main

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

const columnBlocks = 16 * 16

// dimension is one of the three dimensions the tool compares.
type dimension struct {
	// Name is the dimension's path, as -dimension and the world's
	// dimensions/minecraft/<name> folder spell it.
	Name string `json:"name"`
	// MinY and Height are the dimension type's min_y and height.
	MinY   int `json:"min_y"`
	Height int `json:"height"`
}

// The heights are the data pack's dimension types.
var dimensions = map[string]dimension{
	"overworld":  {"overworld", -64, 384},
	"the_nether": {"the_nether", 0, 256},
	"the_end":    {"the_end", 0, 256},
}

// ID is the dimension's namespaced ID, as /execute in takes it.
func (d dimension) ID() string { return "minecraft:" + d.Name }

func (d dimension) blocks() int   { return columnBlocks * d.Height }
func (d dimension) cells() int    { return 16 * d.Height / 4 }
func (d dimension) sections() int { return d.Height / 16 }

// unknownState stands for a vanilla block state this repository's registry
// does not know, so that it never compares equal to one of ours.
const unknownState = 0xFFFF

// unknownBiome is the biome counterpart of unknownState.
const unknownBiome = -1

// chunkData is one generated chunk in a form both sides can be read into.
type chunkData struct {
	X, Z int32
	Dim  dimension
	// Blocks is indexed by index.
	Blocks []uint16
	// Biomes holds biome registry IDs, indexed by cellIndex.
	Biomes []int32
	// Structures lists the structure types that start in this chunk.
	Structures []string
	// Ticks holds our generator's scheduled tick positions in world coordinates.
	Ticks map[[3]int32]bool
	// BlockEntities holds the saved block entity compounds by world x, y, z.
	BlockEntities map[[3]int32]nbt.Compound
	// Entities holds the saved entity compounds; EntitiesRecorded is false
	// for a vanilla cache entry without its entities.
	Entities         []nbt.Compound
	EntitiesRecorded bool
}

// addBlockEntities keys each compound by its x, y and z.
func (c *chunkData) addBlockEntities(compounds []nbt.Compound) {
	c.BlockEntities = make(map[[3]int32]nbt.Compound, len(compounds))
	for _, be := range compounds {
		x, _ := nbt.IntValue(be["x"])
		y, _ := nbt.IntValue(be["y"])
		z, _ := nbt.IntValue(be["z"])
		c.BlockEntities[[3]int32{x, y, z}] = be
	}
}

func newChunkData(d dimension, x, z int32) *chunkData {
	return &chunkData{X: x, Z: z, Dim: d, Blocks: make([]uint16, d.blocks()), Biomes: make([]int32, d.cells())}
}

// index takes x and z local to the chunk and y as a world coordinate.
func (c *chunkData) index(x, y, z int) int {
	return (y-c.Dim.MinY)*columnBlocks + z*16 + x
}

// cellIndex takes cell coordinates: x and z in 0..3, y counted in cells
// from the bottom of the dimension.
func cellIndex(x, y, z int) int {
	return y*16 + z*4 + x
}

// biomeRegistry resolves biome names to the IDs this repository's
// generator writes.
var biomeRegistry, _ = registry.Get("minecraft:worldgen/biome")

func biomeID(name string) int32 {
	if id := biomeRegistry.ID(name); id >= 0 {
		return id
	}
	return unknownBiome
}

func biomeName(id int32) string {
	if id < 0 || int(id) >= len(biomeRegistry.Entries) {
		return "?"
	}
	return biomeRegistry.Entries[id]
}

// blockName is a state's block name without its properties.
func blockName(state uint16) string {
	if state == unknownState {
		return "?"
	}
	name, _, ok := registry.BlockStateName(state)
	if !ok {
		return "?"
	}
	return name
}
