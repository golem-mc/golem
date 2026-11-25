package world

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkChunkPacket is the cost of the payload of a Chunk Data and Update
// Light packet for a chunk of the flat world, with no block entities and then
// with ten, which is what a join or a walk into new ground sends.
func BenchmarkChunkPacket(b *testing.B) {
	w, _ := NewWorld(b.TempDir(), NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	loaded, _ := w.Acquire(ChunkPos{})

	b.Run("0blockentities", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			w.ChunkPacket(loaded)
		}
	})

	chest := registry.BlockStates["minecraft:chest"]
	for i := range 10 {
		x, z := int32(i%4), int32(i/4)
		w.SetBlock(x, GroundLevel, z, chest)
		w.EnsureBlockEntity(x, GroundLevel, z, KindChest)
	}
	b.Run("10blockentities", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			w.ChunkPacket(loaded)
		}
	})

	// A campfire's update tag carries its items, glint override included.
	for i := range 4 {
		x := int32(i)
		w.SetBlock(x, GroundLevel+1, 5, registry.BlockStates["minecraft:campfire"])
		w.EnsureBlockEntity(x, GroundLevel+1, 5, KindCampfire).Use(func(be *BlockEntity) {
			for slot := range be.Slots {
				be.Slots[slot] = protocol.ItemStack{
					ItemID: int32(registry.ItemPorkchop), Count: 1, GlintOverride: protocol.GlintHidden,
				}
			}
		})
	}
	b.Run("4campfiresglint", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			w.ChunkPacket(loaded)
		}
	})

	p := NewPhysics(w)
	ride := movingPistonStateOf(dirEast, false)
	dirt := registry.BlockDirt.DefaultState()
	for i := range 4 {
		x := int32(i)
		w.SetBlock(x, GroundLevel+1, 7, ride)
		p.pistonRideCreate([3]int32{x, GroundLevel + 1, 7}, ride, dirt, dirEast, true, false)
	}
	b.Run("4pistonrides", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			w.ChunkPacket(loaded)
		}
	})
}

// BenchmarkSaveChunk is the cost of writing one dirty chunk of the flat world
// to its region file, the work a save does per chunk somebody changed.
func BenchmarkSaveChunk(b *testing.B) {
	w, _ := NewWorld(b.TempDir(), NewFlatGenerator())
	b.Cleanup(func() { _ = w.Close() })
	pos := ChunkPos{X: 1, Z: -1}
	loaded, _ := w.Acquire(pos)
	w.SetBlock(3, GroundLevel, 3, registry.BlockStates["minecraft:chest"])
	w.EnsureBlockEntity(3, GroundLevel, 3, KindChest)

	b.ReportAllocs()
	for range b.N {
		_, _ = w.saveChunk(pos, loaded)
	}
}

// BenchmarkEncodeSections is the cost of encoding the block sections of a
// generated overworld chunk afresh, which every chunk pays once before it is
// first sent and again after each change.
func BenchmarkEncodeSections(b *testing.B) {
	g := NewOverworldGenerator(5)
	chunks := make([]*Chunk, 16)
	for i := range chunks {
		chunks[i] = newChunk(int32(i*7), int32(i*3))
		g.Generate(chunks[i])
	}
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		c := chunks[i%len(chunks)]
		c.encodeChunkSections(0, len(c.sections))
		i++
	}
}

// BenchmarkLoadChunk reads saved, lit overworld chunks from their region file,
// as a restart's join or a return to saved ground does before insertion.
func BenchmarkLoadChunk(b *testing.B) {
	dir := b.TempDir()
	w, err := NewWorld(dir, NewOverworldGenerator(5))
	if err != nil {
		b.Fatal(err)
	}
	var all []ChunkPos
	for x := range int32(4) {
		for z := range int32(4) {
			pos := ChunkPos{X: x, Z: z}
			w.Acquire(pos)
			all = append(all, pos)
		}
	}
	w.Save()
	w.Close()
	w, err = NewWorld(dir, NewOverworldGenerator(5))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	b.ReportAllocs()
	b.ResetTimer()
	i := 0
	for b.Loop() {
		c, err := w.loadChunk(all[i%len(all)])
		if err != nil || c == nil {
			b.Fatal("chunk did not load", err)
		}
		i++
	}
}

// BenchmarkEncodeChunkNBT is the part of a save done under the world's
// read lock: the NBT tree and its bytes.
func BenchmarkEncodeChunkNBT(b *testing.B) {
	g := NewOverworldGenerator(5)
	chunks := make([]*Chunk, 16)
	for i := range chunks {
		chunks[i] = newChunk(int32(i%4), int32(i/4))
		g.Generate(chunks[i])
	}
	var buf []byte
	b.ReportAllocs()
	b.ResetTimer()
	i := 0
	for b.Loop() {
		root, err := encodeChunkNBTWith(chunks[i%len(chunks)], nil, chunkTicks{})
		if err != nil {
			b.Fatal(err)
		}
		buf, err = nbt.AppendEncode(buf[:0], "", root)
		if err != nil {
			b.Fatal(err)
		}
		i++
	}
	b.ReportMetric(float64(len(buf)), "bytes(last)")
}

// BenchmarkAcquireSavedWithNeighbours acquires and releases a saved, lit
// overworld chunk whose neighbours are held, so the border join on insertion
// sees loaded neighbours.
func BenchmarkAcquireSavedWithNeighbours(b *testing.B) {
	dir := b.TempDir()
	w, err := NewWorld(dir, NewOverworldGenerator(5))
	if err != nil {
		b.Fatal(err)
	}
	for x := range int32(4) {
		for z := range int32(4) {
			w.Acquire(ChunkPos{X: x, Z: z})
		}
	}
	w.Save()
	w.Close()
	w, err = NewWorld(dir, NewOverworldGenerator(5))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	inner := []ChunkPos{{X: 1, Z: 1}, {X: 2, Z: 1}, {X: 1, Z: 2}, {X: 2, Z: 2}}
	for x := range int32(4) {
		for z := range int32(4) {
			if pos := (ChunkPos{X: x, Z: z}); !slices.Contains(inner, pos) {
				w.Acquire(pos)
			}
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	i := 0
	for b.Loop() {
		pos := inner[i%len(inner)]
		if c := w.AcquireSaved(pos); c == nil {
			b.Fatal("chunk did not load")
		}
		w.Release(pos)
		i++
	}
}
