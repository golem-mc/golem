package world

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestRelightedLightReachesPacketAndSave relights a cell and checks the Chunk
// Data packet, the Update Light packet and a save all carry the new light,
// though the encoded forms are only built on first read.
func TestRelightedLightReachesPacketAndSave(t *testing.T) {
	t.Parallel()
	w := lightWorld(t)
	w.FlushLighting()
	pos := ChunkPos{0, 0}
	w.mu.RLock()
	c := w.chunks[pos].chunk
	w.mu.RUnlock()
	w.ChunkPacket(c)
	beforeLight := w.LightPacket(pos)

	w.SetBlock(3, 70, 3, torchState)
	<-w.RelightAreaAsync(3, 3, 3, 3)

	w.mu.RLock()
	stale := c.lightFormsStale
	want := c.encodeLight()
	wantChunk := c.encodeChunkLight()
	w.mu.RUnlock()
	if !stale {
		t.Fatal("a relight should leave the forms to be built on first read")
	}
	if bytes.Equal(want, beforeLight[2:]) {
		t.Fatal("the torch changed no light")
	}
	if got := w.ChunkPacket(c); !bytes.HasSuffix(got, wantChunk) {
		t.Fatal("Chunk Data does not carry the new light")
	}

	w.SetBlock(3, 70, 3, airState)
	<-w.RelightAreaAsync(3, 3, 3, 3)
	w.mu.RLock()
	stale = c.lightFormsStale
	want = c.encodeLight()
	w.mu.RUnlock()
	if !stale {
		t.Fatal("Update Light must be the first reader of stale forms")
	}
	if got := w.LightPacket(pos); !bytes.Equal(got[2:], want) {
		t.Fatal("Update Light does not carry the new light")
	}
	w.SetBlock(3, 70, 3, torchState)
	<-w.RelightAreaAsync(3, 3, 3, 3)
	w.mu.RLock()
	want = c.encodeLight()
	root, err := encodeChunkNBTWith(c, nil, chunkTicks{})
	w.mu.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
	data, ok := nbt.Get[nbt.ByteArray](root, "LightData")
	if !ok || !bytes.Equal(data, want) {
		t.Fatal("the save does not carry the new light")
	}
}
