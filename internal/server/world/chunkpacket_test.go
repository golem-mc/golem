package world

import (
	"bytes"
	"os"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// chunkPacketReference is the Chunk Data payload appended field by field into
// one growing buffer, against which ChunkPacket's sized build is checked.
func chunkPacketReference(w *World, c *Chunk) []byte {
	w.mu.RLock()
	var b protocol.Buffer
	b.PutI32(c.x)
	b.PutI32(c.z)
	b.PutRaw(c.heightmaps)
	b.PutBytes(c.encoded)
	light := w.chunkLightWired(c)
	own := c.blockEntities()
	carried := slices.Clone(c.carriedBlockEntities.Items)
	active := c.activeTrialSpawners(carried)
	w.mu.RUnlock()
	putBlockEntities(&b, own, carried, active)
	b.PutRaw(light)
	return b.Bytes()
}

// TestChunkPacketMatchesReference checks ChunkPacket's bytes against the
// reference for a chunk with no block entities, then with a chest, a sign with
// text and a carried banner.
func TestChunkPacketMatchesReference(t *testing.T) {
	t.Parallel()
	w, _ := NewWorld(t.TempDir(), NewFlatGenerator())
	defer w.Close()
	pos := ChunkPos{X: 1, Z: -2}
	c, _ := w.Acquire(pos)
	defer w.Release(pos)
	x0, z0 := pos.X*16, pos.Z*16

	check := func(what string) {
		t.Helper()
		got := w.ChunkPacket(c)
		if want := chunkPacketReference(w, c); !bytes.Equal(got, want) {
			t.Errorf("%s: ChunkPacket differs from the reference (%d bytes, want %d)", what, len(got), len(want))
		}
	}
	check("no block entities")

	w.SetBlock(x0+1, GroundLevel, z0+1, registry.BlockStates["minecraft:chest"])
	w.EnsureBlockEntity(x0+1, GroundLevel, z0+1, KindChest)
	sign, _ := registry.FindState("minecraft:oak_sign", map[string]string{"rotation": "0"})
	w.SetBlock(x0+5, GroundLevel, z0+9, sign)
	w.EnsureBlockEntity(x0+5, GroundLevel, z0+9, KindSign).Use(func(b *BlockEntity) {
		b.SignFront.Messages[0] = "a line long enough to need its own room"
	})
	w.mu.Lock()
	c.carriedBlockEntities = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
		"id": nbt.String("minecraft:banner"),
		"x":  nbt.Int(x0 + 15), "y": nbt.Int(GroundLevel), "z": nbt.Int(z0 + 15),
		"CustomName": nbt.String(`"Flag"`),
	}}}
	w.mu.Unlock()
	check("block entities")
}

func TestMixedChunkPacketGolden(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pos := ChunkPos{X: 1, Z: -2}
	c, _ := w.Acquire(pos)
	defer w.Release(pos)
	x, z := pos.X*16, pos.Z*16
	w.SetBlock(x+15, GroundLevel, z, stoneState)
	w.SetBlock(x, GroundLevel, z+1, registry.BlockStates["minecraft:water"])
	w.SetBlock(x+1, GroundLevel, z+1, registry.BlockStates["minecraft:glass"])

	got := w.ChunkPacket(c)
	want, err := os.ReadFile("testdata/mixed_chunk_packet.golden")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("mixed chunk packet differs from the recorded payload (%d bytes, want %d)", len(got), len(want))
	}
}

// TestChunkSectionsMatchReference checks the cached section form of generated
// overworld and nether chunks against their sections written one after another
// into one growing buffer.
func TestChunkSectionsMatchReference(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	seedWorldDir(t, dir, 5)
	overworld, _ := NewWorld(dir, NewOverworldGenerator(5))
	for _, w := range []*World{overworld, NewDimensionWorld(t.TempDir(), true, NewNetherGenerator(7))} {
		defer w.Close()
		for i := range int32(4) {
			c := newChunk(i*5, -i*3)
			w.gen.Generate(c)
			var want protocol.Buffer
			for s := w.firstSection(); s < w.firstSection()+w.wireSections(); s++ {
				c.sections[s].encode(&want)
			}
			if got := w.encodeChunkWired(c); !bytes.Equal(got, want.Bytes()) {
				t.Errorf("chunk (%d,%d): sections differ from the reference", c.x, c.z)
			}
		}
	}
}
