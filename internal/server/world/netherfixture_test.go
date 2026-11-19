package world

import (
	"bytes"
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

func TestNetherFixtureReadersAreIndependent(t *testing.T) {
	t.Parallel()
	const seed = 0
	pos := ChunkPos{X: -27, Z: -10}
	open := func() *Chunk {
		return generateNetherRange(seed, pos.X, pos.Z, pos.X, pos.Z)[pos]
	}
	a, b := open(), open()
	before, err := fixtureChunkBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	a.SetBlock(0, 200, 0, torchState)
	starts := a.structuresTag("starts")
	if len(starts) == 0 {
		t.Fatal("fixture has no structure NBT to test")
	}
	starts[bastionID].(nbt.Compound)["id"] = nbt.String("minecraft:invalid")
	for _, c := range []*Chunk{b, open()} {
		got, err := fixtureChunkBytes(c)
		if err != nil || !bytes.Equal(got, before) {
			t.Fatalf("reader changed another copy or the source: %v", err)
		}
	}
	_, fresh := generateNetherRangeWith(seed, pos.X, pos.Z, pos.X, pos.Z)
	want, err := fixtureChunkBytes(fresh[pos])
	if err != nil || !bytes.Equal(want, before) {
		t.Fatalf("fixture differs from fresh generation: %v", err)
	}
}

func BenchmarkNetherGoldenRegion(b *testing.B) {
	generateNetherRange(0, -42, -16, -37, -11)
	for b.Loop() {
		generateNetherRange(0, -42, -16, -37, -11)
	}
}
