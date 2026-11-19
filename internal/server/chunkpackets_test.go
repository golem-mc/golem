package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

func TestPendingChunkPacketView(t *testing.T) {
	w, err := world.NewWorld(t.TempDir(), world.NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	send, generate, queued := world.ChunkPos{}, world.ChunkPos{X: 1}, world.ChunkPos{X: 2}
	c := &client{
		loadedWorld: w, radius: 3,
		pendingSend: []world.ChunkPos{send}, pendingGeneration: []world.ChunkPos{generate},
		generating: map[world.ChunkPos]*world.ChunkRequest{queued: nil},
	}
	c.syncPendingPackets()
	if len(c.pendingPackets) != 3 {
		t.Fatalf("pending packet holds = %d, want 3", len(c.pendingPackets))
	}
	c.releasePendingPacket(send)
	c.pendingSend = nil
	c.pendingGeneration = nil
	c.syncPendingPackets()
	if len(c.pendingPackets) != 1 {
		t.Fatalf("pending packet holds after send/cancellation = %d, want 1", len(c.pendingPackets))
	}
	c.releasePendingPackets()
	if len(c.pendingPackets) != 0 {
		t.Fatal("disconnect or dimension change retained a pending packet")
	}
}
