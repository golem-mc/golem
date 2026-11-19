package world

import (
	"bytes"
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
