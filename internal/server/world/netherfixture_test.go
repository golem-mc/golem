package world

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

var netherRegionFixtures sync.Map // netherDigestRange -> func() (netherRangeFixture, error)

// netherRangeFixture is a generated range, encoded: its chunks, and the
// fortress starts the generator held once the range was generated, as saved
// NBT keyed by start chunk.
type netherRangeFixture struct {
	chunks, fortresses map[ChunkPos][]byte
}

// cachedNetherRange shares generation, including the scheduler's neighbour
// order, while decoding independent blocks, biomes, block entities and structures.
func cachedNetherRange(seed int64, x0, z0, x1, z1 int32) map[ChunkPos]*Chunk {
	encoded := cachedNetherFixture(seed, x0, z0, x1, z1).chunks
	out := make(map[ChunkPos]*Chunk, len(encoded))
	for pos, data := range encoded {
		_, tag, err := nbt.Decode(data)
		if err != nil {
			panic(err)
		}
		c, err := decodeChunkNBT(tag.(nbt.Compound))
		if err != nil {
			panic(err)
		}
		out[pos] = c
	}
	return out
}

func cachedNetherFixture(seed int64, x0, z0, x1, z1 int32) netherRangeFixture {
	key := netherDigestRange{seed, x0, z0, x1, z1}
	build, _ := netherRegionFixtures.LoadOrStore(key, sync.OnceValues(func() (netherRangeFixture, error) {
		g, chunks := generateNetherRangeWith(seed, x0, z0, x1, z1)
		f := netherRangeFixture{chunks: make(map[ChunkPos][]byte, len(chunks)), fortresses: map[ChunkPos][]byte{}}
		for pos, c := range chunks {
			data, err := fixtureChunkBytes(c)
			if err != nil {
				return f, err
			}
			f.chunks[pos] = data
		}
		for _, s := range g.fortress.starts {
			if s == nil {
				continue
			}
			data, err := nbt.Encode("", fortressStartNBT(s))
			if err != nil {
				return f, err
			}
			f.fortresses[ChunkPos{X: s.ChunkX, Z: s.ChunkZ}] = data
		}
		return f, nil
	}))
	f, err := build.(func() (netherRangeFixture, error))()
	if err != nil {
		panic(err)
	}
	return f
}

func fixtureChunkBytes(c *Chunk) ([]byte, error) {
	root, err := encodeChunkNBT(c)
	if err != nil {
		return nil, err
	}
	return nbt.Encode("", root)
}

func BenchmarkNetherGoldenRegion(b *testing.B) {
	generateNetherRange(0, -42, -16, -37, -11)
	for b.Loop() {
		generateNetherRange(0, -42, -16, -37, -11)
	}
}
