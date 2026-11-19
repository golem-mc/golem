package world

import (
	"bytes"
	"slices"
	"sync"
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

// TestLazyLightFormsConcurrentReaders exercises cache snapshots and border
// joins under the world read lock while packet readers rebuild stale forms.
func TestLazyLightFormsConcurrentReaders(t *testing.T) {
	for _, path := range []string{"shadow", "join"} {
		t.Run(path, func(t *testing.T) {
			w := lightWorld(t)
			w.FlushLighting()
			pos := ChunkPos{1, 0}
			w.SetBlock(31, 70, 3, torchState)
			<-w.RelightAreaAsync(31, 3, 31, 3)
			w.mu.RLock()
			c := w.chunks[pos].chunk
			w.mu.RUnlock()
			incoming := newChunk(2, 0)
			w.gen.Generate(incoming)
			incoming.SetBlock(0, 70, 8, torchState)
			w.lightAlone(incoming)
			for range 32 {
				w.mu.Lock()
				c.lightFormsStale = true
				w.mu.Unlock()
				start := make(chan struct{})
				var wg sync.WaitGroup
				for _, read := range []func(){
					func() { w.LightPacket(pos) },
					func() { w.ChunkPacket(c) },
					func() {
						w.mu.RLock()
						defer w.mu.RUnlock()
						if path == "shadow" {
							s := w.newLightShadow(16, 0, 31, 15)
							s.release(false)
						} else {
							j := w.newLightJoin(ChunkPos{2, 0}, incoming)
							if !slices.Contains(j.touched, c) {
								t.Error("border join did not read the live chunk's cache")
							}
						}
					},
				} {
					wg.Go(func() { <-start; read() })
				}
				close(start)
				wg.Wait()
			}
		})
	}
}
