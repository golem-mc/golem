package world

import (
	"bytes"
	"sync"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestChunkPacketRetention(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pos := ChunkPos{}
	c, _ := w.Acquire(pos)
	w.HoldChunkPacket(pos)
	w.HoldChunkPacket(pos)
	packet := w.CachedChunkPacket(c)
	light := w.LightPacket(pos)
	w.ReleaseChunkPacket(pos)
	// Empty chunks exercise the cache bound without generating unused terrain.
	churn := func() {
		for i := range recentChunkPackets + 1 {
			other := newChunk(int32(i+1), 0)
			other.lightFormsStale = true
			w.CachedChunkPacket(other)
		}
	}
	churn()
	if got := w.CachedChunkPacket(c); got != packet {
		t.Fatal("a second pending viewer lost the shared packet")
	}
	w.ReleaseChunkPacket(pos)
	churn()
	if c.packet.packet != nil || c.lightEncoded != nil || c.chunkLightEncoded != nil {
		t.Fatal("an idle chunk retained its packet or light forms beyond the bound")
	}
	if !c.hasLightForms() {
		t.Fatal("eviction lost the light engine's initialized mark")
	}
	if got := w.LightPacket(pos); !bytes.Equal(got, light) {
		t.Fatal("eviction changed Update Light")
	}
	w.mu.RLock()
	root, err := encodeChunkNBTWith(c, nil, chunkTicks{})
	w.mu.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
	if data, ok := nbt.Get[nbt.ByteArray](root, "LightData"); !ok || !bytes.Equal(data, light[2:]) {
		t.Fatal("eviction changed saved light")
	}
	if c.lightEncoded != nil || c.chunkLightEncoded != nil {
		t.Fatal("Update Light or a save repopulated an evicted cache")
	}
	if got := w.CachedChunkPacket(c); got == packet || !bytes.Equal(got.Data(), packet.Data()) {
		t.Fatal("a later send must rebuild an identical packet")
	}
	w.HoldChunkPacket(ChunkPos{X: -1})
	w.ReleaseChunkPacket(ChunkPos{X: -1})
	if w.packets.recent.Len() > recentChunkPackets || len(w.packets.entries) > recentChunkPackets {
		t.Fatal("cancelled generation or old packets exceeded the cache bound")
	}
}

func TestChunkPacketEvictionDuringRelight(t *testing.T) {
	w := lightWorld(t)
	w.FlushLighting()
	c, _ := w.Acquire(ChunkPos{})
	w.CachedChunkPacket(c)
	w.mu.RLock()
	s := w.newLightShadow(0, 0, 15, 15)
	w.mu.RUnlock()
	defer s.release(true)
	for i := range recentChunkPackets + 1 {
		w.CachedChunkPacket(newChunk(int32(i+100), 0))
	}
	w.mu.Lock()
	s.install()
	w.mu.Unlock()
	if c.lightEncoded != nil || c.chunkLightEncoded != nil {
		t.Fatal("a relight restored the evicted snapshot's light forms")
	}
	if got := w.CachedChunkPacket(c).Data(); !bytes.Equal(got, chunkPacketReference(w, c)) {
		t.Fatal("relight after eviction changed Chunk Data")
	}
}

func TestChunkPacketUnloadForgetsCache(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pos := ChunkPos{}
	c, _ := w.Acquire(pos)
	w.CachedChunkPacket(c)
	w.Release(pos)
	w.FlushSaves()
	if w.ChunkLoaded(pos) {
		t.Fatal("released chunk did not unload")
	}
	if len(w.packets.entries) != 0 || w.packets.recent.Len() != 0 {
		t.Fatal("the packet cache kept unloaded terrain alive")
	}
}

func TestChunkPacketEvictionConcurrentReaders(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pos := ChunkPos{}
	c, _ := w.Acquire(pos)
	want := w.CachedChunkPacket(c).Data()
	wantLight := w.LightPacket(pos)
	var readers sync.WaitGroup
	for range 4 {
		readers.Go(func() {
			for range 100 {
				if !bytes.Equal(w.CachedChunkPacket(c).Data(), want) || !bytes.Equal(w.LightPacket(pos), wantLight) {
					t.Error("concurrent eviction changed the packet bytes")
					return
				}
			}
		})
	}
	for i := range recentChunkPackets * 2 {
		w.CachedChunkPacket(newChunk(int32(i+1), 0))
	}
	readers.Wait()
}

func TestChunkPacketEvictionDoesNotAdoptUnloadedChunk(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pos := ChunkPos{}
	c, _ := w.Acquire(pos)
	w.CachedChunkPacket(c)
	w.Release(pos)
	w.FlushSaves()
	// An eviction selected c before it unloaded; a new view then books the
	// same position while the eviction is waiting for the old packet builder.
	w.HoldChunkPacket(pos)
	w.evictChunkPacket(c)
	if w.packets.entries[pos].chunk != nil {
		t.Fatal("a future view adopted unloaded terrain from an old eviction")
	}
	w.ReleaseChunkPacket(pos)
}
