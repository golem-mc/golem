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
	stale := c.lightFormsInit && c.lightEncoded == nil && c.chunkLightEncoded == nil
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
	stale = c.lightFormsInit && c.lightEncoded == nil && c.chunkLightEncoded == nil
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

	// Each form is built on its own, in either read order. A retained chunk
	// fills only the slot read; an evicted one rebuilds without filling either.
	torch := false
	for _, tc := range []struct{ chunkFirst, evict bool }{{true, false}, {false, false}, {true, true}, {false, true}} {
		w.HoldChunkPacket(pos)
		w.CachedChunkPacket(c)
		w.mu.RLock()
		beforeLight, beforeChunk := c.encodeLight(), c.encodeChunkLight()
		w.mu.RUnlock()
		torch = !torch
		if torch {
			w.SetBlock(5, 70, 5, torchState)
		} else {
			w.SetBlock(5, 70, 5, airState)
		}
		<-w.RelightAreaAsync(5, 5, 5, 5)

		w.mu.RLock()
		if !c.lightFormsInit || c.lightEncoded != nil || c.chunkLightEncoded != nil {
			t.Fatalf("%+v: a relight should leave both forms to be built", tc)
		}
		if bytes.Equal(beforeLight, c.encodeLight()) || bytes.Equal(beforeChunk, c.encodeChunkLight()) {
			t.Fatalf("%+v: the edit changed no light", tc)
		}
		w.mu.RUnlock()

		if tc.evict {
			w.ReleaseChunkPacket(pos)
			for j := range recentChunkPackets + 1 {
				w.CachedChunkPacket(newChunk(int32(j+1), 0))
			}
			c.lightFormsMu.Lock()
			retained := c.lightFormsRetained
			c.lightFormsMu.Unlock()
			if retained || c.packet.packet != nil {
				t.Fatalf("%+v: the chunk was not evicted", tc)
			}
		}
		read := func(chunkLight bool) {
			w.mu.RLock()
			defer w.mu.RUnlock()
			var got, want []byte
			if chunkLight {
				got, want = c.chunkLightForm(), c.encodeChunkLight()
			} else {
				got, want = c.lightForm(), c.encodeLight()
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%+v: form (chunk light %v) differs from its encoder", tc, chunkLight)
			}
		}
		slots := func() (bool, bool) { return c.lightEncoded != nil, c.chunkLightEncoded != nil }
		read(tc.chunkFirst)
		light, chunkLight := slots()
		wantFirst := !tc.evict
		if tc.chunkFirst && (light || chunkLight != wantFirst) || !tc.chunkFirst && (chunkLight || light != wantFirst) {
			t.Fatalf("%+v: first read filled light %v, chunk light %v", tc, light, chunkLight)
		}
		read(!tc.chunkFirst)
		light, chunkLight = slots()
		if light != wantFirst || chunkLight != wantFirst {
			t.Fatalf("%+v: after both reads light %v, chunk light %v", tc, light, chunkLight)
		}
		if !tc.evict {
			w.ReleaseChunkPacket(pos)
		}
	}
}
