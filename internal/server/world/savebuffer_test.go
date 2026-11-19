package world

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestSaveChunkBytesMatchEncode saves generated chunks one after another, so
// each reuses the buffer the one before grew, and checks every record is
// byte-identical to a fresh nbt.Encode of the same chunk.
func TestSaveChunkBytesMatchEncode(t *testing.T) {
	w, _ := NewWorld(t.TempDir(), NewOverworldGenerator(5))
	t.Cleanup(func() { _ = w.Close() })

	for i := range 6 {
		pos := ChunkPos{X: int32(i*3 - 4), Z: int32(i - 2)}
		c, _ := w.Acquire(pos)
		if i%2 == 0 {
			x, z := pos.X*16+3, pos.Z*16+3
			w.SetBlock(x, GroundLevel, z, registry.BlockStates["minecraft:chest"])
			w.EnsureBlockEntity(x, GroundLevel, z, KindChest)
		}
		if wrote, err := w.saveChunk(pos, c); err != nil || !wrote {
			t.Fatalf("chunk %v: saveChunk = %v, %v", pos, wrote, err)
		}

		w.mu.RLock()
		root, err := encodeChunkNBTWith(c, encodeBlockEntities(c.blockEntities()), w.ticksOfChunk(pos))
		root = w.clipChunkNBT(root)
		w.mu.RUnlock()
		if err != nil {
			t.Fatal(err)
		}
		want, err := nbt.Encode("", root)
		if err != nil {
			t.Fatal(err)
		}

		rx, rz := regionCoords(pos)
		rf, err := w.region(rx, rz)
		if err != nil {
			t.Fatal(err)
		}
		got, err := rf.readChunk(pos)
		w.releaseRegion(rf)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("chunk %v: saved %d bytes differ from nbt.Encode's %d", pos, len(got), len(want))
		}
	}
}
