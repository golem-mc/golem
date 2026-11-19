package world

import (
	"math/bits"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The heightmap types a chunk packet carries, in the order it sends them.
// vanilla: Heightmap.Types
const (
	heightmapWorldSurface           = 1
	heightmapMotionBlocking         = 4
	heightmapMotionBlockingNoLeaves = 5
)

// heightmapBits is ceil(log2(height+1)), enough for every value from an empty
// column to a full one.
// vanilla: Heightmap.Types.bitsPerEntry
func heightmapBits(height int32) int {
	return bits.Len(uint(height))
}

// heightmapSolid is the motion-blocking heightmaps' block read, without the
// fluid half.
// vanilla: Heightmap.MATERIAL_MOTION_BLOCKING
func heightmapSolid(state uint16) bool {
	return registry.BlockOf(state).InTag(registry.TagIDBlocksMotionInHeightmap)
}

// heightmapColumn returns a column's three heightmap values: the highest
// matching block's height above the floor plus one, 0 for none.
// vanilla: Heightmap.Types
func (w *World) heightmapColumn(c *Chunk, lx, lz int32) (surface, motion, noLeaves int32) {
	top := w.dim.minY + w.dim.height - 1
	// Start at the highest non-uniform-air section; air above holds nothing and
	// a flat chunk is mostly air.
	for s := (top - worldMinY) / sectionSize; s >= 0; s-- {
		if !c.sections[s].isUniform(airState) {
			top = min(top, worldMinY+(s+1)*sectionSize-1)
			break
		}
	}
	for y := top; y >= w.dim.minY; y-- {
		state := c.BlockAt(lx, y, lz)
		// Unknown states are treated as air.
		if IsAirState(state) {
			continue
		}
		raw := y - w.dim.minY + 1
		if surface == 0 {
			surface = raw
		}
		if motion == 0 && motionBlockingByState[state] {
			motion = raw
		}
		if noLeaves == 0 && noLeavesBlockingByState[state] {
			noLeaves = raw
		}
		if surface != 0 && motion != 0 && noLeaves != 0 {
			break
		}
	}
	return surface, motion, noLeaves
}

// noLeavesBlockingByState is MOTION_BLOCKING_NO_LEAVES for every state,
// which generation-time animals stand on.
// vanilla: Heightmap.Types.MOTION_BLOCKING_NO_LEAVES
var noLeavesBlockingByState = func() [65536]bool {
	var m [65536]bool
	for state := range m {
		m[state] = fluidHolding(uint16(state)).kind != fluidNone ||
			registry.BlockOf(uint16(state)).InTag(registry.TagIDBlocksMotionInHeightmapNoLeaves)
	}
	return m
}()

// NoLeavesHeight is the y of the column's highest motion-blocking non-leaf
// block, or one below the floor for none or an unloaded chunk.
// vanilla: Heightmap.Types.MOTION_BLOCKING_NO_LEAVES
func (w *World) NoLeavesHeight(x, z int32) int32 {
	return w.highestOf(&noLeavesBlockingByState, x, z)
}

// OceanFloorHeight is the y of the column's highest block that blocks
// motion, or one below the floor for none or an unloaded chunk.
// vanilla: Heightmap.Types.OCEAN_FLOOR
func (w *World) OceanFloorHeight(x, z int32) int32 {
	return w.highestOf(&oceanFloorByState, x, z)
}

// highestOf is the y of the column's highest block whose state the table
// holds, or one below the floor for none or an unloaded chunk.
func (w *World) highestOf(table *[65536]bool, x, z int32) int32 {
	empty := w.dim.minY - 1
	pos := ChunkPos{X: ChunkOf(float64(x)), Z: ChunkOf(float64(z))}
	lx, lz := x&15, z&15

	w.mu.RLock()
	defer w.mu.RUnlock()
	loaded, ok := w.chunks[pos]
	if !ok {
		return empty
	}
	for y := w.dim.minY + w.dim.height - 1; y >= w.dim.minY; y-- {
		if table[loaded.chunk.BlockAt(lx, y, lz)] {
			return y
		}
	}
	return empty
}

// encodeHeightmaps builds the chunk's three client heightmaps: values packed at
// the dimension's bit width, each prefixed by type and long count.
// vanilla: Heightmap.Types, ClientboundLevelChunkPacketData
func (w *World) encodeHeightmaps(c *Chunk) []byte {
	bits := heightmapBits(w.dim.height)
	perLong := 64 / bits

	var values [3][sectionSize * sectionSize]uint64
	for lx := range int32(sectionSize) {
		for lz := range int32(sectionSize) {
			surface, motion, noLeaves := w.heightmapColumn(c, lx, lz)
			i := int(lz*sectionSize + lx) // the fastest-increasing coordinate is x
			values[0][i] = uint64(surface)
			values[1][i] = uint64(motion)
			values[2][i] = uint64(noLeaves)
		}
	}

	longs := (sectionSize*sectionSize + perLong - 1) / perLong
	var b protocol.Buffer
	// The count, then per map its type and long count, each one varint byte.
	b.Grow(1 + 3*(2+longs*8))
	b.PutVarInt(3)
	types := [3]int32{heightmapWorldSurface, heightmapMotionBlocking, heightmapMotionBlockingNoLeaves}
	for k := range 3 {
		b.PutVarInt(types[k])
		b.PutVarInt(int32(longs))
		v := &values[k]
		putPacked(&b, bits, len(v), func(i int) uint64 { return v[i] })
	}
	return b.Bytes()
}
