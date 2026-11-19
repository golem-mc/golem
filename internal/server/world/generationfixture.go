package world

import (
	"log/slog"
	"slices"
	"sync"

	"github.com/golem-mc/golem/nbt"
)

// GenerationFixture shares one generator's chunks between worlds: each
// position is generated once, and every world reading it decodes a copy of
// its own. Copies go through the chunk's file form, so they share no state.
// The generator must leave its entities to GeneratedEntities, as the nether's
// does: a copy carries none.
type GenerationFixture struct {
	gen    Generator
	chunks sync.Map // ChunkPos -> func() (fixtureChunk, error)
}

// fixtureChunk is a generated chunk's file form and the ticks its generator
// scheduled, which a file keeps only once physics holds them.
type fixtureChunk struct {
	data  []byte
	ticks []posKey
}

// NewGenerationFixture returns a fixture generating with gen, which no world
// may also generate with: the fixture is its only caller.
func NewGenerationFixture(gen Generator) *GenerationFixture {
	return &GenerationFixture{gen: gen}
}

// chunk returns a fresh copy of the generated chunk at pos.
func (f *GenerationFixture) chunk(pos ChunkPos) (*Chunk, error) {
	build, _ := f.chunks.LoadOrStore(pos, sync.OnceValues(func() (fixtureChunk, error) {
		c := newChunk(pos.X, pos.Z)
		f.gen.Generate(c)
		root, err := encodeChunkNBTWith(c, encodeBlockEntities(c.blockEntities()), chunkTicks{})
		if err != nil {
			return fixtureChunk{}, err
		}
		data, err := nbt.Encode("", root)
		return fixtureChunk{data: data, ticks: c.pendingGenerated}, err
	}))
	generated, err := build.(func() (fixtureChunk, error))()
	if err != nil {
		return nil, err
	}
	tag, err := decodeRoot(generated.data)
	if err != nil {
		return nil, err
	}
	c, err := decodeChunkNBT(tag)
	if err != nil {
		return nil, err
	}
	c.dataVersion = 0 // generated here, not read from another version's file
	c.pendingGenerated = slices.Clone(generated.ticks)
	return c, nil
}

// GenerateFrom makes the world take the chunks it generates from f rather
// than its own generator, which still answers everything else (structures,
// entities, safe spots). It is called before the world loads a chunk.
func (w *World) GenerateFrom(f *GenerationFixture) { w.fixture = f }

// fromFixture is the fixture's copy of the chunk at pos, nil without a
// fixture or when the copy failed, which leaves the chunk to gen.
func (w *World) fromFixture(pos ChunkPos) *Chunk {
	if w.fixture == nil {
		return nil
	}
	c, err := w.fixture.chunk(pos)
	if err != nil {
		slog.Warn("could not copy a fixture chunk, generating instead", "x", pos.X, "z", pos.Z, "error", err)
	}
	return c
}
